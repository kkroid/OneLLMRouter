#include <QtTest>
#include <QDir>
#include <QFile>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QTcpServer>
#include <QTcpSocket>

#include "config/config_client.h"

class ConfigClientTest : public QObject
{
    Q_OBJECT
private slots:
    void coreRoundTripIsSecretSafe();
    void endpointModelDefinitionsSurviveRoundTrip();
    void clientCommandsUseCoreContracts();
    void discoversProtocolModels_data();
    void discoversProtocolModels();
};

class HomeEnvironment
{
public:
    explicit HomeEnvironment(const QString &home)
        : oldHome(qgetenv("HOME")), oldProfile(qgetenv("USERPROFILE"))
    {
        qputenv("HOME", home.toUtf8());
        qputenv("USERPROFILE", home.toUtf8());
    }
    ~HomeEnvironment()
    {
        qputenv("HOME", oldHome);
        qputenv("USERPROFILE", oldProfile);
    }
private:
    QByteArray oldHome;
    QByteArray oldProfile;
};

void ConfigClientTest::coreRoundTripIsSecretSafe()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QVERIFY2(!core.isEmpty(), "CMake must configure ONELLM_TEST_CORE");
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    const QString path = directory.filePath("router.yaml");
    QFile file(path);
    QVERIFY(file.open(QIODevice::WriteOnly));
    file.write("server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
               "  - name: Alpha\n    prefix: alpha\n    base_url: https://old.invalid\n"
               "    api_key: old-secret\n    models:\n"
               "      - id: old\n        endpoints: [anthropic]\n"
               "codex:\n  models: {}\nmodel_slots: {}\n");
    file.close();

    ConfigClient client(core, path);
    ConfigSnapshot snapshot;
    QVERIFY2(client.load(&snapshot).succeeded, "config-get failed");
    QCOMPARE(snapshot.providers.size(), 1);
    QVERIFY(snapshot.providers[0].apiKeySet);
    snapshot.providers[0].name = "Updated";
    snapshot.providers[0].models = {"new-model"};
    snapshot.providers[0].modelDefinitions = {
        QJsonObject{{"id", "new-model"}, {"endpoints", QJsonArray{"anthropic"}}}
    };
    snapshot.codexModels.insert("new-model", {"medium", {"low", "medium"}});
    snapshot.modelSlots.insert("default", "alpha/new-model");
    QVERIFY2(client.apply(snapshot).succeeded, "config-apply failed");

    ConfigSnapshot reloaded;
    QVERIFY(client.load(&reloaded).succeeded);
    QCOMPARE(reloaded.providers[0].name, QString("Updated"));
    QCOMPARE(reloaded.providers[0].models, QStringList{"new-model"});
    QCOMPARE(reloaded.modelSlots.value("default"), QString("alpha/new-model"));
    const QByteArray yaml = [&] { QFile applied(path); applied.open(QIODevice::ReadOnly); return applied.readAll(); }();
    QVERIFY(yaml.contains("old-secret"));
}

void ConfigClientTest::endpointModelDefinitionsSurviveRoundTrip()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    const QString path = directory.filePath("router.yaml");
    QFile file(path);
    QVERIFY(file.open(QIODevice::WriteOnly));
    file.write("server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
               "  - name: DeepSeek\n    prefix: ds\n    base_url: https://api.invalid/anthropic\n"
               "    responses_base_url: https://api.invalid\n    api_key: secret\n"
               "    models:\n      - id: deepseek-v4-pro[1m]\n        endpoints: [anthropic]\n"
               "      - id: deepseek-v4-pro\n        endpoints: [responses]\n"
               "        upstream_model: deepseek-v4-pro\ncodex:\n  models: {}\nmodel_slots: {}\n");
    file.close();

    ConfigClient client(core, path);
    ConfigSnapshot snapshot;
    QVERIFY(client.load(&snapshot).succeeded);
    QCOMPARE(snapshot.providers[0].models,
             QStringList({"deepseek-v4-pro[1m]", "deepseek-v4-pro"}));
    QCOMPARE(snapshot.providers[0].modelDefinitions.size(), 2);
    QVERIFY(snapshot.providers[0].modelDefinitions.at(0).isObject());
    snapshot.providers[0].name = "Updated";
    QVERIFY(client.apply(snapshot).succeeded);

    ConfigSnapshot reloaded;
    QVERIFY(client.load(&reloaded).succeeded);
    QCOMPARE(reloaded.providers[0].modelDefinitions.size(), 2);
    const QJsonObject anthropic = reloaded.providers[0].modelDefinitions.at(0).toObject();
    const QJsonObject responses = reloaded.providers[0].modelDefinitions.at(1).toObject();
    QVERIFY(anthropic.value("endpoints").toArray().contains("anthropic"));
    QVERIFY(responses.value("endpoints").toArray().contains("responses"));
    QCOMPARE(responses.value("upstream_model").toString(), QString("deepseek-v4-pro"));
}

void ConfigClientTest::clientCommandsUseCoreContracts()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    HomeEnvironment environment(directory.path());
    const QString configPath = directory.filePath("router.yaml");
    QFile config(configPath);
    QVERIFY(config.open(QIODevice::WriteOnly));
    config.write("server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
                 "  - name: Alpha\n    prefix: alpha\n    base_url: https://alpha.invalid\n"
                 "    responses_base_url: https://alpha.invalid\n    api_key: fake-key\n"
                 "    models:\n      - id: model\n        endpoints: [anthropic, responses]\n"
                 "codex:\n  overwrite_catalog: false\n  models: {}\n"
                 "model_slots:\n  default: alpha/model\n  opus: alpha/model\n"
                 "  sonnet: alpha/model\n  haiku: alpha/model\n  fable: alpha/model\n");
    config.close();
    ConfigClient client(core, configPath);

    ClientCommandResult claude = client.claudeStatus();
    QVERIFY(claude.succeeded);
    QCOMPARE(claude.claude.syncState, QString("absent"));
    claude = client.claudeApply();
    QVERIFY(claude.succeeded);
    QCOMPARE(claude.claude.syncState, QString("current"));
    QVERIFY(claude.claude.changed);
    claude = client.claudeStatus();
    QCOMPARE(claude.claude.syncState, QString("current"));

    QFile settings(directory.filePath(".claude/settings.json"));
    QVERIFY(settings.open(QIODevice::WriteOnly | QIODevice::Truncate));
    const QByteArray different =
        "{\"theme\":\"dark\",\"env\":{\"ANTHROPIC_MODEL\":\"different/model\"}}";
    settings.write(different);
    settings.close();
    QCOMPARE(client.claudeStatus().claude.syncState, QString("different"));
    claude = client.claudeApply();
    QVERIFY(claude.succeeded);
    QVERIFY(claude.claude.backupCreated);
    claude = client.claudeRestore();
    QVERIFY(claude.succeeded);
    QCOMPARE(claude.claude.syncState, QString("different"));
    QVERIFY(settings.open(QIODevice::ReadOnly));
    QCOMPARE(settings.readAll(), different);
    settings.close();
    QVERIFY(settings.open(QIODevice::WriteOnly | QIODevice::Truncate));
    settings.write("invalid");
    settings.close();
    const ClientCommandResult invalid = client.claudeApply();
    QVERIFY(!invalid.succeeded);
    QCOMPARE(invalid.claude.parseState, QString("invalid"));
    QCOMPARE(invalid.errors.first().code, QString("settings_invalid"));

    ClientCommandResult codex = client.codexStatus();
    QVERIFY(codex.succeeded);
    QCOMPARE(codex.codex.configSyncState, QString("absent"));
    QDir().mkpath(directory.filePath(".codex"));
    QFile codexConfig(directory.filePath(".codex/config.toml"));
    QVERIFY(codexConfig.open(QIODevice::WriteOnly));
    codexConfig.write("model = \"alpha/model\"\nmodel_provider = \"different\"\n");
    codexConfig.close();
    codex = client.codexStatus();
    QVERIFY(codex.succeeded);
    QCOMPARE(codex.codex.configSyncState, QString("different"));
    QCOMPARE(codex.codex.sourceTag, QString("OneLLMRouter"));
    QVERIFY(codexConfig.open(QIODevice::WriteOnly | QIODevice::Truncate));
    codexConfig.write(QString(
        "model = \"alpha/model\"\nmodel_provider = \"onellm\"\n"
        "model_catalog_json = \"%1\"\n\n[model_providers.onellm]\n"
        "name = \"OneLLMRouter\"\nbase_url = \"http://localhost:3456/openai/v1\"\n"
        "wire_api = \"responses\"\nrequires_openai_auth = true\n")
        .arg(directory.filePath(".onellm/model-catalog.json").replace("\\", "\\\\"))
        .toUtf8());
    codexConfig.close();
    QCOMPARE(client.codexStatus().codex.configSyncState, QString("current"));
    QVERIFY(codexConfig.open(QIODevice::WriteOnly | QIODevice::Truncate));
    codexConfig.write("invalid = [");
    codexConfig.close();
    QCOMPARE(client.codexStatus().codex.configSyncState, QString("invalid"));
    codex = client.codexPreview("alpha/model");
    QVERIFY(codex.succeeded);
    QVERIFY(codex.codex.snippet.contains("model = \"alpha/model\""));
    QVERIFY(!codex.codex.configWriteSupported);
    codex = client.codexCatalogApply();
    QVERIFY(codex.succeeded);
    QCOMPARE(QDir::fromNativeSeparators(codex.codex.writtenPaths.first()),
             directory.filePath(".onellm/model-catalog.json"));
    QVERIFY(QFile::exists(directory.filePath(".onellm/model-catalog.json")));
    QVERIFY(!QFile::exists(directory.filePath(".codex/model-catalog.json")));
}

void ConfigClientTest::discoversProtocolModels_data()
{
    QTest::addColumn<ModelProtocol>("protocol");
    QTest::addColumn<QString>("path");
    QTest::addColumn<QByteArray>("authHeader");
    QTest::newRow("anthropic") << ModelProtocol::Anthropic << QString("/models")
                                << QByteArray("x-api-key: replacement-key");
    QTest::newRow("openai-chat") << ModelProtocol::OpenAIChat << QString("/models")
                                  << QByteArray("authorization: Bearer replacement-key");
    QTest::newRow("openai-responses") << ModelProtocol::OpenAIResponses << QString("/v1/models")
                                       << QByteArray("authorization: Bearer replacement-key");
}

void ConfigClientTest::discoversProtocolModels()
{
    QFETCH(ModelProtocol, protocol);
    QFETCH(QString, path);
    QFETCH(QByteArray, authHeader);
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
            const QByteArray body = "{\"data\":[{\"id\":\"model-b\"},{\"id\":\"model-a\"}]}";
            socket->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "
                          + QByteArray::number(body.size()) + "\r\nConnection: close\r\n\r\n" + body);
            socket->disconnectFromHost();
        });
    });
    const QString base = QString("http://127.0.0.1:%1").arg(server.serverPort());
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    const QString configPath = directory.filePath("router.yaml");
    QFile config(configPath);
    QVERIFY(config.open(QIODevice::WriteOnly));
    const QByteArray original = QString(
        "server:\n  host: 127.0.0.1\n  http_port: 3456\nproviders:\n"
        "  - name: Alpha\n    prefix: alpha\n    base_url: %1\n"
        "    openai_base_url: %1\n    responses_base_url: %1\n"
        "    api_key: replacement-key\n    proxy: false\n"
        "    models:\n      - {id: configured-model, endpoints: [anthropic]}\n"
        "codex:\n  models: {}\nmodel_slots: {}\n")
        .arg(base).toUtf8();
    config.write(original);
    config.close();
    ConfigClient client(core, configPath);
    ConfigSnapshot snapshot;
    QVERIFY(client.load(&snapshot).succeeded);
    QSignalSpy success(&client, &ConfigClient::modelsDiscovered);
    QSignalSpy failure(&client, &ConfigClient::discoveryFailed);
    client.discoverModels(snapshot, 0, protocol);
    QVERIFY(success.wait(3000));
    QCOMPARE(failure.count(), 0);
    const QList<QVariant> result = success.takeFirst();
    QCOMPARE(result.at(0).toString(), QString("alpha"));
    QCOMPARE(result.at(1).toStringList(), QStringList({"model-a", "model-b"}));
    const QByteArray lowerRequest = request.toLower();
    QVERIFY(lowerRequest.startsWith("get " + path.toUtf8().toLower() + " http/1.1"));
    QVERIFY(lowerRequest.contains(authHeader.toLower()));
    QFile unchanged(configPath);
    QVERIFY(unchanged.open(QIODevice::ReadOnly));
    QCOMPARE(unchanged.readAll(), original);
}

QTEST_MAIN(ConfigClientTest)
#include "config_client_test.moc"
