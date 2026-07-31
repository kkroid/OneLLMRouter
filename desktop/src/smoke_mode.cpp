#include "smoke_mode.h"

#include <QCoreApplication>
#include <QDir>
#include <QFileInfo>
#include <QJsonDocument>
#include <QDebug>
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

int smokeObservationDelayMs()
{
    return 500;
}

bool smokeCoreExitIsSuccessful(bool healthObserved, bool shutdownRequested,
                               int exitCode, QProcess::ExitStatus exitStatus)
{
    return healthObserved && shutdownRequested && exitCode == 0 &&
           exitStatus == QProcess::NormalExit;
}

SmokeRunner::SmokeRunner(QString configPath, QString resultPath,
                         QObject *parent)
    : QObject(parent),
      m_configPath(QFileInfo(configPath).absoluteFilePath()),
      m_resultPath(QFileInfo(resultPath).absoluteFilePath()),
      m_discovery(corePath(), m_configPath, 2000, this),
      m_process(this)
{
    m_startTimer.setSingleShot(true);
    m_startTimer.setInterval(30000);
    connect(&m_startTimer, &QTimer::timeout, this,
            [this] { fail(5, QStringLiteral("startup timed out")); });
    connect(&m_discovery, &RouterDiscovery::configFailed, this,
            [this](const QString &message) {
                qCritical().noquote() << "smoke config failure:" << message;
                fail(2, message);
            });
    connect(&m_discovery, &RouterDiscovery::portConflict, this,
            [this](const RouterConfigInfo &, const QString &message) {
                qCritical().noquote() << "smoke port conflict:" << message;
                fail(3, message);
            });
    connect(&m_discovery, &RouterDiscovery::routerAbsent, this,
            [this](const RouterConfigInfo &) {
                if (m_process.ownership() == ProcessOwnership::Owned) {
                    QTimer::singleShot(250, &m_discovery,
                                       &RouterDiscovery::discover);
                } else if (!m_process.startOwned(m_configPath)) {
                    fail(4, QStringLiteral("failed to start core"));
                }
            });
    connect(&m_discovery, &RouterDiscovery::externalRouterFound, this,
            [this](const RouterConfigInfo &, const RouterHealth &health) {
                if (m_process.ownership() != ProcessOwnership::Owned ||
                    health.pid != m_process.processId()) {
                    qCritical() << "smoke ownership mismatch"
                                << int(m_process.ownership())
                                << health.pid << m_process.processId();
                    fail(3, QStringLiteral("owned process health PID mismatch"));
                } else {
                    observeAndStop(health);
                }
            });
    connect(&m_process, &RouterProcess::processStarted, this,
            [this](qint64) {
                QTimer::singleShot(250, &m_discovery,
                                   &RouterDiscovery::discover);
            });
    connect(&m_process, &RouterProcess::processError, this,
            [this](const QString &message) {
                qCritical().noquote() << "smoke process failure:" << message;
                fail(4, message);
            });
    connect(&m_process, &RouterProcess::gracefulStopTimedOut, this,
            [this] { fail(6, QStringLiteral("graceful stop timed out")); });
    connect(&m_process, &RouterProcess::processFinished, this,
            [this](int exitCode, QProcess::ExitStatus exitStatus) {
                if (m_terminal) return;
                if (!smokeCoreExitIsSuccessful(
                        m_observedHealth.valid, m_shutdownRequested,
                        exitCode, exitStatus)) {
                    fail(4, QStringLiteral(
                        "core exited before successful smoke shutdown"));
                    return;
                }
                succeed();
            });
}

void SmokeRunner::start()
{
    if (m_configPath.isEmpty() || m_resultPath.isEmpty()) {
        fail(2, QStringLiteral("missing smoke path"));
        return;
    }
    m_startTimer.start();
    m_discovery.discover();
}

void SmokeRunner::fail(int exitCode, const QString &message)
{
    if (m_terminal) return;
    m_terminal = true;
    m_startTimer.stop();
    if (!m_resultPath.isEmpty()) {
        QSaveFile file(m_resultPath);
        const QByteArray payload = QJsonDocument(QJsonObject{
            {"service", "onellm-router"},
            {"healthy", false},
            {"exit_code", exitCode},
            {"error", message},
        }).toJson(QJsonDocument::Compact);
        if (file.open(QIODevice::WriteOnly) &&
            file.write(payload) == payload.size()) {
            file.commit();
        }
    }
    QCoreApplication::exit(exitCode);
}

void SmokeRunner::observeAndStop(const RouterHealth &health)
{
    if (m_terminal || m_observedHealth.valid) return;
    m_startTimer.stop();
    m_observedHealth = health;
    QTimer::singleShot(smokeObservationDelayMs(), this, [this] {
        if (m_terminal) return;
        if (!m_process.requestGracefulStop()) {
            fail(6, QStringLiteral("failed to request graceful stop"));
            return;
        }
        m_shutdownRequested = true;
    });
}

void SmokeRunner::succeed()
{
    QSaveFile file(m_resultPath);
    const QByteArray payload =
        QJsonDocument(buildSmokeResult(m_observedHealth.pid,
                                       m_observedHealth.port))
            .toJson(QJsonDocument::Compact);
    if (!file.open(QIODevice::WriteOnly) ||
        file.write(payload) != payload.size() || !file.commit()) {
        fail(7, QStringLiteral("failed to write smoke result"));
        return;
    }
    m_terminal = true;
    QCoreApplication::exit(0);
}
