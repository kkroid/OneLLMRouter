#pragma once

#include "i18n.h"
#include "proxy_probe.h"
#include "router_discovery.h"
#include "router_process.h"

#include <QDateTime>
#include <QHash>
#include <QMenu>
#include <QObject>
#include <QSystemTrayIcon>
#include <QTimer>

struct TrayActionPolicy {
    bool start = false, stop = false, restart = false, externalManaged = false;
};

TrayActionPolicy trayActionPolicy(ProcessOwnership ownership, RouterState state);
bool shouldAutoStartRouter(ProcessOwnership ownership, bool autoStartAllowed);
bool healthMatchesOwnedProcess(ProcessOwnership ownership, qint64 processId,
                               const RouterHealth &health);
QString autoStartCommand(const QString &executable, const QString &configPath);
QString autoStartValueName();

class NotificationLimiter {
public:
    bool shouldNotify(const QString &key, const QDateTime &now);
private:
    QHash<QString, QDateTime> m_lastShown;
};

class TrayApplication : public QObject
{
    Q_OBJECT
public:
    explicit TrayApplication(QString configPath, bool activateRuntime = true,
                             QObject *parent = nullptr);
    QMenu *menu();

private:
    void rebuildMenu();
    void discover();
    void startOwned();
    void stopOwned();
    void setState(RouterState state, const QString &detail = {});
    QString stateText() const;
    void setAutoStartEnabled(bool enabled);
    bool autoStartEnabled() const;

    QString m_configPath;
    Strings m_strings;
    QMenu m_menu;
    QSystemTrayIcon m_trayIcon;
    RouterDiscovery m_discovery;
    RouterProcess m_process;
    ProxyProbe m_proxyProbe;
    QTimer m_pollTimer;
    NotificationLimiter m_limiter;
    RouterConfigInfo m_config;
    RouterHealth m_health;
    RouterState m_state = RouterState::Stopped;
    bool m_proxyReachable = false;
    bool m_autoStartAllowed = true;
};
