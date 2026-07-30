#include <QtTest>

#include "i18n.h"
#include "tray_application.h"

class TrayApplicationTest : public QObject
{
    Q_OBJECT

private slots:
    void quotesAutoStartCommand();
    void usesDedicatedAutoStartValue();
    void externalPolicyHasNoDestructiveActions();
    void selectsEnglishAndChineseStrings();
    void rateLimitsIdenticalNotifications();
    void rebuildsMenuWhenAboutToShow();
};

void TrayApplicationTest::quotesAutoStartCommand()
{
    const QString executable =
        QDir::toNativeSeparators("C:/Program Files/OneLLM/tray.exe");
    const QString config =
        QDir::toNativeSeparators("C:/Users/Test User/router.yaml");
    QCOMPARE(autoStartCommand(executable, config),
             QString("\"%1\" --config \"%2\"").arg(executable, config));
}

void TrayApplicationTest::usesDedicatedAutoStartValue()
{
    QCOMPARE(autoStartValueName(), QString("OneLLMRouter Desktop"));
}

void TrayApplicationTest::externalPolicyHasNoDestructiveActions()
{
    const TrayActionPolicy policy =
        trayActionPolicy(ProcessOwnership::External, RouterState::Healthy);
    QVERIFY(policy.externalManaged);
    QVERIFY(!policy.start);
    QVERIFY(!policy.stop);
    QVERIFY(!policy.restart);
}

void TrayApplicationTest::selectsEnglishAndChineseStrings()
{
    QCOMPARE(stringsForLocale(QLocale(QLocale::English)).quit, QString("Quit"));
    QCOMPARE(stringsForLocale(QLocale(QLocale::Chinese)).quit,
             QString::fromUtf8("退出"));
}

void TrayApplicationTest::rateLimitsIdenticalNotifications()
{
    NotificationLimiter limiter;
    const QDateTime now = QDateTime::fromSecsSinceEpoch(1000);
    QVERIFY(limiter.shouldNotify("degraded", now));
    QVERIFY(!limiter.shouldNotify("degraded", now.addSecs(299)));
    QVERIFY(limiter.shouldNotify("conflict", now.addSecs(10)));
    QVERIFY(limiter.shouldNotify("degraded", now.addSecs(300)));
}

void TrayApplicationTest::rebuildsMenuWhenAboutToShow()
{
    TrayApplication tray("C:/tmp/router.yaml", false);
    QVERIFY(QMetaObject::invokeMethod(tray.menu(), "aboutToShow",
                                      Qt::DirectConnection));

    bool foundStart = false;
    for (QAction *action : tray.menu()->actions()) {
        foundStart = foundStart || action->objectName() == "startRouter";
    }
    QVERIFY(foundStart);
}

QTEST_MAIN(TrayApplicationTest)
#include "tray_application_test.moc"
