#include <QtTest>
#include <QJsonDocument>
#include <QJsonObject>
#include <QTcpServer>
#include <QTcpSocket>

#include "router_discovery.h"

class RouterDiscoveryTest : public QObject
{
    Q_OBJECT

private slots:
    void buildsConfigInfoArgumentsWithoutShell();
    void localHealthProbeBypassesSystemProxy();
    void parsesSecretFreeConfigInfo();
    void rejectsUnknownConfigFields();
    void rejectsMalformedConfigInfo();
    void parsesExpectedRouterHealth();
    void matchesHealthToSelectedConfig();
    void rejectsWrongHealthIdentity();
    void classifiesExternalConflictAndAbsent();
    void redirectIsConflict();
    void closedDynamicPortIsAbsent();
    void configInfoTimeoutReportsFailure();
    void configInfoTimeoutCanRetryAfterProcessFinishes();
    void samePortDifferentConfigIsConflict();
    void overlappingDiscoveryDoesNotCreateSecondProbe();
};

void RouterDiscoveryTest::buildsConfigInfoArgumentsWithoutShell()
{
    QCOMPARE(configInfoArguments("C:/tmp/router.yaml"),
             QStringList({"--config", "C:/tmp/router.yaml", "config-info", "--json"}));
}

void RouterDiscoveryTest::localHealthProbeBypassesSystemProxy()
{
    QCOMPARE(localHealthProxy().type(), QNetworkProxy::NoProxy);
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
        "http_port":45678,"models":2,"config_path":"C:/tmp/router.yaml",
        "proxy_socks5":"127.0.0.1:1082"
    })");

    QVERIFY(health.valid);
    QCOMPARE(health.service, QString("onellm-router"));
    QCOMPARE(health.pid, 42);
    QCOMPARE(health.configPath, QString("C:/tmp/router.yaml"));
    QCOMPARE(health.proxySocks5, QString("127.0.0.1:1082"));
}

void RouterDiscoveryTest::matchesHealthToSelectedConfig()
{
    RouterConfigInfo config;
    config.valid = true;
    config.configPath = "C:/tmp/router.yaml";
    config.port = 45678;
    RouterHealth health;
    health.valid = true;
    health.configPath = "c:/TMP/router.yaml";
    health.port = 45678;

    QVERIFY(healthMatchesConfig(health, config));
    health.configPath = "C:/tmp/other.yaml";
    QVERIFY(!healthMatchesConfig(health, config));
    health.configPath = config.configPath;
    health.port++;
    QVERIFY(!healthMatchesConfig(health, config));
    health.port = config.port;
    health.valid = false;
    QVERIFY(!healthMatchesConfig(health, config));
}

void RouterDiscoveryTest::rejectsWrongHealthIdentity()
{
    QVERIFY(!parseRouterHealth(R"({"status":"ok","service":"other"})").valid);
    QVERIFY(!parseRouterHealth(R"({"status":"down","service":"onellm-router"})").valid);
    QVERIFY(!parseRouterHealth(R"({
        "status":"ok","service":"onellm-router","pid":42,"version":"1.4.0",
        "http_port":45678,"models":2,"config_path":""
    })").valid);
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
             DiscoveryClassification::Conflict);
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

void RouterDiscoveryTest::closedDynamicPortIsAbsent()
{
    QTcpServer server;
    const quint16 port = safeDynamicPort(server);
    QVERIFY(port != 0);
    server.close();
    RouterDiscovery discovery(fixturePath(), QString::number(port), 500);
    QSignalSpy absent(&discovery, &RouterDiscovery::routerAbsent);
    QSignalSpy conflict(&discovery, &RouterDiscovery::portConflict);
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(absent.count(), 1, 2000);
    QCOMPARE(conflict.count(), 0);
}

void RouterDiscoveryTest::configInfoTimeoutReportsFailure()
{
    RouterDiscovery discovery(fixturePath(), "hang", 50);
    QSignalSpy failed(&discovery, &RouterDiscovery::configFailed);
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(failed.count(), 1, 1000);
}

void RouterDiscoveryTest::configInfoTimeoutCanRetryAfterProcessFinishes()
{
    RouterDiscovery discovery(fixturePath(), "hang", 50);
    QSignalSpy failed(&discovery, &RouterDiscovery::configFailed);
    QElapsedTimer elapsed;
    qint64 firstFailureMs = -1;
    connect(&discovery, &RouterDiscovery::configFailed, &discovery,
            [&] {
                if (firstFailureMs < 0) {
                    firstFailureMs = elapsed.elapsed();
                    discovery.discover();
                }
            });
    elapsed.start();
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(failed.count(), 2, 1000);
    QVERIFY(elapsed.elapsed() - firstFailureMs >= 30);
}

void RouterDiscoveryTest::samePortDifferentConfigIsConflict()
{
    QTcpServer server;
    const quint16 port = safeDynamicPort(server);
    QVERIFY(port != 0);
    const QString configPath =
        QDir::temp().absoluteFilePath(QString::number(port));
    const QByteArray body = QJsonDocument(QJsonObject{
        {"status", "ok"},
        {"service", "onellm-router"},
        {"pid", 1},
        {"version", "1.4.0"},
        {"http_port", port},
        {"models", 0},
        {"config_path", QDir::temp().absoluteFilePath("other-router.yaml")},
    }).toJson(QJsonDocument::Compact);
    connect(&server, &QTcpServer::newConnection, &server, [&server, body] {
        QTcpSocket *socket = server.nextPendingConnection();
        connect(socket, &QTcpSocket::readyRead, socket, [socket, body] {
            socket->readAll();
            socket->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                          "Content-Length: " + QByteArray::number(body.size()) +
                          "\r\nConnection: close\r\n\r\n" + body);
            socket->disconnectFromHost();
        });
    });

    RouterDiscovery discovery(fixturePath(), configPath, 1000);
    QSignalSpy conflict(&discovery, &RouterDiscovery::portConflict);
    QSignalSpy external(&discovery, &RouterDiscovery::externalRouterFound);
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(conflict.count(), 1, 3000);
    QCOMPARE(external.count(), 0);
}

void RouterDiscoveryTest::overlappingDiscoveryDoesNotCreateSecondProbe()
{
    QTcpServer server;
    const quint16 port = safeDynamicPort(server);
    QVERIFY(port != 0);
    const QString configPath =
        QDir::temp().absoluteFilePath(QString::number(port));
    int requests = 0;
    connect(&server, &QTcpServer::newConnection, &server, [&, configPath] {
        QTcpSocket *socket = server.nextPendingConnection();
        connect(socket, &QTcpSocket::readyRead, socket,
                [&, socket, port, configPath] {
            ++requests;
            socket->readAll();
            const QByteArray body = QJsonDocument(QJsonObject{
                {"status", "ok"},
                {"service", "onellm-router"},
                {"pid", 1},
                {"version", "1.4.0"},
                {"http_port", port},
                {"models", 0},
                {"config_path", configPath},
            }).toJson(QJsonDocument::Compact);
            QTimer::singleShot(100, socket, [socket, body] {
                socket->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                              "Content-Length: " + QByteArray::number(body.size()) +
                              "\r\nConnection: close\r\n\r\n" + body);
                socket->disconnectFromHost();
            });
        });
    });
    RouterDiscovery discovery(fixturePath(), configPath, 1000);
    QSignalSpy external(&discovery, &RouterDiscovery::externalRouterFound);
    discovery.discover();
    discovery.discover();
    QTRY_COMPARE_WITH_TIMEOUT(external.count(), 1, 3000);
    QCOMPARE(requests, 1);
}

QTEST_GUILESS_MAIN(RouterDiscoveryTest)
#include "router_discovery_test.moc"
