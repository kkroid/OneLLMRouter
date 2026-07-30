#include "smoke_mode.h"

#include <QCoreApplication>
#include <QDir>
#include <QFileInfo>
#include <QJsonDocument>
#include <QSaveFile>

namespace {
QString corePath()
{
    return QDir(QCoreApplication::applicationDirPath())
        .filePath("onellm-router-core.exe");
}
}

QJsonObject buildSmokeResult(qint64 pid, int port)
{
    return {
        {"service", "onellm-router"},
        {"ownership", "owned"},
        {"healthy", true},
        {"pid", pid},
        {"port", port},
    };
}

SmokeRunner::SmokeRunner(QString configPath, QString resultPath,
                         QObject *parent)
    : QObject(parent),
      m_configPath(QFileInfo(configPath).absoluteFilePath()),
      m_resultPath(QFileInfo(resultPath).absoluteFilePath()),
      m_discovery(corePath(), m_configPath, this),
      m_process(this)
{
    m_startTimer.setSingleShot(true);
    m_startTimer.setInterval(30000);
    connect(&m_startTimer, &QTimer::timeout, this, [this] { fail(5); });
    connect(&m_discovery, &RouterDiscovery::configFailed, this,
            [this](const QString &) { fail(2); });
    connect(&m_discovery, &RouterDiscovery::portConflict, this,
            [this](const RouterConfigInfo &, const QString &) { fail(3); });
    connect(&m_discovery, &RouterDiscovery::routerAbsent, this,
            [this](const RouterConfigInfo &) {
                if (m_process.ownership() == ProcessOwnership::Owned) {
                    QTimer::singleShot(250, &m_discovery,
                                       &RouterDiscovery::discover);
                } else if (!m_process.startOwned(m_configPath)) {
                    fail(4);
                }
            });
    connect(&m_discovery, &RouterDiscovery::externalRouterFound, this,
            [this](const RouterConfigInfo &, const RouterHealth &health) {
                if (m_process.ownership() != ProcessOwnership::Owned ||
                    health.pid != m_process.processId()) {
                    fail(3);
                } else {
                    writeResultAndStop(health);
                }
            });
    connect(&m_process, &RouterProcess::processStarted, this,
            [this](qint64) {
                QTimer::singleShot(250, &m_discovery,
                                   &RouterDiscovery::discover);
            });
    connect(&m_process, &RouterProcess::processError, this,
            [this](const QString &) { fail(4); });
    connect(&m_process, &RouterProcess::gracefulStopTimedOut, this,
            [this] { fail(6); });
    connect(&m_process, &RouterProcess::processFinished, this,
            [this](int, QProcess::ExitStatus) {
                QCoreApplication::exit(m_resultWritten ? 0 : 4);
            });
}

void SmokeRunner::start()
{
    if (m_configPath.isEmpty() || m_resultPath.isEmpty()) {
        fail(2);
        return;
    }
    m_startTimer.start();
    m_discovery.discover();
}

void SmokeRunner::fail(int exitCode)
{
    m_startTimer.stop();
    QCoreApplication::exit(exitCode);
}

void SmokeRunner::writeResultAndStop(const RouterHealth &health)
{
    m_startTimer.stop();
    QSaveFile file(m_resultPath);
    const QByteArray payload =
        QJsonDocument(buildSmokeResult(health.pid, health.port))
            .toJson(QJsonDocument::Compact);
    if (!file.open(QIODevice::WriteOnly) ||
        file.write(payload) != payload.size() || !file.commit()) {
        fail(7);
        return;
    }
    m_resultWritten = true;
    if (!m_process.requestGracefulStop()) {
        fail(6);
    }
}
