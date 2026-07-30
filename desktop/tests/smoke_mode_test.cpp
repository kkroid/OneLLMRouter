#include <QtTest>

#include "smoke_mode.h"

class SmokeModeTest : public QObject
{
    Q_OBJECT
private slots:
    void buildsOwnedHealthyResult();
    void leavesObservationWindowBeforeShutdown();
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

QTEST_APPLESS_MAIN(SmokeModeTest)
#include "smoke_mode_test.moc"
