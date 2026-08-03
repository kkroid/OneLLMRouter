#include <QtTest>
#include <QSettings>
#include <QTemporaryDir>

#include "i18n.h"
#include "tray_application.h"

class TrayApplicationTest : public QObject
{
    Q_OBJECT

private slots:
    void quotesAutoStartCommand();
    void quotesApplicationRestartArguments();
    void usesDedicatedAutoStartValue();
    void migratesLegacyAutoStartValue();
    void disablingAutoStartRemovesCurrentAndLegacyValues();
    void externalPolicyHasNoDestructiveActions();
    void conflictPolicyHasNoDestructiveActions();
    void explicitStopDisablesAutomaticRestart();
    void explicitStopSuppressesAutoStartWhenStopRequestFails();
    void ownedHealthMustMatchChildPid();
    void selectsEnglishAndChineseStrings();
    void distinguishesUnknownAndDisabledProxy();
    void rateLimitsIdenticalNotifications();
    void rebuildsMenuWhenAboutToShow();
    void detachedExternalBecomesControllableStoppedState();
    void loadsEmbeddedStatusIcons();
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

void TrayApplicationTest::quotesApplicationRestartArguments()
{
    const QString config =
        QDir::toNativeSeparators("C:/Users/Test User/router.yaml");
    QCOMPARE(applicationRestartArguments(config),
             QString("--config \"%1\"").arg(config));
}

void TrayApplicationTest::usesDedicatedAutoStartValue()
{
    QCOMPARE(autoStartValueName(), QString("OneLLMRouter Desktop"));
    QCOMPARE(legacyAutoStartValueName(), QString("OneLLMRouter"));
}

void TrayApplicationTest::migratesLegacyAutoStartValue()
{
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    QSettings settings(directory.filePath("autostart.ini"),
                       QSettings::IniFormat);
    settings.setValue(legacyAutoStartValueName(), "old portable command");

    const QString currentCommand = "current desktop command";
    QVERIFY(migrateLegacyAutoStart(settings, currentCommand));
    QVERIFY(!settings.contains(legacyAutoStartValueName()));
    QCOMPARE(settings.value(autoStartValueName()).toString(), currentCommand);
    QVERIFY(!migrateLegacyAutoStart(settings, currentCommand));
}

void TrayApplicationTest::disablingAutoStartRemovesCurrentAndLegacyValues()
{
    QTemporaryDir directory;
    QVERIFY(directory.isValid());
    QSettings settings(directory.filePath("autostart.ini"),
                       QSettings::IniFormat);
    settings.setValue(autoStartValueName(), "current desktop command");
    settings.setValue(legacyAutoStartValueName(), "old portable command");

    configureAutoStart(settings, false, {});
    QVERIFY(!settings.contains(autoStartValueName()));
    QVERIFY(!settings.contains(legacyAutoStartValueName()));
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

void TrayApplicationTest::conflictPolicyHasNoDestructiveActions()
{
    const TrayActionPolicy policy =
        trayActionPolicy(ProcessOwnership::Owned, RouterState::Conflict);
    QVERIFY(!policy.stop);
    QVERIFY(!policy.restart);
}

void TrayApplicationTest::explicitStopDisablesAutomaticRestart()
{
    QVERIFY(shouldAutoStartRouter(ProcessOwnership::None, true));
    QVERIFY(!shouldAutoStartRouter(ProcessOwnership::None, false));
    QVERIFY(!shouldAutoStartRouter(ProcessOwnership::External, true));
}

void TrayApplicationTest::explicitStopSuppressesAutoStartWhenStopRequestFails()
{
    TrayApplication tray("C:/tmp/router.yaml", false);
    auto *discovery = tray.findChild<RouterDiscovery *>();
    auto *process = tray.findChild<RouterProcess *>();
    QVERIFY(discovery);
    QVERIFY(process);

    QVERIFY(QMetaObject::invokeMethod(&tray, "stopOwned",
                                      Qt::DirectConnection));
    discovery->routerAbsent({});
    QVERIFY(!process->hasChildProcess());
}

void TrayApplicationTest::ownedHealthMustMatchChildPid()
{
    RouterHealth health;
    health.valid = true;
    health.pid = 42;
    QVERIFY(healthMatchesOwnedProcess(ProcessOwnership::Owned, 42, health));
    QVERIFY(!healthMatchesOwnedProcess(ProcessOwnership::Owned, 43, health));
    QVERIFY(!healthMatchesOwnedProcess(ProcessOwnership::External, 42, health));
}

void TrayApplicationTest::selectsEnglishAndChineseStrings()
{
    const Strings english = stringsForLocale(QLocale(QLocale::English));
    QCOMPARE(english.quit, QString("Quit"));
    QCOMPARE(english.proxyUnknown, QString("Proxy: Unknown"));
    QCOMPARE(english.proxyDisabled, QString("Proxy: Not configured"));
    QCOMPARE(english.gracefulStopTimedOut,
             QString("Graceful stop timed out"));

    const Strings chinese = stringsForLocale(QLocale(QLocale::Chinese));
    QCOMPARE(chinese.quit, QString::fromUtf8("退出"));
    QCOMPARE(chinese.proxyUnknown, QString::fromUtf8("代理：未知"));
    QCOMPARE(chinese.proxyDisabled, QString::fromUtf8("代理：未配置"));
    QCOMPARE(chinese.gracefulStopTimedOut,
             QString::fromUtf8("优雅停止超时"));
}

void TrayApplicationTest::distinguishesUnknownAndDisabledProxy()
{
    TrayApplication tray("C:/tmp/router.yaml", false);
    auto *discovery = tray.findChild<RouterDiscovery *>();
    QVERIFY(discovery);
    QVERIFY(QMetaObject::invokeMethod(tray.menu(), "aboutToShow",
                                      Qt::DirectConnection));

    const Strings localized = stringsForLocale(QLocale::system());
    bool foundUnknownProxy = false;
    for (QAction *action : tray.menu()->actions()) {
        foundUnknownProxy = foundUnknownProxy ||
                            action->text() == localized.proxyUnknown;
    }
    QVERIFY(foundUnknownProxy);

    RouterConfigInfo config;
    config.valid = true;
    config.port = 3456;
    RouterHealth health;
    health.valid = true;
    health.pid = 42;
    discovery->externalRouterFound(config, health);
    QVERIFY(QMetaObject::invokeMethod(tray.menu(), "aboutToShow",
                                      Qt::DirectConnection));

    bool foundDisabledProxy = false;
    bool foundUnreachableProxy = false;
    for (QAction *action : tray.menu()->actions()) {
        foundDisabledProxy = foundDisabledProxy ||
                             action->text() == localized.proxyDisabled;
        foundUnreachableProxy = foundUnreachableProxy ||
                                action->text().contains(localized.unreachable);
    }
    QVERIFY(foundDisabledProxy);
    QVERIFY(!foundUnreachableProxy);

    discovery->configFailed("invalid configuration");
    QVERIFY(QMetaObject::invokeMethod(tray.menu(), "aboutToShow",
                                      Qt::DirectConnection));
    foundUnknownProxy = false;
    for (QAction *action : tray.menu()->actions()) {
        foundUnknownProxy = foundUnknownProxy ||
                            action->text() == localized.proxyUnknown;
    }
    QVERIFY(foundUnknownProxy);
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
    bool foundCopilot = false;
    for (QAction *action : tray.menu()->actions()) {
        foundStart = foundStart || action->objectName() == "startRouter";
        foundCopilot = foundCopilot ||
                       action->text().contains("Copilot", Qt::CaseInsensitive);
    }
    QVERIFY(foundStart);
    QVERIFY(!foundCopilot);
}

void TrayApplicationTest::detachedExternalBecomesControllableStoppedState()
{
    RouterProcess process;
    RouterHealth health;
    health.valid = true;
    QVERIFY(process.attachExternal(health));
    QVERIFY(process.detachExternal());
    QCOMPARE(process.ownership(), ProcessOwnership::None);
    QVERIFY(!process.health().valid);
    QVERIFY(trayActionPolicy(process.ownership(), RouterState::Stopped).start);
}

void TrayApplicationTest::loadsEmbeddedStatusIcons()
{
    QVERIFY(!QIcon(QStringLiteral(":/icons/green.ico")).isNull());
    QVERIFY(!QIcon(QStringLiteral(":/icons/yellow.ico")).isNull());
    QVERIFY(!QIcon(QStringLiteral(":/icons/red.ico")).isNull());
}

QTEST_MAIN(TrayApplicationTest)
#include "tray_application_test.moc"
