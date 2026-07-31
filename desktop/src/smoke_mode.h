#pragma once

#include "router_discovery.h"
#include "router_process.h"

#include <QJsonObject>
#include <QObject>
#include <QTimer>

QJsonObject buildSmokeResult(qint64 pid, int port);
int smokeObservationDelayMs();
bool smokeCoreExitIsSuccessful(bool healthObserved, bool shutdownRequested,
                               int exitCode, QProcess::ExitStatus exitStatus);

class SmokeRunner : public QObject
{
    Q_OBJECT
public:
    SmokeRunner(QString configPath, QString resultPath,
                QObject *parent = nullptr);

public slots:
    void start();

private:
    void fail(int exitCode, const QString &message = {});
    void observeAndStop(const RouterHealth &health);
    void succeed();

    QString m_configPath;
    QString m_resultPath;
    RouterDiscovery m_discovery;
    RouterProcess m_process;
    QTimer m_startTimer;
    RouterHealth m_observedHealth;
    bool m_shutdownRequested = false;
    bool m_terminal = false;
};
