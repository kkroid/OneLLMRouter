#include <QtTest>
#include <QJsonArray>

#include "clients/clients_model.h"

class ClientsModelTest : public QObject
{
    Q_OBJECT
private slots:
    void parsesClaudeFailureWithoutSecrets();
    void parsesCodexPreview();
};

void ClientsModelTest::parsesClaudeFailureWithoutSecrets()
{
    const QJsonObject root{
        {"schema_version", 1}, {"client", "claude"}, {"operation", "apply"},
        {"ok", false},
        {"errors", QJsonArray{QJsonObject{{"code", "settings_invalid"},
                                          {"message", "settings are invalid"},
                                          {"path", "C:/test/settings.json"}}}},
        {"result", QJsonObject{{"settings_path", "C:/test/settings.json"},
                                {"parse_state", "invalid"},
                                {"sync_state", "invalid"},
                                {"configured_model_count", 2}}}};
    const ClientCommandResult result = parseClaudeClientEnvelope(root);
    QVERIFY(!result.succeeded);
    QCOMPARE(result.error, QString("settings are invalid"));
    QCOMPARE(result.claude.parseState, QString("invalid"));
    QCOMPARE(result.claude.configuredModelCount, 2);
}

void ClientsModelTest::parsesCodexPreview()
{
    const QJsonObject root{
        {"schema_version", 1}, {"client", "codex"}, {"operation", "preview"},
        {"ok", true}, {"errors", QJsonArray{}},
        {"result", QJsonObject{{"source_tag", "OneLLMRouter"},
                                {"model_count", 3},
                                {"effective_model", "alpha/model"},
                                {"source_providers", QJsonArray{"alpha"}},
                                {"written_paths", QJsonArray{}},
                                {"snippet", "model = \"alpha/model\"\n"}}}};
    const ClientCommandResult result = parseCodexClientEnvelope(root);
    QVERIFY(result.succeeded);
    QCOMPARE(result.codex.sourceTag, QString("OneLLMRouter"));
    QCOMPARE(result.codex.modelCount, 3);
    QCOMPARE(result.codex.snippet, QString("model = \"alpha/model\"\n"));
}

QTEST_MAIN(ClientsModelTest)
#include "clients_model_test.moc"
