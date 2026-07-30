#include "router_discovery.h"

#include <QFileInfo>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QNetworkRequest>
#include <QSet>
#include <QUrl>

#ifdef Q_OS_WIN
#include <qt_windows.h>
#endif

namespace {

bool isIntegerInRange(const QJsonValue &value, int minimum, int maximum)
{
    if (!value.isDouble()) {
        return false;
    }
    const double number = value.toDouble();
    const int integer = value.toInt();
    return number == integer && integer >= minimum && integer <= maximum;
}

bool isString(const QJsonObject &object, const QString &key)
{
    return object.contains(key) && object.value(key).isString();
}

bool isLoopbackHost(const QString &host)
{
    return host.compare("localhost", Qt::CaseInsensitive) == 0 ||
           host == "127.0.0.1" || host == "::1";
}

ProbeTransport probeTransport(const QNetworkReply *reply)
{
    if (reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).isValid()) {
        return ProbeTransport::HttpResponse;
    }

    switch (reply->error()) {
    case QNetworkReply::ConnectionRefusedError:
        return ProbeTransport::ConnectionRefused;
    case QNetworkReply::HostNotFoundError:
    case QNetworkReply::TimeoutError:
    case QNetworkReply::TemporaryNetworkFailureError:
    case QNetworkReply::NetworkSessionFailedError:
        return ProbeTransport::Unreachable;
    default:
        return ProbeTransport::OtherListener;
    }
}

} // namespace

QStringList configInfoArguments(const QString &configPath)
{
    return {"--config", configPath, "config-info", "--json"};
}

RouterConfigInfo parseRouterConfigInfo(const QByteArray &payload)
{
    QJsonParseError parseError;
    const QJsonDocument document = QJsonDocument::fromJson(payload, &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        return {};
    }

    const QJsonObject object = document.object();
    const QSet<QString> expectedKeys = {
        "service", "config_path", "host", "http_port", "log_dir",
        "proxy_socks5", "bell", "onellm_catalog_path", "codex_catalog_path",
    };
    const QStringList actualKeyList = object.keys();
    const QSet<QString> actualKeys(actualKeyList.cbegin(), actualKeyList.cend());
    if (actualKeys != expectedKeys ||
        object.value("service").toString() != "onellm-router" ||
        !isString(object, "config_path") || !isString(object, "host") ||
        !isIntegerInRange(object.value("http_port"), 1, 65535) ||
        !isString(object, "log_dir") || !isString(object, "proxy_socks5") ||
        !object.value("bell").isBool() ||
        !isString(object, "onellm_catalog_path") ||
        !isString(object, "codex_catalog_path")) {
        return {};
    }

    RouterConfigInfo result;
    result.valid = true;
    result.configPath = object.value("config_path").toString();
    result.host = object.value("host").toString();
    result.port = object.value("http_port").toInt();
    result.logDir = object.value("log_dir").toString();
    result.proxySocks5 = object.value("proxy_socks5").toString();
    result.bell = object.value("bell").toBool();
    result.oneLlmCatalogPath = object.value("onellm_catalog_path").toString();
    result.codexCatalogPath = object.value("codex_catalog_path").toString();
    return result;
}

RouterHealth parseRouterHealth(const QByteArray &payload)
{
    QJsonParseError parseError;
    const QJsonDocument document = QJsonDocument::fromJson(payload, &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        return {};
    }

    const QJsonObject object = document.object();
    if (object.value("status").toString() != "ok" ||
        object.value("service").toString() != "onellm-router" ||
        !isIntegerInRange(object.value("pid"), 1, INT_MAX) ||
        !isString(object, "version") ||
        object.value("version").toString().isEmpty() ||
        !isIntegerInRange(object.value("http_port"), 1, 65535) ||
        !isIntegerInRange(object.value("models"), 0, INT_MAX) ||
        !object.value("copilot_token").isBool() ||
        (object.contains("proxy_socks5") &&
         !object.value("proxy_socks5").isString())) {
        return {};
    }

    RouterHealth result;
    result.valid = true;
    result.status = object.value("status").toString();
    result.service = object.value("service").toString();
    result.pid = object.value("pid").toInt();
    result.version = object.value("version").toString();
    result.port = object.value("http_port").toInt();
    result.models = object.value("models").toInt();
    result.copilotToken = object.value("copilot_token").toBool();
    result.proxySocks5 = object.value("proxy_socks5").toString();
    return result;
}

DiscoveryClassification classifyHealthProbe(ProbeTransport transport,
                                            const RouterHealth &health)
{
    if (transport == ProbeTransport::HttpResponse) {
        return health.valid ? DiscoveryClassification::External
                            : DiscoveryClassification::Conflict;
    }
    if (transport == ProbeTransport::ConnectionRefused) {
        return DiscoveryClassification::Absent;
    }
    return DiscoveryClassification::Conflict;
}

RouterDiscovery::RouterDiscovery(QString coreExecutable, QString configPath,
                                 int timeoutMs, QObject *parent)
    : QObject(parent),
      m_coreExecutable(std::move(coreExecutable)),
      m_configPath(std::move(configPath)),
      m_network(new QNetworkAccessManager(this)),
      m_timeoutMs(timeoutMs)
{
    m_configTimer.setSingleShot(true);
    connect(&m_configTimer, &QTimer::timeout, this, [this] {
        if (!m_busy || m_configProcess.state() == QProcess::NotRunning) return;
        m_configTimedOut = true;
        ++m_generation;
        m_configProcess.kill();
    });
    connect(&m_configProcess, &QProcess::errorOccurred, this,
            [this](QProcess::ProcessError error) {
                if (error == QProcess::FailedToStart) {
                    m_configTimer.stop();
                    m_busy = false;
                    emit configFailed(m_configProcess.errorString());
                }
            });
    connect(&m_configProcess,
            qOverload<int, QProcess::ExitStatus>(&QProcess::finished), this,
            [this](int exitCode, QProcess::ExitStatus exitStatus) {
                m_configTimer.stop();
                if (!m_busy) return;
                if (m_configTimedOut) {
                    m_configTimedOut = false;
                    m_busy = false;
                    emit configFailed(QStringLiteral("config-info timed out"));
                    return;
                }
                if (exitStatus != QProcess::NormalExit || exitCode != 0) {
                    m_busy = false;
                    emit configFailed(
                        QString::fromLocal8Bit(m_configProcess.readAllStandardError()));
                    return;
                }
                const RouterConfigInfo config =
                    parseRouterConfigInfo(m_configProcess.readAllStandardOutput());
                if (!config.valid || !isLoopbackHost(config.host)) {
                    m_busy = false;
                    emit configFailed(QStringLiteral("Invalid config-info response"));
                    return;
                }
                probeHealth(config, m_generation);
            });
}

void RouterDiscovery::discover()
{
    if (m_busy) return;
    m_busy = true;
    m_configTimedOut = false;
    ++m_generation;
    m_configProcess.setProgram(m_coreExecutable);
    m_configProcess.setArguments(configInfoArguments(m_configPath));
    m_configProcess.setWorkingDirectory(QFileInfo(m_coreExecutable).absolutePath());
#ifdef Q_OS_WIN
    m_configProcess.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *arguments) {
            arguments->flags |= CREATE_NO_WINDOW;
        });
#endif
    m_configProcess.start();
    m_configTimer.start(m_timeoutMs);
}

void RouterDiscovery::probeHealth(const RouterConfigInfo &config,
                                  quint64 generation)
{
    QUrl url;
    url.setScheme(QStringLiteral("http"));
    url.setHost(config.host);
    url.setPort(config.port);
    url.setPath(QStringLiteral("/health"));

    QNetworkRequest request(url);
    request.setTransferTimeout(m_timeoutMs);
    request.setAttribute(QNetworkRequest::RedirectPolicyAttribute,
                         QNetworkRequest::ManualRedirectPolicy);
    QNetworkReply *reply = m_network->get(request);
    connect(reply, &QNetworkReply::finished, this,
            [this, config, reply, generation] {
                finishProbe(config, reply, generation);
            });
}

void RouterDiscovery::finishProbe(const RouterConfigInfo &config,
                                  QNetworkReply *reply, quint64 generation)
{
    if (!m_busy || generation != m_generation) {
        reply->deleteLater();
        return;
    }
    m_busy = false;
    if (reply->attribute(QNetworkRequest::RedirectionTargetAttribute).isValid()) {
        emit portConflict(config, QStringLiteral("Health endpoint redirected"));
        reply->deleteLater();
        return;
    }
    const ProbeTransport transport = probeTransport(reply);
    RouterHealth health;
    if (transport == ProbeTransport::HttpResponse &&
        reply->error() == QNetworkReply::NoError) {
        health = parseRouterHealth(reply->readAll());
        if (health.valid && health.port != config.port) {
            health.valid = false;
        }
    }

    const DiscoveryClassification classification =
        classifyHealthProbe(transport, health);
    if (classification == DiscoveryClassification::External) {
        emit externalRouterFound(config, health);
    } else if (classification == DiscoveryClassification::Absent) {
        emit routerAbsent(config);
    } else {
        emit portConflict(config, QStringLiteral("Invalid router health identity"));
    }
    reply->deleteLater();
}
