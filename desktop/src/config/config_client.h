#pragma once

#include "../clients/clients_model.h"

#include <QJsonObject>
#include <QList>
#include <QMap>
#include <QNetworkAccessManager>
#include <QObject>
#include <QStringList>

struct ProviderConfigSnapshot {
    QString name;
    QString prefix;
    QString baseUrl;
    QString openAIBaseUrl;
    QString responsesBaseUrl;
    bool apiKeySet = false;
    QStringList models;
    enum class ProxyPolicy { Inherit, UseProxy, Direct } proxy = ProxyPolicy::Inherit;
};

struct CodexReasoningConfig {
    QString defaultLevel;
    QStringList supportedLevels;
};

struct ConfigSnapshot {
    QList<ProviderConfigSnapshot> providers;
    QMap<QString, CodexReasoningConfig> codexModels;
    QMap<QString, QString> modelSlots;
    bool overwriteCatalog = false;
};

struct ConfigFieldError {
    QString field;
    QString message;
};

struct ConfigResult {
    bool succeeded = false;
    QString error;
    QList<ConfigFieldError> fieldErrors;
};

enum class ModelProtocol { Anthropic, OpenAIChat, OpenAIResponses };

class ConfigClient : public QObject
{
    Q_OBJECT
public:
    explicit ConfigClient(QString executable, QString configPath,
                          QObject *parent = nullptr);

    ConfigResult load(ConfigSnapshot *snapshot) const;
    ConfigResult validate(const ConfigSnapshot &snapshot,
                          const QMap<int, QString> &apiKeys = {}) const;
    ConfigResult apply(const ConfigSnapshot &snapshot,
                       const QMap<int, QString> &apiKeys = {}) const;
    ClientCommandResult claudeStatus() const;
    ClientCommandResult claudeApply() const;
    ClientCommandResult claudeRestore() const;
    ClientCommandResult codexStatus() const;
    ClientCommandResult codexPreview(const QString &model) const;
    ClientCommandResult codexCatalogApply() const;
    void discoverModels(const ProviderConfigSnapshot &provider,
                        ModelProtocol protocol, const QString &apiKey = {});

signals:
    void modelsDiscovered(const QString &providerPrefix,
                          const QStringList &models);
    void discoveryFailed(const QString &providerPrefix,
                         const QString &message);

private:
    ConfigResult run(const QStringList &arguments, const QByteArray &input,
                     QJsonObject *output) const;
    ClientCommandResult runClient(const QStringList &arguments,
                                  const QString &client) const;
    static QByteArray serialize(const ConfigSnapshot &snapshot,
                                const QMap<int, QString> &apiKeys);

    QString m_executable;
    QString m_configPath;
    QNetworkAccessManager m_network;
};
