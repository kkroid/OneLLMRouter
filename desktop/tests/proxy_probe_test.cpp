#include <QtTest>
#include <QTcpServer>

#include "proxy_probe.h"

class ProxyProbeTest : public QObject
{
    Q_OBJECT

private slots:
    void parsesLoopbackEndpoints_data();
    void parsesLoopbackEndpoints();
    void rejectsInvalidOrRemoteEndpoints_data();
    void rejectsInvalidOrRemoteEndpoints();
    void reportsReachableOnDynamicLoopbackListener();
    void reportsUnreachableAfterDynamicListenerCloses();
};

void ProxyProbeTest::parsesLoopbackEndpoints_data()
{
    QTest::addColumn<QString>("address");
    QTest::addColumn<QString>("host");
    QTest::addColumn<int>("port");

    QTest::newRow("ipv4") << "127.0.0.1:1082" << "127.0.0.1" << 1082;
    QTest::newRow("localhost") << "localhost:8080" << "localhost" << 8080;
    QTest::newRow("ipv6") << "[::1]:1082" << "::1" << 1082;
}

void ProxyProbeTest::parsesLoopbackEndpoints()
{
    QFETCH(QString, address);
    QFETCH(QString, host);
    QFETCH(int, port);

    const auto endpoint = parseProxyEndpoint(address);
    QVERIFY(endpoint.has_value());
    QCOMPARE(endpoint->host, host);
    QCOMPARE(endpoint->port, quint16(port));
}

void ProxyProbeTest::rejectsInvalidOrRemoteEndpoints_data()
{
    QTest::addColumn<QString>("address");
    QTest::newRow("empty") << "";
    QTest::newRow("missing port") << "127.0.0.1";
    QTest::newRow("zero") << "127.0.0.1:0";
    QTest::newRow("large") << "127.0.0.1:70000";
    QTest::newRow("text port") << "127.0.0.1:http";
    QTest::newRow("remote ipv4") << "8.8.8.8:53";
    QTest::newRow("remote host") << "example.com:443";
    QTest::newRow("bare ipv6") << "::1:1082";
}

void ProxyProbeTest::rejectsInvalidOrRemoteEndpoints()
{
    QFETCH(QString, address);
    QVERIFY(!parseProxyEndpoint(address).has_value());
}

static quint16 dynamicProxyPort(QTcpServer &server)
{
    while (server.listen(QHostAddress::LocalHost, 0)) {
        const quint16 port = server.serverPort();
        if (port != 3456 && port != 3457) return port;
        server.close();
    }
    return 0;
}

void ProxyProbeTest::reportsReachableOnDynamicLoopbackListener()
{
    QTcpServer server;
    const quint16 port = dynamicProxyPort(server);
    QVERIFY(port != 0);
    ProxyProbe probe;
    QSignalSpy finished(&probe, &ProxyProbe::probeFinished);
    probe.probe(QString("127.0.0.1:%1").arg(port));
    QTRY_COMPARE_WITH_TIMEOUT(finished.count(), 1, 1000);
    QVERIFY(finished.takeFirst().at(1).toBool());
}

void ProxyProbeTest::reportsUnreachableAfterDynamicListenerCloses()
{
    QTcpServer server;
    const quint16 port = dynamicProxyPort(server);
    QVERIFY(port != 0);
    server.close();
    ProxyProbe probe;
    QSignalSpy finished(&probe, &ProxyProbe::probeFinished);
    probe.probe(QString("127.0.0.1:%1").arg(port));
    QTRY_COMPARE_WITH_TIMEOUT(finished.count(), 1, 1000);
    QVERIFY(!finished.takeFirst().at(1).toBool());
}

QTEST_GUILESS_MAIN(ProxyProbeTest)
#include "proxy_probe_test.moc"
