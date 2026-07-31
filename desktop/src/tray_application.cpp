#include "tray_application.h"

#include <QAction>
#include <QCoreApplication>
#include <QDesktopServices>
#include <QDir>
#include <QFileInfo>
#include <QSettings>
#include <QUrl>

namespace {
QString corePath()
{
    return QDir(QCoreApplication::applicationDirPath())
        .filePath("onellm-router-core.exe");
}
QString registryPath()
{
    return "HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Run";
}
}

TrayActionPolicy trayActionPolicy(ProcessOwnership ownership, RouterState state)
{
    return {
        ownership == ProcessOwnership::None && state == RouterState::Stopped,
        ownership == ProcessOwnership::Owned && state != RouterState::Conflict,
        ownership == ProcessOwnership::Owned && state != RouterState::Conflict,
        ownership == ProcessOwnership::External,
    };
}

bool shouldAutoStartRouter(ProcessOwnership ownership, bool autoStartAllowed)
{
    return ownership == ProcessOwnership::None && autoStartAllowed;
}

bool healthMatchesOwnedProcess(ProcessOwnership ownership, qint64 processId,
                               const RouterHealth &health)
{
    return ownership == ProcessOwnership::Owned && health.valid &&
           processId > 0 && health.pid == processId;
}

QString autoStartCommand(const QString &executable, const QString &configPath)
{
    return QString("\"%1\" --config \"%2\"")
        .arg(QDir::toNativeSeparators(executable),
             QDir::toNativeSeparators(configPath));
}

QString autoStartValueName()
{
    return "OneLLMRouter Desktop";
}

bool NotificationLimiter::shouldNotify(const QString &key, const QDateTime &now)
{
    const auto previous = m_lastShown.constFind(key);
    if (previous != m_lastShown.cend() && previous->secsTo(now) < 300) {
        return false;
    }
    m_lastShown.insert(key, now);
    return true;
}

TrayApplication::TrayApplication(QString configPath, bool activateRuntime,
                                 QObject *parent)
    : QObject(parent),
      m_configPath(QFileInfo(configPath).absoluteFilePath()),
      m_strings(stringsForLocale(QLocale::system())),
      m_discovery(corePath(), m_configPath, 2000, this),
      m_process(this),
      m_proxyProbe(this)
{
    connect(&m_menu, &QMenu::aboutToShow, this, &TrayApplication::rebuildMenu);
    m_trayIcon.setContextMenu(&m_menu);
    connect(&m_discovery, &RouterDiscovery::configFailed, this,
            [this](const QString &) { setState(RouterState::Error); });
    connect(&m_discovery, &RouterDiscovery::routerAbsent, this,
            [this](const RouterConfigInfo &config) {
                m_config = config;
                if (m_process.ownership() == ProcessOwnership::External) {
                    m_process.detachExternal();
                    m_health = {};
                    setState(RouterState::Stopped);
                } else if (shouldAutoStartRouter(m_process.ownership(),
                                                 m_autoStartAllowed)) {
                    startOwned();
                }
            });
    connect(&m_discovery, &RouterDiscovery::externalRouterFound, this,
            [this](const RouterConfigInfo &config, const RouterHealth &health) {
                m_config = config;
                if (m_process.ownership() == ProcessOwnership::None) {
                    m_process.attachExternal(health);
                } else if (m_process.ownership() == ProcessOwnership::External) {
                    m_process.updateHealth(health);
                } else if (!healthMatchesOwnedProcess(
                               m_process.ownership(), m_process.processId(), health)) {
                    setState(RouterState::Conflict);
                    return;
                } else {
                    m_process.updateHealth(health);
                }
                m_health = health;
                setState(RouterState::Healthy);
                if (!config.proxySocks5.isEmpty()) m_proxyProbe.probe(config.proxySocks5);
            });
    connect(&m_discovery, &RouterDiscovery::portConflict, this,
            [this](const RouterConfigInfo &config, const QString &) {
                m_config = config;
                setState(RouterState::Conflict);
            });
    connect(&m_process, &RouterProcess::stateChanged, this,
            [this](RouterState state) { setState(state); });
    connect(&m_process, &RouterProcess::processStarted, this,
            [this](qint64) { QTimer::singleShot(250, this, &TrayApplication::discover); });
    connect(&m_process, &RouterProcess::gracefulStopTimedOut, this,
            [this] { setState(RouterState::Error, QStringLiteral("Graceful stop timed out")); });
    connect(&m_proxyProbe, &ProxyProbe::probeFinished, this,
            [this](const QString &, bool reachable) { m_proxyReachable = reachable; });
    m_pollTimer.setInterval(2000);
    connect(&m_pollTimer, &QTimer::timeout, this, &TrayApplication::discover);
    if (activateRuntime) {
        m_trayIcon.show();
        m_pollTimer.start();
        QTimer::singleShot(0, this, &TrayApplication::discover);
    }
}

QMenu *TrayApplication::menu() { return &m_menu; }

void TrayApplication::rebuildMenu()
{
    m_menu.clear();
    auto disabled = [this](const QString &text) {
        QAction *action = m_menu.addAction(text);
        action->setEnabled(false);
    };
    disabled(QString("OneLLMRouter %1 - %2")
                 .arg(m_health.version.isEmpty() ? QString(ONELLM_VERSION)
                                                 : m_health.version,
                      stateText()));
    disabled(m_strings.modelsPort.arg(m_health.models)
                 .arg(m_config.port ? m_config.port : m_health.port));
    disabled(m_strings.proxy.arg(m_config.proxySocks5.isEmpty() ? "-" : m_config.proxySocks5,
                                 m_proxyReachable ? m_strings.reachable : m_strings.unreachable));
    m_menu.addSeparator();
    const auto policy = trayActionPolicy(m_process.ownership(), m_state);
    if (policy.externalManaged) disabled(m_strings.externallyManaged);
    if (policy.start) {
        QAction *action = m_menu.addAction(m_strings.start);
        action->setObjectName("startRouter");
        connect(action, &QAction::triggered, this, &TrayApplication::startOwned);
    }
    if (policy.restart) connect(m_menu.addAction(m_strings.restart), &QAction::triggered,
                                &m_process, &RouterProcess::restart);
    if (policy.stop) connect(m_menu.addAction(m_strings.stop), &QAction::triggered,
                             this, &TrayApplication::stopOwned);
    m_menu.addSeparator();
    connect(m_menu.addAction(m_strings.openConfig), &QAction::triggered, this,
            [this] { QDesktopServices::openUrl(QUrl::fromLocalFile(m_configPath)); });
    QAction *logs = m_menu.addAction(m_strings.openLogs);
    logs->setEnabled(!m_config.logDir.isEmpty());
    connect(logs, &QAction::triggered, this,
            [this] { QDesktopServices::openUrl(QUrl::fromLocalFile(m_config.logDir)); });
    QAction *autoStart = m_menu.addAction(m_strings.autoStart);
    autoStart->setCheckable(true);
    autoStart->setChecked(autoStartEnabled());
    connect(autoStart, &QAction::toggled, this, &TrayApplication::setAutoStartEnabled);
    m_menu.addSeparator();
    connect(m_menu.addAction(m_strings.quit), &QAction::triggered,
            qApp, &QCoreApplication::quit);
}

void TrayApplication::discover() { m_discovery.discover(); }

void TrayApplication::startOwned()
{
    m_autoStartAllowed = true;
    if (m_process.startOwned(m_configPath)) setState(RouterState::Starting);
}

void TrayApplication::stopOwned()
{
    if (m_process.requestGracefulStop()) m_autoStartAllowed = false;
}

void TrayApplication::setState(RouterState state, const QString &detail)
{
    if (m_state == state) return;
    const RouterState previous = m_state;
    m_state = state;
    const QString icon = state == RouterState::Healthy ? ":/icons/green.ico"
        : (state == RouterState::Starting || state == RouterState::Degraded)
              ? ":/icons/yellow.ico" : ":/icons/red.ico";
    m_trayIcon.setIcon(QIcon(icon));
    m_trayIcon.setToolTip(QString("OneLLMRouter - %1").arg(stateText()));
    const bool recovery = state == RouterState::Healthy && previous != RouterState::Healthy;
    const bool problem = state == RouterState::Stopped || state == RouterState::Degraded ||
                         state == RouterState::Conflict || state == RouterState::Error;
    const QString key = QString::number(int(state));
    if ((recovery || problem) &&
        m_limiter.shouldNotify(key, QDateTime::currentDateTimeUtc()))
        m_trayIcon.showMessage("OneLLMRouter",
                               detail.isEmpty() ? stateText() : stateText() + ": " + detail);
}

QString TrayApplication::stateText() const
{
    switch (m_state) {
    case RouterState::Stopped: return m_strings.stopped;
    case RouterState::Starting: return m_strings.starting;
    case RouterState::Healthy: return m_strings.healthy;
    case RouterState::Degraded: return m_strings.degraded;
    case RouterState::Conflict: return m_strings.conflict;
    case RouterState::Error: return m_strings.error;
    }
    return m_strings.error;
}

void TrayApplication::setAutoStartEnabled(bool enabled)
{
    QSettings settings(registryPath(), QSettings::NativeFormat);
    if (enabled)
        settings.setValue(autoStartValueName(),
                          autoStartCommand(QCoreApplication::applicationFilePath(), m_configPath));
    else
        settings.remove(autoStartValueName());
}

bool TrayApplication::autoStartEnabled() const
{
    QSettings settings(registryPath(), QSettings::NativeFormat);
    return settings.value(autoStartValueName()).toString() ==
           autoStartCommand(QCoreApplication::applicationFilePath(), m_configPath);
}
