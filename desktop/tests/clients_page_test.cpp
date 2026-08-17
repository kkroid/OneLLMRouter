#include <QtTest>
#include <QComboBox>
#include <QFile>
#include <QLabel>
#include <QPushButton>
#include <QTemporaryDir>

#include "clients/clients_page.h"

class ClientsPageTest : public QObject
{
    Q_OBJECT
private slots:
    void showsConfiguredModelsAndKeyState();
    void filtersModelsByEndpoint();
    void externalCoreDisablesMutations();
    void previewAvailabilityTracksConfiguration();
};

static ConfigSnapshot snapshot()
{
    ConfigSnapshot value;
    ProviderConfigSnapshot anthropic;
    anthropic.prefix = "alpha";
    anthropic.baseUrl = "https://alpha.invalid";
    anthropic.apiKeySet = true;
    anthropic.models = {"claude-model"};
    anthropic.modelDefinitions = {
        QJsonObject{{"id", "claude-model"},
                    {"endpoints", QJsonArray{"anthropic"}}},
    };
    value.providers.append(anthropic);
    ProviderConfigSnapshot responses;
    responses.prefix = "beta";
    responses.responsesBaseUrl = "https://beta.invalid";
    responses.models = {"codex-model"};
    responses.modelDefinitions = {
        QJsonObject{{"id", "codex-model"},
                    {"endpoints", QJsonArray{"responses"}}},
    };
    value.providers.append(responses);
    value.modelSlots.insert("default", "alpha/claude-model");
    return value;
}

void ClientsPageTest::filtersModelsByEndpoint()
{
    ConfigSnapshot value;
    ProviderConfigSnapshot provider;
    provider.prefix = "ds";
    provider.baseUrl = "https://api.invalid/anthropic";
    provider.responsesBaseUrl = "https://api.invalid";
    provider.models = {"deepseek-v4-pro[1m]", "deepseek-v4-pro"};
    provider.modelDefinitions = {
        QJsonObject{{"id", "deepseek-v4-pro[1m]"},
                    {"endpoints", QJsonArray{"anthropic"}}},
        QJsonObject{{"id", "deepseek-v4-pro"},
                    {"endpoints", QJsonArray{"responses"}}},
    };
    value.providers.append(provider);

    ConfigClient client("missing-core", "missing.yaml");
    ClientsPage page(&client);
    page.setConfiguration(value);
    auto *slot = page.findChild<QComboBox *>("slot_default");
    auto *codex = page.findChild<QComboBox *>("codexPreviewModel");
    QVERIFY(slot && codex);
    QVERIFY(slot->findText("ds/deepseek-v4-pro[1m]") >= 0);
    QCOMPARE(slot->findText("ds/deepseek-v4-pro"), -1);
    QCOMPARE(codex->findText("ds/deepseek-v4-pro[1m]"), -1);
    QVERIFY(codex->findText("ds/deepseek-v4-pro") >= 0);
}

void ClientsPageTest::showsConfiguredModelsAndKeyState()
{
    ConfigClient client("missing-core", "missing.yaml");
    ClientsPage page(&client);
    page.setConfiguration(snapshot());
    auto *slot = page.findChild<QComboBox *>("slot_default");
    auto *codex = page.findChild<QComboBox *>("codexPreviewModel");
    QVERIFY(slot && codex);
    QCOMPARE(slot->currentText(), QString("alpha/claude-model"));
    QCOMPARE(slot->findText("beta/codex-model"), -1);
    QCOMPARE(codex->currentText(), QString("beta/codex-model"));
    const QString keys = page.findChild<QLabel *>("claudeApiKeyState")->text();
    QVERIFY(keys.contains("1 of 2"));
    QVERIFY(keys.contains("api_key_set"));
}

void ClientsPageTest::externalCoreDisablesMutations()
{
    ConfigClient client("missing-core", "missing.yaml");
    ClientsPage page(&client);
    page.setConfiguration(snapshot());
    page.setReadOnly(true);
    QVERIFY(!page.findChild<QComboBox *>("slot_default")->isEnabled());
    QVERIFY(!page.findChild<QPushButton *>("claudeApply")->isEnabled());
    QVERIFY(!page.findChild<QPushButton *>("claudeRestore")->isEnabled());
    QVERIFY(!page.findChild<QPushButton *>("codexCatalogApply")->isEnabled());
    QVERIFY(page.findChild<QPushButton *>("claudeCheck")->isEnabled());
    QVERIFY(page.findChild<QPushButton *>("codexCheck")->isEnabled());
}

void ClientsPageTest::previewAvailabilityTracksConfiguration()
{
    ConfigClient client("missing-core", "missing.yaml");
    ClientsPage page(&client);
    auto *preview = page.findChild<QPushButton *>("codexPreview");
    QVERIFY(preview);

    page.setConfiguration(snapshot());
    QVERIFY(preview->isEnabled());
    page.setConfiguration(ConfigSnapshot{});
    QVERIFY(!preview->isEnabled());
    page.setConfiguration(snapshot());
    QVERIFY(preview->isEnabled());

    page.setReadOnly(true);
    QVERIFY(preview->isEnabled());
    page.setConfiguration(ConfigSnapshot{});
    QVERIFY(!preview->isEnabled());
    page.setConfiguration(snapshot());
    QVERIFY(preview->isEnabled());
}

QTEST_MAIN(ClientsPageTest)
#include "clients_page_test.moc"
