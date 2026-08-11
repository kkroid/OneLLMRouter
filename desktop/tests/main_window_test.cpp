#include <QtTest>
#include <QComboBox>
#include <QDir>
#include <QLabel>
#include <QFile>
#include <QLineEdit>
#include <QListWidget>
#include <QPushButton>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QTcpServer>
#include <QTcpSocket>
#include <QTabWidget>

#include "main_window.h"

class MainWindowTest : public QObject
{
    Q_OBJECT
private slots:
    void externalCoreIsReadOnly();
    void apiKeyEditorIsPasswordOnly();
    void modelsAreManagedOnProviderPage();
    void editsPersistAcrossProviderSwitchAndRequestRestart();
    void validationErrorKeepsEditorOpen();
    void discoveryLocksProviderAndReportsMerge();
    void noOpSavePreservesReasoningModels();
    void existingKeyIsUsedForDiscovery();
    void claudeApplySavesSelectedSlotFirst();
    void claudeApplyStopsOnValidationFailure();
};

class TemporaryHome
{
public:
    explicit TemporaryHome(const QString &path)
        : m_home(qgetenv("HOME")), m_profile(qgetenv("USERPROFILE"))
    {
        qputenv("HOME", path.toUtf8());
        qputenv("USERPROFILE", path.toUtf8());
    }
    ~TemporaryHome()
    {
        qputenv("HOME", m_home);
        qputenv("USERPROFILE", m_profile);
    }
private:
    QByteArray m_home;
    QByteArray m_profile;
};

static QByteArray clientTestConfig()
{
    return "server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
           "  - name: Alpha\n    prefix: alpha\n    base_url: https://alpha.invalid\n"
           "    api_key: fake-key\n    models: [old-model, new-model]\n"
           "codex:\n  models: {}\nmodel_slots:\n  default: alpha/old-model\n"
           "  opus: alpha/old-model\n  sonnet: alpha/old-model\n"
           "  haiku: alpha/old-model\n  fable: alpha/old-model\n";
}

static void replaceText(QLineEdit *editor, const QString &text)
{
    editor->setFocus();
    editor->selectAll();
    if (text.isEmpty()) QTest::keyClick(editor, Qt::Key_Backspace);
    else QTest::keyClicks(editor, text);
}

void MainWindowTest::externalCoreIsReadOnly()
{
    ConfigClient client("missing-core", "missing.yaml");
    MainWindow window(&client, true);
    auto *save = window.findChild<QPushButton *>("saveConfig");
    QVERIFY(save);
    QVERIFY(!save->isEnabled());
    auto *status = window.findChild<QLabel *>("statusLabel");
    QVERIFY(status->text().contains("read-only"));
    window.setReadOnly(false);
    QVERIFY(!save->isEnabled());
    QVERIFY(status->text().isEmpty());
}

void MainWindowTest::apiKeyEditorIsPasswordOnly()
{
    ConfigClient client("missing-core", "missing.yaml");
    MainWindow window(&client);
    auto *key = window.findChild<QLineEdit *>("apiKey");
    QVERIFY(key);
    QCOMPARE(key->echoMode(), QLineEdit::Password);
    QVERIFY(key->text().isEmpty());
}

void MainWindowTest::modelsAreManagedOnProviderPage()
{
    ConfigClient client("missing-core", "missing.yaml");
    MainWindow window(&client);
    auto *tabs = window.findChild<QTabWidget *>("configurationTabs");
    auto *models = window.findChild<QListWidget *>("modelList");
    auto *discover = window.findChild<QPushButton *>("discoverModels");
    auto *modelsLabel = window.findChild<QLabel *>("configuredModelsLabel");
    QVERIFY(tabs && models && discover && modelsLabel);
    QCOMPARE(tabs->count(), 3);
    QCOMPARE(tabs->tabText(0), QString("Providers"));
    QVERIFY(tabs->widget(0)->isAncestorOf(models));
    QVERIFY(tabs->widget(0)->isAncestorOf(discover));
    QCOMPARE(discover->text(), QString("Discover Models"));
    QCOMPARE(modelsLabel->text(), QString("Configured Models"));
    QVERIFY(!window.findChild<QPushButton *>("updateProvider"));
}

void MainWindowTest::editsPersistAcrossProviderSwitchAndRequestRestart()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QVERIFY2(!core.isEmpty(), "CMake must configure ONELLM_TEST_CORE");
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    const QString path = directory.filePath("router.yaml");
    QFile config(path);
    QVERIFY(config.open(QIODevice::WriteOnly));
    config.write("server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
                 "  - {name: Alpha, prefix: alpha, base_url: https://alpha.invalid, api_key: alpha-key, models: [a]}\n"
                 "  - {name: Beta, prefix: beta, base_url: https://beta.invalid, api_key: beta-key, models: [b]}\n"
                 "codex:\n  models: {}\nmodel_slots: {}\n");
    config.close();

    ConfigClient client(core, path);
    MainWindow window(&client);
    QSignalSpy restart(&window, &MainWindow::restartRequested);
    auto *providers = window.findChild<QListWidget *>("providerList");
    auto *name = window.findChild<QLineEdit *>("providerName");
    auto *dirty = window.findChild<QLabel *>("dirtyLabel");
    auto *save = window.findChild<QPushButton *>("saveConfig");
    QVERIFY(providers && name && dirty && save);
    QVERIFY(!save->isEnabled());

    replaceText(name, "Alpha Updated");
    QVERIFY(!dirty->isHidden());
    QVERIFY(save->isEnabled());
    providers->setCurrentRow(1);
    providers->setCurrentRow(0);
    QCOMPARE(name->text(), QString("Alpha Updated"));

    QTest::mouseClick(save, Qt::LeftButton);
    QCOMPARE(restart.count(), 1);
    QVERIFY(!dirty->isVisible());
    QVERIFY(!save->isEnabled());
    QFile saved(path);
    QVERIFY(saved.open(QIODevice::ReadOnly));
    QVERIFY(saved.readAll().contains("Alpha Updated"));
}

void MainWindowTest::validationErrorKeepsEditorOpen()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QVERIFY2(!core.isEmpty(), "CMake must configure ONELLM_TEST_CORE");
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    const QString path = directory.filePath("router.yaml");
    QFile file(path);
    QVERIFY(file.open(QIODevice::WriteOnly));
    const QByteArray original =
        "server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
        "  - name: Alpha\n    prefix: alpha\n    base_url: https://old.invalid\n"
        "    api_key: old-secret\n    models: [old]\ncodex:\n  models: {}\nmodel_slots: {}\n";
    file.write(original);
    file.close();

    ConfigClient client(core, path);
    MainWindow window(&client);
    window.show();
    auto *baseUrl = window.findChild<QLineEdit *>("anthropicBaseUrl");
    auto *openAIUrl = window.findChild<QLineEdit *>("openAIBaseUrl");
    auto *responsesUrl = window.findChild<QLineEdit *>("responsesBaseUrl");
    QVERIFY(baseUrl && openAIUrl && responsesUrl);
    replaceText(baseUrl, {});
    replaceText(openAIUrl, {});
    replaceText(responsesUrl, {});
    QTest::mouseClick(window.findChild<QPushButton *>("saveConfig"), Qt::LeftButton);

    QVERIFY(window.isVisible());
    QVERIFY(window.findChild<QLabel *>("statusLabel")->text().contains("providers[0]"));
    QFile unchanged(path);
    QVERIFY(unchanged.open(QIODevice::ReadOnly));
    QCOMPARE(unchanged.readAll(), original);
}

void MainWindowTest::discoveryLocksProviderAndReportsMerge()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QVERIFY2(!core.isEmpty(), "CMake must configure ONELLM_TEST_CORE");
    QTcpServer server;
    QVERIFY(server.listen(QHostAddress::LocalHost));
    connect(&server, &QTcpServer::newConnection, &server, [&] {
        QTcpSocket *socket = server.nextPendingConnection();
        connect(socket, &QTcpSocket::readyRead, socket, [socket] {
            socket->readAll();
            QTimer::singleShot(100, socket, [socket] {
                const QByteArray body = "{\"data\":[{\"id\":\"discovered-a\"}]}";
                socket->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "
                              + QByteArray::number(body.size())
                              + "\r\nConnection: close\r\n\r\n" + body);
                socket->disconnectFromHost();
            });
        });
    });
    QTemporaryDir directory;
    const QString path = directory.filePath("router.yaml");
    QFile file(path);
    QVERIFY(file.open(QIODevice::WriteOnly));
    file.write(QString("server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
                       "  - {name: Alpha, prefix: alpha, base_url: 'http://127.0.0.1:%1', proxy: false, models: [old-a]}\n"
                       "  - {name: Beta, prefix: beta, base_url: https://beta.invalid, proxy: false, models: [old-b]}\n"
                       "codex:\n  models: {}\nmodel_slots: {}\n")
                   .arg(server.serverPort()).toUtf8());
    file.close();

    ConfigClient client(core, path);
    MainWindow window(&client);
    auto *providers = window.findChild<QListWidget *>("providerList");
    auto *models = window.findChild<QListWidget *>("modelList");
    auto *status = window.findChild<QLabel *>("statusLabel");
    QCOMPARE(providers->currentRow(), 0);
    QTest::mouseClick(window.findChild<QPushButton *>("discoverModels"), Qt::LeftButton);
    QVERIFY(!providers->isEnabled());
    QTRY_VERIFY_WITH_TIMEOUT(status->text().contains("Found 1 models; added 1"), 3000);
    QVERIFY(providers->isEnabled());
    QCOMPARE(models->findItems("discovered-a", Qt::MatchExactly).size(), 1);
}

void MainWindowTest::noOpSavePreservesReasoningModels()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QVERIFY2(!core.isEmpty(), "CMake must configure ONELLM_TEST_CORE");
    QTemporaryDir directory;
    const QString path = directory.filePath("router.yaml");
    QFile file(path);
    QVERIFY(file.open(QIODevice::WriteOnly));
    file.write("server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
               "  - {name: Alpha, prefix: alpha, base_url: https://alpha.invalid, models: [plain-model]}\n"
               "codex:\n  models:\n    configured-model:\n      default_reasoning_level: medium\n"
               "      supported_reasoning_levels: [low, medium]\nmodel_slots: {}\n");
    file.close();
    ConfigClient client(core, path);
    ConfigSnapshot before;
    QVERIFY(client.load(&before).succeeded);
    MainWindow window(&client);
    QVERIFY(QMetaObject::invokeMethod(&window, "save", Qt::DirectConnection));
    ConfigSnapshot reloaded;
    QVERIFY(client.load(&reloaded).succeeded);
    QCOMPARE(reloaded.codexModels.size(), before.codexModels.size());
    QCOMPARE(reloaded.codexModels.keys(), before.codexModels.keys());
    for (auto iterator = before.codexModels.cbegin();
         iterator != before.codexModels.cend(); ++iterator) {
        QCOMPARE(reloaded.codexModels.value(iterator.key()).defaultLevel,
                 iterator.value().defaultLevel);
        QCOMPARE(reloaded.codexModels.value(iterator.key()).supportedLevels,
                 iterator.value().supportedLevels);
    }
    QVERIFY(!reloaded.codexModels.contains("plain-model"));
}

void MainWindowTest::existingKeyIsUsedForDiscovery()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QVERIFY2(!core.isEmpty(), "CMake must configure ONELLM_TEST_CORE");
    QTcpServer server;
    QVERIFY(server.listen(QHostAddress::LocalHost));
    QByteArray request;
    connect(&server, &QTcpServer::newConnection, &server, [&] {
        QTcpSocket *socket = server.nextPendingConnection();
        connect(socket, &QTcpSocket::readyRead, socket, [&, socket] {
            request += socket->readAll();
            if (!request.contains("\r\n\r\n")) return;
            const QByteArray body = "{\"data\":[{\"id\":\"discovered-model\"}]}";
            socket->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "
                          + QByteArray::number(body.size())
                          + "\r\nConnection: close\r\n\r\n" + body);
            socket->disconnectFromHost();
        });
    });
    QTemporaryDir directory;
    const QString path = directory.filePath("router.yaml");
    QFile file(path);
    QVERIFY(file.open(QIODevice::WriteOnly));
    file.write(QString("server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
                       "  - name: Alpha\n    prefix: alpha\n    base_url: http://127.0.0.1:%1\n"
                       "    api_key: existing-secret\n    proxy: false\n    models: [model]\n"
                       "codex:\n  models: {}\nmodel_slots: {}\n")
                   .arg(server.serverPort()).toUtf8());
    file.close();
    ConfigClient client(core, path);
    QSignalSpy discovery(&client, &ConfigClient::modelsDiscovered);
    MainWindow window(&client);
    QTest::mouseClick(window.findChild<QPushButton *>("discoverModels"), Qt::LeftButton);
    QVERIFY(discovery.wait(3000));
    QVERIFY(request.toLower().contains("x-api-key: existing-secret"));
    QCOMPARE(window.findChild<QLabel *>("statusLabel")->text(),
             QString("Found 1 models; added 1. Save to apply."));
    QCOMPARE(window.findChild<QListWidget *>("modelList")
                 ->findItems("discovered-model", Qt::MatchExactly).size(), 1);
}

void MainWindowTest::claudeApplySavesSelectedSlotFirst()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    TemporaryHome home(directory.path());
    const QString path = directory.filePath("router.yaml");
    QFile config(path);
    QVERIFY(config.open(QIODevice::WriteOnly));
    config.write(clientTestConfig());
    config.close();

    ConfigClient client(core, path);
    MainWindow window(&client);
    auto *slot = window.findChild<QComboBox *>("slot_default");
    auto *apply = window.findChild<QPushButton *>("claudeApply");
    QVERIFY(slot && apply);
    slot->setCurrentText("alpha/new-model");
    QTest::mouseClick(apply, Qt::LeftButton);

    ConfigSnapshot saved;
    QVERIFY(client.load(&saved).succeeded);
    QCOMPARE(saved.modelSlots.value("default"), QString("alpha/new-model"));
    QFile settings(directory.filePath(".claude/settings.json"));
    QVERIFY(settings.open(QIODevice::ReadOnly));
    const QByteArray generated = settings.readAll();
    QVERIFY(generated.contains("\"ANTHROPIC_MODEL\": \"alpha/new-model\""));
    QVERIFY(!generated.contains("\"ANTHROPIC_MODEL\": \"alpha/old-model\""));
}

void MainWindowTest::claudeApplyStopsOnValidationFailure()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    TemporaryHome home(directory.path());
    const QString path = directory.filePath("router.yaml");
    QFile config(path);
    QVERIFY(config.open(QIODevice::WriteOnly));
    config.write(clientTestConfig());
    config.close();
    QDir().mkpath(directory.filePath(".claude"));
    const QString settingsPath = directory.filePath(".claude/settings.json");
    QFile settings(settingsPath);
    QVERIFY(settings.open(QIODevice::WriteOnly));
    const QByteArray original = "{\"theme\":\"keep\"}\n";
    settings.write(original);
    settings.close();

    ConfigClient client(core, path);
    MainWindow window(&client);
    replaceText(window.findChild<QLineEdit *>("anthropicBaseUrl"), {});
    replaceText(window.findChild<QLineEdit *>("openAIBaseUrl"), {});
    replaceText(window.findChild<QLineEdit *>("responsesBaseUrl"), {});
    QTest::mouseClick(window.findChild<QPushButton *>("claudeApply"), Qt::LeftButton);

    QVERIFY(settings.open(QIODevice::ReadOnly));
    QCOMPARE(settings.readAll(), original);
    const QString error = window.findChild<QLabel *>("claudeError")->text();
    QVERIFY(error.contains("providers[0]"));
}

QTEST_MAIN(MainWindowTest)
#include "main_window_test.moc"
