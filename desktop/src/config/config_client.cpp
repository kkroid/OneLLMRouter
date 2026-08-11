#include "config_client.h"

#include <QJsonArray>
#include <QJsonDocument>
#include <QProcess>

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

QString protocolArgument(ModelProtocol protocol)
{
    switch (protocol) {
    case ModelProtocol::Anthropic:
        return "anthropic";
    case ModelProtocol::OpenAIChat:
        return "openai";
    case ModelProtocol::OpenAIResponses:
        return "responses";
    }
    return {};
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
    QJsonParseError parseError;
    const QJsonDocument document = QJsonDocument::fromJson(standardOutput, &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        return {false, "Core returned invalid JSON"};
    }
    *output = document.object();
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0)
        return {false, "Core command failed"};
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
    const QJsonObject codex = root.value("codex").toObject();
    loaded.overwriteCatalog = codex.value("overwrite_catalog").toBool();
    const QJsonObject models = codex.value("models").toObject();
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
                                     {"codex", QJsonObject{{"overwrite_catalog", snapshot.overwriteCatalog},
                                                            {"models", codexModels}}},
                                     {"model_slots", modelSlots}}).toJson(QJsonDocument::Compact);
}

ClientCommandResult ConfigClient::runClient(const QStringList &arguments,
                                            const QString &client) const
{
    QJsonObject output;
    const ConfigResult command = run(arguments, {}, &output);
    if (output.isEmpty()) return {false, command.error};
    ClientCommandResult result = client == "claude"
        ? parseClaudeClientEnvelope(output) : parseCodexClientEnvelope(output);
    if (!command.succeeded && result.succeeded) {
        result.succeeded = false;
        result.error = command.error;
    }
    return result;
}

ClientCommandResult ConfigClient::claudeStatus() const
{
    return runClient({"client", "claude", "status", "--json"}, "claude");
}

ClientCommandResult ConfigClient::claudeApply() const
{
    return runClient({"client", "claude", "apply", "--json"}, "claude");
}

ClientCommandResult ConfigClient::claudeRestore() const
{
    return runClient({"client", "claude", "restore", "--json"}, "claude");
}

ClientCommandResult ConfigClient::codexStatus() const
{
    return runClient({"client", "codex", "status", "--json"}, "codex");
}

ClientCommandResult ConfigClient::codexPreview(const QString &model) const
{
    return runClient({"client", "codex", "preview", "--model", model, "--json"},
                     "codex");
}

ClientCommandResult ConfigClient::codexCatalogApply() const
{
    return runClient({"client", "codex", "catalog-apply", "--json"}, "codex");
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

void ConfigClient::discoverModels(const ConfigSnapshot &snapshot,
                                  int providerIndex, ModelProtocol protocol,
                                  const QMap<int, QString> &apiKeys)
{
    if (providerIndex < 0 || providerIndex >= snapshot.providers.size()) {
        emit discoveryFailed({}, "Provider is no longer available");
        return;
    }
    const QString providerPrefix = snapshot.providers[providerIndex].prefix;
    const QByteArray input = serialize(snapshot, apiKeys);
    auto *process = new QProcess(this);
    const QStringList arguments{
        "--config", m_configPath, "config-discover-models", "--stdin-json",
        "--provider-index", QString::number(providerIndex),
        "--protocol", protocolArgument(protocol),
    };
    connect(process, &QProcess::started, process, [process, input] {
        process->write(input);
        process->closeWriteChannel();
    });
    connect(process, &QProcess::errorOccurred, this,
            [this, process, providerPrefix](QProcess::ProcessError error) {
        if (error != QProcess::FailedToStart) return;
        emit discoveryFailed(providerPrefix, "Core command could not start");
        process->deleteLater();
    });
    connect(process, qOverload<int, QProcess::ExitStatus>(&QProcess::finished),
            this, [this, process, providerPrefix](int exitCode,
                                                  QProcess::ExitStatus exitStatus) {
        const QByteArray payload = process->readAllStandardOutput();
        process->deleteLater();
        QJsonParseError parseError;
        const QJsonDocument document = QJsonDocument::fromJson(payload, &parseError);
        if (exitStatus != QProcess::NormalExit || exitCode != 0 ||
            parseError.error != QJsonParseError::NoError || !document.isObject()) {
            emit discoveryFailed(providerPrefix, "Core returned invalid model discovery data");
            return;
        }
        const QJsonObject object = document.object();
        if (!object.value("ok").toBool()) {
            emit discoveryFailed(providerPrefix,
                                 object.value("error").toString("Model discovery failed"));
            return;
        }
        QStringList models;
        for (const QJsonValue &value : object.value("models").toArray())
            models.append(value.toString());
        emit modelsDiscovered(object.value("provider").toString(providerPrefix),
                              models);
    });
    process->start(m_executable, arguments);
}
