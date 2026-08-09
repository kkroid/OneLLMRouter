#include <QtTest>
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
    void discoversProtocolModels_data();
    void discoversProtocolModels();
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
               "    api_key: old-secret\n    models: [old]\ncodex:\n  models: {}\nmodel_slots: {}\n");
    file.close();

    ConfigClient client(core, path);
    ConfigSnapshot snapshot;
    QVERIFY2(client.load(&snapshot).succeeded, "config-get failed");
    QCOMPARE(snapshot.providers.size(), 1);
    QVERIFY(snapshot.providers[0].apiKeySet);
    snapshot.providers[0].name = "Updated";
    snapshot.providers[0].models = {"new-model"};
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
    ProviderConfigSnapshot provider;
    provider.baseUrl = base;
    provider.openAIBaseUrl = base;
    provider.responsesBaseUrl = base;
    ConfigClient client("unused", "unused");
    QSignalSpy success(&client, &ConfigClient::modelsDiscovered);
    QSignalSpy failure(&client, &ConfigClient::discoveryFailed);
    client.discoverModels(provider, protocol, "replacement-key");
    QVERIFY(success.wait(3000));
    QCOMPARE(failure.count(), 0);
    const QList<QVariant> result = success.takeFirst();
    QCOMPARE(result.at(0).toString(), QString());
    QCOMPARE(result.at(1).toStringList(), QStringList({"model-a", "model-b"}));
    const QByteArray lowerRequest = request.toLower();
    QVERIFY(lowerRequest.startsWith("get " + path.toUtf8().toLower() + " http/1.1"));
    QVERIFY(lowerRequest.contains(authHeader.toLower()));
}

QTEST_MAIN(ConfigClientTest)
#include "config_client_test.moc"
