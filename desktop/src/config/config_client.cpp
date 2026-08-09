#include "config_client.h"

#include <QJsonArray>
#include <QJsonDocument>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QProcess>
#include <QUrl>

namespace {

QJsonObject reasoningObject(const CodexReasoningConfig &reasoning)
{
    QJsonArray levels;
    for (const QString &level : reasoning.supportedLevels) levels.append(level);
    return {{"default_reasoning_level", reasoning.defaultLevel},
            {"supported_reasoning_levels", levels}};
}

ConfigResult validationResult(const QJsonObject &object)
{
    ConfigResult result;
    result.succeeded = object.value("valid").toBool(false);
    for (const QJsonValue &value : object.value("errors").toArray()) {
        const QJsonObject error = value.toObject();
        result.fieldErrors.append({error.value("field").toString(),
                                   error.value("message").toString()});
    }
    return result;
}

QString endpoint(const ProviderConfigSnapshot &provider, ModelProtocol protocol)
{
    QString base;
    QString suffix = "/models";
    if (protocol == ModelProtocol::Anthropic) base = provider.baseUrl;
    if (protocol == ModelProtocol::OpenAIChat) base = provider.openAIBaseUrl;
    if (protocol == ModelProtocol::OpenAIResponses) {
        base = provider.responsesBaseUrl;
        suffix = "/v1/models";
    }
    while (base.endsWith('/')) base.chop(1);
    return base.isEmpty() ? QString() : base + suffix;
}

} // namespace

ConfigClient::ConfigClient(QString executable, QString configPath, QObject *parent)
    : QObject(parent), m_executable(std::move(executable)),
      m_configPath(std::move(configPath))
{
}

ConfigResult ConfigClient::run(const QStringList &arguments, const QByteArray &input,
                               QJsonObject *output) const
{
    QProcess process;
    QStringList commandArguments{"--config", m_configPath};
    commandArguments.append(arguments);
    process.start(m_executable, commandArguments);
    if (!process.waitForStarted(5000)) return {false, "Core command could not start"};
    if (!input.isEmpty()) process.write(input);
    process.closeWriteChannel();
    if (!process.waitForFinished(10000)) {
        process.kill();
        process.waitForFinished();
        return {false, "Core command timed out"};
    }
    const QByteArray standardOutput = process.readAllStandardOutput();
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        return {false, "Core command failed"};
    }
    QJsonParseError parseError;
    const QJsonDocument document = QJsonDocument::fromJson(standardOutput, &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        return {false, "Core returned invalid JSON"};
    }
    *output = document.object();
    return {true};
}

ConfigResult ConfigClient::load(ConfigSnapshot *snapshot) const
{
    QJsonObject root;
    ConfigResult result = run({"config-get", "--json"}, {}, &root);
    if (!result.succeeded) return result;

    ConfigSnapshot loaded;
    for (const QJsonValue &value : root.value("providers").toArray()) {
        const QJsonObject object = value.toObject();
        ProviderConfigSnapshot provider;
        provider.name = object.value("name").toString();
        provider.prefix = object.value("prefix").toString();
        provider.baseUrl = object.value("base_url").toString();
        provider.openAIBaseUrl = object.value("openai_base_url").toString();
        provider.responsesBaseUrl = object.value("responses_base_url").toString();
        provider.apiKeySet = object.value("api_key_set").toBool();
        for (const QJsonValue &model : object.value("models").toArray())
            provider.models.append(model.toString());
        if (object.value("proxy").isBool())
            provider.proxy = object.value("proxy").toBool()
                ? ProviderConfigSnapshot::ProxyPolicy::UseProxy
                : ProviderConfigSnapshot::ProxyPolicy::Direct;
        loaded.providers.append(provider);
    }
    const QJsonObject models = root.value("codex").toObject().value("models").toObject();
    for (auto iterator = models.begin(); iterator != models.end(); ++iterator) {
        CodexReasoningConfig reasoning;
        const QJsonObject object = iterator.value().toObject();
        reasoning.defaultLevel = object.value("default_reasoning_level").toString();
        for (const QJsonValue &level : object.value("supported_reasoning_levels").toArray())
            reasoning.supportedLevels.append(level.toString());
        loaded.codexModels.insert(iterator.key(), reasoning);
    }
    const QJsonObject modelSlots = root.value("model_slots").toObject();
    for (const QString &slot : {"default", "opus", "sonnet", "haiku", "fable"})
        loaded.modelSlots.insert(slot, modelSlots.value(slot).toString());
    *snapshot = loaded;
    return result;
}

QByteArray ConfigClient::serialize(const ConfigSnapshot &snapshot,
                                   const QMap<int, QString> &apiKeys)
{
    QJsonArray providers;
    for (int index = 0; index < snapshot.providers.size(); ++index) {
        const ProviderConfigSnapshot &provider = snapshot.providers[index];
        QJsonArray models;
        for (const QString &model : provider.models) models.append(model);
        QJsonObject object{{"name", provider.name}, {"prefix", provider.prefix},
                           {"base_url", provider.baseUrl},
                           {"openai_base_url", provider.openAIBaseUrl},
                           {"responses_base_url", provider.responsesBaseUrl},
                           {"api_key_set", provider.apiKeySet}, {"models", models}};
        if (provider.proxy == ProviderConfigSnapshot::ProxyPolicy::Inherit)
            object.insert("proxy", QJsonValue::Null);
        else
            object.insert("proxy", provider.proxy == ProviderConfigSnapshot::ProxyPolicy::UseProxy);
        if (apiKeys.contains(index) && !apiKeys.value(index).isEmpty())
            object.insert("api_key", apiKeys.value(index));
        providers.append(object);
    }
    QJsonObject codexModels;
    for (auto iterator = snapshot.codexModels.cbegin(); iterator != snapshot.codexModels.cend(); ++iterator)
        codexModels.insert(iterator.key(), reasoningObject(iterator.value()));
    QJsonObject modelSlots;
    for (auto iterator = snapshot.modelSlots.cbegin(); iterator != snapshot.modelSlots.cend(); ++iterator)
        modelSlots.insert(iterator.key(), iterator.value());
    return QJsonDocument(QJsonObject{{"providers", providers},
                                     {"codex", QJsonObject{{"models", codexModels}}},
                                     {"model_slots", modelSlots}}).toJson(QJsonDocument::Compact);
}

ConfigResult ConfigClient::validate(const ConfigSnapshot &snapshot,
                                    const QMap<int, QString> &apiKeys) const
{
    QJsonObject output;
    ConfigResult result = run({"config-validate", "--json"},
                              serialize(snapshot, apiKeys), &output);
    return result.succeeded ? validationResult(output) : result;
}

ConfigResult ConfigClient::apply(const ConfigSnapshot &snapshot,
                                 const QMap<int, QString> &apiKeys) const
{
    QJsonObject output;
    ConfigResult result = run({"config-apply", "--stdin-json"},
                              serialize(snapshot, apiKeys), &output);
    return result.succeeded ? validationResult(output) : result;
}

void ConfigClient::discoverModels(const ProviderConfigSnapshot &provider,
                                  ModelProtocol protocol, const QString &apiKey)
{
    const QString url = endpoint(provider, protocol);
    if (url.isEmpty()) {
        emit discoveryFailed(provider.prefix,
                             "The selected protocol has no Base URL");
        return;
    }
    QNetworkRequest request{QUrl(url)};
    if (!apiKey.isEmpty()) {
        if (protocol == ModelProtocol::Anthropic)
            request.setRawHeader("x-api-key", apiKey.toUtf8());
        else
            request.setRawHeader("Authorization", "Bearer " + apiKey.toUtf8());
    }
    QNetworkReply *reply = m_network.get(request);
    const QString providerPrefix = provider.prefix;
    connect(reply, &QNetworkReply::finished, this,
            [this, reply, providerPrefix] {
        const QByteArray payload = reply->readAll();
        if (reply->error() != QNetworkReply::NoError) {
            reply->deleteLater();
            emit discoveryFailed(providerPrefix, "Model discovery failed");
            return;
        }
        const QJsonObject object = QJsonDocument::fromJson(payload).object();
        QStringList models;
        const QJsonArray values = object.contains("data")
            ? object.value("data").toArray() : object.value("models").toArray();
        for (const QJsonValue &value : values) {
            const QJsonObject model = value.toObject();
            const QString identifier = model.value("id").toString(model.value("slug").toString());
            if (!identifier.isEmpty()) models.append(identifier);
        }
        models.removeDuplicates();
        models.sort();
        reply->deleteLater();
        emit modelsDiscovered(providerPrefix, models);
    });
}
