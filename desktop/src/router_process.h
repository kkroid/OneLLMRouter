#pragma once

#include "router_types.h"

#include <QByteArray>
#include <QObject>
#include <QProcess>
#include <QStringList>
#include <QTimer>

QStringList routerChildArguments(const QString &absoluteConfigPath);
QByteArray gracefulShutdownCommand();

class RouterProcess : public QObject
{
    Q_OBJECT

public:
    explicit RouterProcess(QObject *parent = nullptr);
    ~RouterProcess() override;

    ProcessOwnership ownership() const;
    RouterHealth health() const;
    bool hasChildProcess() const;
    qint64 processId() const;

    bool attachExternal(const RouterHealth &health);
    bool startOwned(const QString &configPath);
    bool requestGracefulStop();
    bool restart();
    void updateHealth(const RouterHealth &health);

signals:
    void ownershipChanged(ProcessOwnership ownership);
    void stateChanged(RouterState state);
    void processStarted(qint64 pid);
    void processFinished(int exitCode, QProcess::ExitStatus exitStatus);
    void processError(const QString &message);
    void gracefulStopTimedOut();

private:
    QProcess *ensureProcess();
    void handleFinished(int exitCode, QProcess::ExitStatus exitStatus);

    QProcess *m_process = nullptr;
    QTimer m_stopTimer;
    ProcessOwnership m_ownership = ProcessOwnership::None;
    RouterHealth m_health;
    QString m_coreExecutable;
    QString m_configPath;
    bool m_startPending = false;
    bool m_restartPending = false;
};
