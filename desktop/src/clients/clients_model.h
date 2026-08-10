#pragma once

#include <QJsonObject>
#include <QList>
#include <QString>
#include <QStringList>

struct ClientError {
    QString code;
    QString message;
    QString path;
};

struct ClaudeClientState {
    QString settingsPath;
    QString backupPath;
    QString parseState;
    QString syncState;
    bool exists = false;
    bool backupExists = false;
    int managedKeyCount = 0;
    int configuredModelCount = 0;
    bool changed = false;
    bool backupCreated = false;
    bool atomicReplaced = false;
};

struct CodexClientState {
    QString configPath;
    QString configParseState;
    QString configSyncState;
    QString oneLLMCatalogPath;
    QString codexCatalogPath;
    QString catalogSyncState;
    QString catalogGeneratedAt;
    QString effectiveModel;
    QString effectiveProvider;
    QString configuredModelProvider;
    QString sourceTag;
    QString snippet;
    QStringList sourceProviders;
    QStringList writtenPaths;
    int modelCount = 0;
    bool configExists = false;
    bool oneLLMCatalogExists = false;
    bool codexCatalogExists = false;
    bool overwriteCatalog = false;
    bool configWriteSupported = false;
};

struct ClientCommandResult {
    bool succeeded = false;
    QString error;
    QList<ClientError> errors;
    ClaudeClientState claude;
    CodexClientState codex;
};

ClientCommandResult parseClaudeClientEnvelope(const QJsonObject &root);
ClientCommandResult parseCodexClientEnvelope(const QJsonObject &root);
