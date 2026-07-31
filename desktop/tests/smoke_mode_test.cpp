#include <QtTest>

#include "smoke_mode.h"

class SmokeModeTest : public QObject
{
    Q_OBJECT
private slots:
    void buildsOwnedHealthyResult();
    void leavesObservationWindowBeforeShutdown();
    void requiresRequestedNormalCoreExitForSuccess();
};

void SmokeModeTest::buildsOwnedHealthyResult()
{
    const QJsonObject result = buildSmokeResult(42, 45678);
    QCOMPARE(result.keys().size(), 5);
    QCOMPARE(result.value("service").toString(), QString("onellm-router"));
    QCOMPARE(result.value("ownership").toString(), QString("owned"));
    QVERIFY(result.value("healthy").toBool());
    QCOMPARE(result.value("pid").toInt(), 42);
    QCOMPARE(result.value("port").toInt(), 45678);
}

void SmokeModeTest::leavesObservationWindowBeforeShutdown()
{
    QVERIFY(smokeObservationDelayMs() >= 500);
}

void SmokeModeTest::requiresRequestedNormalCoreExitForSuccess()
{
    QVERIFY(smokeCoreExitIsSuccessful(true, true, 0,
                                      QProcess::NormalExit));
    QVERIFY(!smokeCoreExitIsSuccessful(true, false, 0,
                                       QProcess::NormalExit));
    QVERIFY(!smokeCoreExitIsSuccessful(false, true, 0,
                                       QProcess::NormalExit));
    QVERIFY(!smokeCoreExitIsSuccessful(true, true, 1,
                                       QProcess::NormalExit));
    QVERIFY(!smokeCoreExitIsSuccessful(true, true, 0,
                                       QProcess::CrashExit));
}

QTEST_APPLESS_MAIN(SmokeModeTest)
#include "smoke_mode_test.moc"
