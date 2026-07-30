#include <QtTest>

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

QTEST_APPLESS_MAIN(RouterDiscoveryTest)
#include "router_discovery_test.moc"
