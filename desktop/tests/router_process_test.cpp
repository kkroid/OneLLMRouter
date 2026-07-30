#include <QtTest>

#include "router_process.h"

class RouterProcessTest : public QObject
{
    Q_OBJECT

private slots:
    void usesExplicitTrayChildArguments();
    void usesNarrowShutdownProtocol();
    void externalInstanceIsReadOnlyAndCreatesNoProcess();
    void absentInstanceCannotBeControlled();
};

void RouterProcessTest::usesExplicitTrayChildArguments()
{
    const QString config = "C:/Users/test/.onellm/onellm-router.yaml";
    QCOMPARE(routerChildArguments(config),
             QStringList({"serve", "--tray-child", "--config", config}));
}

void RouterProcessTest::usesNarrowShutdownProtocol()
{
    QCOMPARE(gracefulShutdownCommand(), QByteArray("shutdown\n"));
}

void RouterProcessTest::externalInstanceIsReadOnlyAndCreatesNoProcess()
{
    RouterProcess process;
    RouterHealth health;
    health.valid = true;

    QVERIFY(process.attachExternal(health));
    QCOMPARE(process.ownership(), ProcessOwnership::External);
    QVERIFY(!process.hasChildProcess());
    QVERIFY(!process.startOwned("C:/tmp/router.yaml"));
    QVERIFY(!process.requestGracefulStop());
    QVERIFY(!process.restart());
}

void RouterProcessTest::absentInstanceCannotBeControlled()
{
    RouterProcess process;
    QCOMPARE(process.ownership(), ProcessOwnership::None);
    QVERIFY(!process.requestGracefulStop());
    QVERIFY(!process.restart());
}

QTEST_APPLESS_MAIN(RouterProcessTest)
#include "router_process_test.moc"
