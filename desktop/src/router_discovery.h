#pragma once

#include "router_types.h"

#include <QNetworkReply>
#include <QObject>
#include <QProcess>
#include <QStringList>

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
                             QObject *parent = nullptr);
    void discover();

signals:
    void configFailed(const QString &message);
    void routerAbsent(const RouterConfigInfo &config);
    void externalRouterFound(const RouterConfigInfo &config,
                             const RouterHealth &health);
    void portConflict(const RouterConfigInfo &config, const QString &message);

private:
    void probeHealth(const RouterConfigInfo &config);
    void finishProbe(const RouterConfigInfo &config, QNetworkReply *reply);

    QString m_coreExecutable;
    QString m_configPath;
    QProcess m_configProcess;
    QNetworkAccessManager *m_network;
};

Q_DECLARE_METATYPE(RouterConfigInfo)
