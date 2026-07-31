#include "router_process.h"

#include <QCoreApplication>
#include <QDir>
#include <QFileInfo>

#ifdef Q_OS_WIN
#include <qt_windows.h>
#endif

QStringList routerChildArguments(const QString &absoluteConfigPath)
{
    return {"serve", "--tray-child", "--config", absoluteConfigPath};
}

QByteArray gracefulShutdownCommand()
{
    return QByteArrayLiteral("shutdown\n");
}

RouterProcess::RouterProcess(QObject *parent)
    : RouterProcess(QString(), 30000, parent)
{
}

RouterProcess::RouterProcess(QString coreExecutable, int stopTimeoutMs,
                             QObject *parent)
    : QObject(parent)
{
    m_stopTimer.setSingleShot(true);
    m_stopTimer.setInterval(stopTimeoutMs);
    m_coreExecutable = std::move(coreExecutable);
    connect(&m_stopTimer, &QTimer::timeout, this,
            &RouterProcess::gracefulStopTimedOut);
}

RouterProcess::~RouterProcess()
{
    if (!m_process) {
        return;
    }
    m_process->disconnect(this);
    if (m_process->state() == QProcess::NotRunning) {
        delete m_process;
    }
}

ProcessOwnership RouterProcess::ownership() const
{
    return m_ownership;
}

RouterHealth RouterProcess::health() const
{
    return m_health;
}

bool RouterProcess::hasChildProcess() const
{
    return m_process != nullptr;
}

qint64 RouterProcess::processId() const
{
    return m_process ? m_process->processId() : 0;
}

bool RouterProcess::attachExternal(const RouterHealth &health)
{
    if (!health.valid || m_ownership != ProcessOwnership::None ||
        (m_process && m_process->state() != QProcess::NotRunning)) {
        return false;
    }
    delete m_process;
    m_process = nullptr;
    m_health = health;
    m_ownership = ProcessOwnership::External;
    emit ownershipChanged(m_ownership);
    emit stateChanged(RouterState::Healthy);
    return true;
}

QProcess *RouterProcess::ensureProcess()
{
    if (m_process) {
        return m_process;
    }

    m_process = new QProcess;
    connect(m_process, &QProcess::started, this, [this] {
        m_startPending = false;
        m_ownership = ProcessOwnership::Owned;
        emit ownershipChanged(m_ownership);
        emit processStarted(m_process->processId());
    });
    connect(m_process,
            qOverload<int, QProcess::ExitStatus>(&QProcess::finished), this,
            &RouterProcess::handleFinished);
    connect(m_process, &QProcess::errorOccurred, this,
            [this](QProcess::ProcessError error) {
                if (error == QProcess::FailedToStart) {
                    m_startPending = false;
                    emit stateChanged(RouterState::Error);
                    emit processError(m_process->errorString());
                }
            });
    return m_process;
}

bool RouterProcess::startOwned(const QString &configPath)
{
    if (m_ownership != ProcessOwnership::None || m_startPending ||
        configPath.isEmpty() ||
        (m_process && m_process->state() != QProcess::NotRunning)) {
        return false;
    }

    if (m_coreExecutable.isEmpty()) {
        m_coreExecutable = QDir(QCoreApplication::applicationDirPath())
                               .filePath(QStringLiteral("onellm-router-core.exe"));
    }
    m_configPath = QFileInfo(configPath).absoluteFilePath();
    QProcess *process = ensureProcess();
    process->setProgram(m_coreExecutable);
    process->setArguments(routerChildArguments(m_configPath));
    process->setWorkingDirectory(QCoreApplication::applicationDirPath());
    process->setProcessChannelMode(QProcess::SeparateChannels);
#ifdef Q_OS_WIN
    process->setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *arguments) {
            arguments->flags |= CREATE_NO_WINDOW;
        });
#endif
    m_startPending = true;
    emit stateChanged(RouterState::Starting);
    process->start(QIODevice::ReadWrite);
    return true;
}

bool RouterProcess::requestGracefulStop()
{
    m_restartPending = false;
    if (m_ownership != ProcessOwnership::Owned || !m_process ||
        m_process->state() == QProcess::NotRunning) {
        return false;
    }

    const QByteArray command = gracefulShutdownCommand();
    if (m_process->write(command) != command.size()) {
        return false;
    }
    m_stopTimer.start();
    return true;
}

bool RouterProcess::restart()
{
    if (m_restartPending || !canControlRouter(m_ownership)) {
        return false;
    }
    if (!requestGracefulStop()) {
        return false;
    }
    m_restartPending = true;
    return true;
}

bool RouterProcess::detachExternal()
{
    if (m_ownership != ProcessOwnership::External) return false;
    m_ownership = ProcessOwnership::None;
    m_health = {};
    emit ownershipChanged(m_ownership);
    emit stateChanged(RouterState::Stopped);
    return true;
}

void RouterProcess::updateHealth(const RouterHealth &health)
{
    if (!health.valid || m_ownership == ProcessOwnership::None) {
        return;
    }
    m_health = health;
    emit stateChanged(RouterState::Healthy);
}

void RouterProcess::handleFinished(int exitCode,
                                   QProcess::ExitStatus exitStatus)
{
    m_stopTimer.stop();
    m_startPending = false;
    m_health = {};
    if (m_ownership == ProcessOwnership::Owned) {
        m_ownership = ProcessOwnership::None;
        emit ownershipChanged(m_ownership);
    }
    emit stateChanged(RouterState::Stopped);
    emit processFinished(exitCode, exitStatus);

    if (m_restartPending) {
        m_restartPending = false;
        startOwned(m_configPath);
    }
}
