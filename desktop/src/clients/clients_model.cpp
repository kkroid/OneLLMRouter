#include "clients_model.h"

#include <QJsonArray>

namespace {

ClientCommandResult parseEnvelope(const QJsonObject &root, const QString &client)
{
    ClientCommandResult result;
    if (root.value("schema_version").toInt() != 1 ||
        root.value("client").toString() != client ||
        !root.value("result").isObject() || !root.value("errors").isArray()) {
        result.error = "Core returned an unsupported client response";
        return result;
    }
    result.succeeded = root.value("ok").toBool(false);
    for (const QJsonValue &value : root.value("errors").toArray()) {
        const QJsonObject error = value.toObject();
        result.errors.append({error.value("code").toString(),
                              error.value("message").toString(),
                              error.value("path").toString()});
    }
    if (!result.succeeded && !result.errors.isEmpty())
        result.error = result.errors.first().message;
    return result;
}

QStringList strings(const QJsonValue &value)
{
    QStringList result;
    for (const QJsonValue &item : value.toArray()) result.append(item.toString());
    return result;
}

} // namespace

ClientCommandResult parseClaudeClientEnvelope(const QJsonObject &root)
{
    ClientCommandResult result = parseEnvelope(root, "claude");
    if (!result.error.isEmpty() && !root.value("result").isObject()) return result;
    const QJsonObject value = root.value("result").toObject();
    result.claude = {
        value.value("settings_path").toString(), value.value("backup_path").toString(),
        value.value("parse_state").toString(), value.value("sync_state").toString(),
        value.value("exists").toBool(), value.value("backup_exists").toBool(),
        value.value("managed_key_count").toInt(),
        value.value("configured_model_count").toInt(), value.value("changed").toBool(),
        value.value("backup_created").toBool(), value.value("atomic_replaced").toBool()};
    return result;
}

ClientCommandResult parseCodexClientEnvelope(const QJsonObject &root)
{
    ClientCommandResult result = parseEnvelope(root, "codex");
    if (!result.error.isEmpty() && !root.value("result").isObject()) return result;
    const QJsonObject value = root.value("result").toObject();
    CodexClientState &state = result.codex;
    state.configPath = value.value("config_path").toString();
    state.configParseState = value.value("config_parse_state").toString();
    state.configSyncState = value.value("config_sync_state").toString();
    state.oneLLMCatalogPath = value.value("onellm_catalog_path").toString();
    state.codexCatalogPath = value.value("codex_catalog_path").toString();
    state.catalogSyncState = value.value("catalog_sync_state").toString();
    state.catalogGeneratedAt = value.value("catalog_generated_at").toString();
    state.effectiveModel = value.value("effective_model").toString();
    state.effectiveProvider = value.value("effective_provider").toString();
    state.configuredModelProvider = value.value("configured_model_provider").toString();
    state.sourceTag = value.value("source_tag").toString();
    state.snippet = value.value("snippet").toString();
    state.sourceProviders = strings(value.value("source_providers"));
    state.writtenPaths = strings(value.value("written_paths"));
    state.modelCount = value.value("model_count").toInt();
    state.configExists = value.value("config_exists").toBool();
    state.oneLLMCatalogExists = value.value("onellm_catalog_exists").toBool();
    state.codexCatalogExists = value.value("codex_catalog_exists").toBool();
    state.overwriteCatalog = value.value("overwrite_catalog").toBool();
    state.configWriteSupported = value.value("config_write_supported").toBool();
    return result;
}
