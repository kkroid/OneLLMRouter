#pragma once

#include "router_types.h"

#include <QNetworkReply>
#include <QNetworkProxy>
#include <QObject>
#include <QProcess>
#include <QStringList>
#include <QTimer>

struct RouterConfigInfo {
    bool valid = false;
    QString configPath;
    QString host;
    int port = 0;
    QString logDir;
    QString proxySocks5;
    bool bell = false;
    QString oneLlmCatalogPath;
    QString codexCatalogPath;
};

enum class ProbeTransport {
    HttpResponse,
    ConnectionRefused,
    Unreachable,
    OtherListener,
};

enum class DiscoveryClassification {
    Absent,
    External,
    Conflict,
};

QStringList configInfoArguments(const QString &configPath);
QNetworkProxy localHealthProxy();
RouterConfigInfo parseRouterConfigInfo(const QByteArray &payload);
RouterHealth parseRouterHealth(const QByteArray &payload);
DiscoveryClassification classifyHealthProbe(ProbeTransport transport,
                                            const RouterHealth &health);

class QNetworkAccessManager;

class RouterDiscovery : public QObject
{
    Q_OBJECT

public:
    explicit RouterDiscovery(QString coreExecutable, QString configPath,
                             int timeoutMs = 2000, QObject *parent = nullptr);
    void discover();

signals:
    void configFailed(const QString &message);
    void routerAbsent(const RouterConfigInfo &config);
    void externalRouterFound(const RouterConfigInfo &config,
                             const RouterHealth &health);
    void portConflict(const RouterConfigInfo &config, const QString &message);

private:
    void probePort(const RouterConfigInfo &config, quint64 generation);
    void probeHealth(const RouterConfigInfo &config, quint64 generation);
    void finishProbe(const RouterConfigInfo &config, QNetworkReply *reply,
                     quint64 generation);

    QString m_coreExecutable;
    QString m_configPath;
    QProcess m_configProcess;
    QNetworkAccessManager *m_network;
    QTimer m_configTimer;
    int m_timeoutMs;
    bool m_busy = false;
    bool m_configTimedOut = false;
    quint64 m_generation = 0;
};

Q_DECLARE_METATYPE(RouterConfigInfo)
