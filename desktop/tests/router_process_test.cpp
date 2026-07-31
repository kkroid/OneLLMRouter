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
    void startFailureNeverBecomesOwned();
    void restartStartsOnlyAfterFinished();
    void explicitStopCancelsPendingRestart();
    void stopTimeoutDoesNotForceTermination();
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

static QString processFixturePath()
{
    return QDir(QCoreApplication::applicationDirPath())
        .filePath("test_core_fixture.exe");
}

void RouterProcessTest::startFailureNeverBecomesOwned()
{
    RouterProcess process("C:/missing/onellm-router-core.exe", 50);
    QSignalSpy error(&process, &RouterProcess::processError);
    QVERIFY(process.startOwned("C:/tmp/config"));
    QTRY_COMPARE_WITH_TIMEOUT(error.count(), 1, 1000);
    QCOMPARE(process.ownership(), ProcessOwnership::None);
}

void RouterProcessTest::restartStartsOnlyAfterFinished()
{
    RouterProcess process(processFixturePath(), 1000);
    QSignalSpy started(&process, &RouterProcess::processStarted);
    QSignalSpy finished(&process, &RouterProcess::processFinished);
    QVERIFY(process.startOwned("C:/tmp/normal"));
    QTRY_COMPARE_WITH_TIMEOUT(started.count(), 1, 1000);
    QVERIFY(process.restart());
    QTRY_COMPARE_WITH_TIMEOUT(finished.count(), 1, 1000);
    QTRY_COMPARE_WITH_TIMEOUT(started.count(), 2, 1000);
    QVERIFY(process.requestGracefulStop());
    QTRY_COMPARE_WITH_TIMEOUT(finished.count(), 2, 1000);
}

void RouterProcessTest::explicitStopCancelsPendingRestart()
{
    RouterProcess process(processFixturePath(), 1000);
    QSignalSpy started(&process, &RouterProcess::processStarted);
    QSignalSpy finished(&process, &RouterProcess::processFinished);
    QVERIFY(process.startOwned("C:/tmp/slow"));
    QTRY_COMPARE_WITH_TIMEOUT(started.count(), 1, 1000);
    QVERIFY(process.restart());
    QVERIFY(process.requestGracefulStop());
    QTRY_COMPARE_WITH_TIMEOUT(finished.count(), 1, 1000);
    QTest::qWait(300);
    QCOMPARE(started.count(), 1);
    QCOMPARE(process.ownership(), ProcessOwnership::None);
}

void RouterProcessTest::stopTimeoutDoesNotForceTermination()
{
    RouterProcess process(processFixturePath(), 30);
    QSignalSpy started(&process, &RouterProcess::processStarted);
    QSignalSpy timeout(&process, &RouterProcess::gracefulStopTimedOut);
    QSignalSpy finished(&process, &RouterProcess::processFinished);
    QVERIFY(process.startOwned("C:/tmp/slow"));
    QTRY_COMPARE_WITH_TIMEOUT(started.count(), 1, 1000);
    QVERIFY(process.requestGracefulStop());
    QTRY_COMPARE_WITH_TIMEOUT(timeout.count(), 1, 500);
    QCOMPARE(finished.count(), 0);
    QTRY_COMPARE_WITH_TIMEOUT(finished.count(), 1, 1000);
}

QTEST_GUILESS_MAIN(RouterProcessTest)
#include "router_process_test.moc"
