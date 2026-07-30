#include <QtTest>

#include "proxy_probe.h"

class ProxyProbeTest : public QObject
{
    Q_OBJECT

private slots:
    void parsesLoopbackEndpoints_data();
    void parsesLoopbackEndpoints();
    void rejectsInvalidOrRemoteEndpoints_data();
    void rejectsInvalidOrRemoteEndpoints();
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

QTEST_APPLESS_MAIN(ProxyProbeTest)
#include "proxy_probe_test.moc"
