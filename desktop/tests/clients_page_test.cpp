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
    value.providers.append(anthropic);
    ProviderConfigSnapshot responses;
    responses.prefix = "beta";
    responses.responsesBaseUrl = "https://beta.invalid";
    responses.models = {"codex-model"};
    value.providers.append(responses);
    value.modelSlots.insert("default", "alpha/claude-model");
    return value;
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
