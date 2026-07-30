#include <QtTest>
#include <QTcpServer>
#include <QTcpSocket>

#include "router_discovery.h"

class RouterDiscoveryTest : public QObject
{
    Q_OBJECT

private slots:
    void buildsConfigInfoArgumentsWithoutShell();
    void parsesSecretFreeConfigInfo();
    void rejectsUnknownConfigFields();
    void rejectsMalformedConfigInfo();
    void parsesExpectedRouterHealth();
    void rejectsWrongHealthIdentity();
    void classifiesExternalConflictAndAbsent();
    void redirectIsConflict();
    void configInfoTimeoutReportsFailure();
    void overlappingDiscoveryDoesNotCreateSecondProbe();
};

void RouterDiscoveryTest::buildsConfigInfoArgumentsWithoutShell()
{
    QCOMPARE(configInfoArguments("C:/tmp/router.yaml"),
             QStringList({"--config", "C:/tmp/router.yaml", "config-info", "--json"}));
}

void RouterDiscoveryTest::parsesSecretFreeConfigInfo()
{
    const auto config = parseRouterConfigInfo(R"({
        "service":"onellm-router",
        "config_path":"C:/tmp/router.yaml",
        "host":"127.0.0.1",
        "http_port":45678,
        "log_dir":"C:/tmp/logs",
        "proxy_socks5":"127.0.0.1:1082",
        "bell":true,
        "onellm_catalog_path":"C:/tmp/model-catalog.json",
        "codex_catalog_path":"C:/tmp/codex-catalog.json"
    })");

    QVERIFY(config.valid);
    QCOMPARE(config.port, 45678);
    QCOMPARE(config.proxySocks5, QString("127.0.0.1:1082"));
}

void RouterDiscoveryTest::rejectsUnknownConfigFields()
{
    const auto config = parseRouterConfigInfo(R"({
        "service":"onellm-router",
        "config_path":"C:/tmp/router.yaml",
        "host":"127.0.0.1",
        "http_port":45678,
        "log_dir":"C:/tmp/logs",
        "proxy_socks5":"",
        "bell":false,
        "onellm_catalog_path":"C:/tmp/a.json",
        "codex_catalog_path":"C:/tmp/b.json",
        "api_key":"must-not-be-consumed"
    })");
    QVERIFY(!config.valid);
}

void RouterDiscoveryTest::rejectsMalformedConfigInfo()
{
    QVERIFY(!parseRouterConfigInfo(R"({"service":"onellm-router","http_port":"45678"})").valid);
    QVERIFY(!parseRouterConfigInfo("{").valid);
}

void RouterDiscoveryTest::parsesExpectedRouterHealth()
{
    const auto health = parseRouterHealth(R"({
        "status":"ok","service":"onellm-router","pid":42,"version":"1.4.0",
        "http_port":45678,"models":2,"copilot_token":true,
        "proxy_socks5":"127.0.0.1:1082"
    })");

    QVERIFY(health.valid);
    QCOMPARE(health.service, QString("onellm-router"));
    QCOMPARE(health.pid, 42);
    QCOMPARE(health.proxySocks5, QString("127.0.0.1:1082"));
}

void RouterDiscoveryTest::rejectsWrongHealthIdentity()
{
    QVERIFY(!parseRouterHealth(R"({"status":"ok","service":"other"})").valid);
    QVERIFY(!parseRouterHealth(R"({"status":"down","service":"onellm-router"})").valid);
}

void RouterDiscoveryTest::classifiesExternalConflictAndAbsent()
{
    RouterHealth valid;
    valid.valid = true;
    QCOMPARE(classifyHealthProbe(ProbeTransport::HttpResponse, valid),
             DiscoveryClassification::External);
    QCOMPARE(classifyHealthProbe(ProbeTransport::HttpResponse, {}),
             DiscoveryClassification::Conflict);
    QCOMPARE(classifyHealthProbe(ProbeTransport::ConnectionRefused, {}),
             DiscoveryClassification::Absent);
    QCOMPARE(classifyHealthProbe(ProbeTransport::Unreachable, {}),
             DiscoveryClassification::Absent);
}

static QString fixturePath()
{
    return QDir(QCoreApplication::applicationDirPath())
        .filePath("test_core_fixture.exe");
}

static quint16 safeDynamicPort(QTcpServer &server)
{
    while (server.listen(QHostAddress::LocalHost, 0)) {
        const quint16 port = server.serverPort();
        if (port != 3456 && port != 3457) return port;
        server.close();
    }
    return 0;
}

void RouterDiscoveryTest::redirectIsConflict()
{
    QTcpServer server;
    const quint16 port = safeDynamicPort(server);
    QVERIFY(port != 0);
    connect(&server, &QTcpServer::newConnection, &server, [&server] {
        QTcpSocket *socket = server.nextPendingConnection();
        connect(socket, &QTcpSocket::readyRead, socket, [socket] {
            socket->readAll();
            socket->write("HTTP/1.1 302 Found\r\nLocation: http://example.com/\r\n"
                          "Content-Length: 0\r\nConnection: close\r\n\r\n");
            socket->disconnectFromHost();
        });
    });
    RouterDiscovery discovery(fixturePath(), QString::number(port), 1000);
    QSignalSpy conflict(&discovery, &RouterDiscovery::portConflict);
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(conflict.count(), 1, 3000);
}

void RouterDiscoveryTest::configInfoTimeoutReportsFailure()
{
    RouterDiscovery discovery(fixturePath(), "hang", 50);
    QSignalSpy failed(&discovery, &RouterDiscovery::configFailed);
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(failed.count(), 1, 1000);
}

void RouterDiscoveryTest::overlappingDiscoveryDoesNotCreateSecondProbe()
{
    QTcpServer server;
    const quint16 port = safeDynamicPort(server);
    QVERIFY(port != 0);
    int requests = 0;
    connect(&server, &QTcpServer::newConnection, &server, [&] {
        ++requests;
        QTcpSocket *socket = server.nextPendingConnection();
        connect(socket, &QTcpSocket::readyRead, socket, [socket, port] {
            socket->readAll();
            const QByteArray body = QString(
                R"({"status":"ok","service":"onellm-router","pid":1,"version":"1.4.0","http_port":%1,"models":0,"copilot_token":false})")
                                        .arg(port).toUtf8();
            QTimer::singleShot(100, socket, [socket, body] {
                socket->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                              "Content-Length: " + QByteArray::number(body.size()) +
                              "\r\nConnection: close\r\n\r\n" + body);
                socket->disconnectFromHost();
            });
        });
    });
    RouterDiscovery discovery(fixturePath(), QString::number(port), 1000);
    QSignalSpy external(&discovery, &RouterDiscovery::externalRouterFound);
    discovery.discover();
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(external.count(), 1, 3000);
    QCOMPARE(requests, 1);
}

QTEST_GUILESS_MAIN(RouterDiscoveryTest)
#include "router_discovery_test.moc"
