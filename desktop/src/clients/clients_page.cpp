#include "clients_page.h"

#include <QComboBox>
#include <QFormLayout>
#include <QGroupBox>
#include <QHBoxLayout>
#include <QLabel>
#include <QPlainTextEdit>
#include <QPushButton>
#include <QVBoxLayout>

namespace {

QLabel *valueLabel(const QString &name, QWidget *parent)
{
    auto *label = new QLabel(parent);
    label->setObjectName(name);
    label->setTextInteractionFlags(Qt::TextSelectableByMouse);
    label->setWordWrap(true);
    return label;
}

} // namespace

ClientsPage::ClientsPage(ConfigClient *client, QWidget *parent)
    : QWidget(parent), m_client(client)
{
    auto *layout = new QVBoxLayout(this);
    auto *claude = new QGroupBox("Claude Code", this);
    auto *claudeLayout = new QFormLayout(claude);
    m_claudePath = valueLabel("claudeSettingsPath", claude);
    m_claudeState = valueLabel("claudeState", claude);
    m_claudeKeys = valueLabel("claudeApiKeyState", claude);
    m_claudeError = valueLabel("claudeError", claude);
    claudeLayout->addRow("Settings", m_claudePath);
    claudeLayout->addRow("Status", m_claudeState);
    claudeLayout->addRow("Provider keys", m_claudeKeys);
    for (const QString &slot : {"default", "opus", "sonnet", "haiku", "fable"}) {
        auto *selector = new QComboBox(claude);
        selector->setObjectName("slot_" + slot);
        m_slots.insert(slot, selector);
        claudeLayout->addRow(slot.left(1).toUpper() + slot.mid(1), selector);
        connect(selector, &QComboBox::currentTextChanged, this,
                [this, slot](const QString &model) { emit modelSlotChanged(slot, model); });
    }
    auto *claudeActions = new QHBoxLayout;
    auto *claudeCheck = new QPushButton("Check", claude);
    claudeCheck->setObjectName("claudeCheck");
    m_claudeApply = new QPushButton("Apply", claude);
    m_claudeApply->setObjectName("claudeApply");
    m_claudeRestore = new QPushButton("Restore", claude);
    m_claudeRestore->setObjectName("claudeRestore");
    claudeActions->addWidget(claudeCheck);
    claudeActions->addWidget(m_claudeApply);
    claudeActions->addWidget(m_claudeRestore);
    claudeLayout->addRow(claudeActions);
    claudeLayout->addRow(m_claudeError);
    layout->addWidget(claude);

    auto *codex = new QGroupBox("Codex", this);
    auto *codexLayout = new QFormLayout(codex);
    m_codexConfigPath = valueLabel("codexConfigPath", codex);
    m_codexCatalogPaths = valueLabel("codexCatalogPaths", codex);
    m_codexState = valueLabel("codexState", codex);
    m_codexEffective = valueLabel("codexEffective", codex);
    m_codexSource = valueLabel("codexSource", codex);
    m_codexError = valueLabel("codexError", codex);
    m_codexModel = new QComboBox(codex);
    m_codexModel->setObjectName("codexPreviewModel");
    m_snippet = new QPlainTextEdit(codex);
    m_snippet->setObjectName("codexSnippet");
    m_snippet->setReadOnly(true);
    m_snippet->setMaximumBlockCount(30);
    codexLayout->addRow("Config", m_codexConfigPath);
    codexLayout->addRow("Catalogs", m_codexCatalogPaths);
    codexLayout->addRow("Status", m_codexState);
    codexLayout->addRow("Effective", m_codexEffective);
    codexLayout->addRow("Source", m_codexSource);
    codexLayout->addRow("Preview model", m_codexModel);
    auto *codexActions = new QHBoxLayout;
    auto *codexCheck = new QPushButton("Check", codex);
    codexCheck->setObjectName("codexCheck");
    m_codexPreview = new QPushButton("Preview", codex);
    m_codexPreview->setObjectName("codexPreview");
    m_codexApply = new QPushButton("Sync Catalog", codex);
    m_codexApply->setObjectName("codexCatalogApply");
    codexActions->addWidget(codexCheck);
    codexActions->addWidget(m_codexPreview);
    codexActions->addWidget(m_codexApply);
    codexLayout->addRow(codexActions);
    codexLayout->addRow("Snippet", m_snippet);
    codexLayout->addRow(m_codexError);
    layout->addWidget(codex);
    layout->addStretch();

    connect(claudeCheck, &QPushButton::clicked, this, &ClientsPage::refreshClaude);
    connect(m_claudeApply, &QPushButton::clicked, this,
            &ClientsPage::claudeApplyRequested);
    connect(m_claudeRestore, &QPushButton::clicked, this, &ClientsPage::restoreClaude);
    connect(codexCheck, &QPushButton::clicked, this, &ClientsPage::refreshCodex);
    connect(m_codexPreview, &QPushButton::clicked, this, &ClientsPage::previewCodex);
    connect(m_codexApply, &QPushButton::clicked, this, &ClientsPage::applyCodexCatalog);
}

void ClientsPage::setConfiguration(const ConfigSnapshot &snapshot)
{
    QStringList anthropicModels;
    QStringList responsesModels;
    int configuredKeys = 0;
    for (const ProviderConfigSnapshot &provider : snapshot.providers) {
        if (provider.apiKeySet) ++configuredKeys;
        for (const QString &model : provider.models) {
            const QString namespaced = provider.prefix + "/" + model;
            if (!provider.baseUrl.isEmpty()) anthropicModels.append(namespaced);
            if (!provider.responsesBaseUrl.isEmpty()) responsesModels.append(namespaced);
        }
    }
    anthropicModels.removeDuplicates();
    anthropicModels.sort();
    responsesModels.removeDuplicates();
    responsesModels.sort();
    for (auto iterator = m_slots.begin(); iterator != m_slots.end(); ++iterator) {
        QComboBox *selector = iterator.value();
        selector->blockSignals(true);
        selector->clear();
        selector->addItem(QString());
        selector->addItems(anthropicModels);
        const QString selected = snapshot.modelSlots.value(iterator.key());
        if (!selected.isEmpty() && selector->findText(selected) < 0) selector->addItem(selected);
        selector->setCurrentText(selected);
        selector->blockSignals(false);
    }
    m_claudeKeys->setText(QString("%1 of %2 configured (api_key_set only)")
                              .arg(configuredKeys).arg(snapshot.providers.size()));
    m_codexModel->clear();
    m_codexModel->addItems(responsesModels);
    updatePreviewAvailability();
}

void ClientsPage::setReadOnly(bool readOnly)
{
    for (QComboBox *slot : m_slots) slot->setEnabled(!readOnly);
    m_claudeApply->setEnabled(!readOnly);
    m_claudeRestore->setEnabled(!readOnly);
    m_codexApply->setEnabled(!readOnly);
    updatePreviewAvailability();
}

void ClientsPage::updatePreviewAvailability()
{
    m_codexPreview->setEnabled(!m_codexModel->currentText().isEmpty());
}

QString ClientsPage::display(const QString &value)
{
    return value.isEmpty() ? QStringLiteral("Not set") : value;
}

void ClientsPage::refreshClaude() { showClaude(m_client->claudeStatus()); }
void ClientsPage::restoreClaude() { showClaude(m_client->claudeRestore()); }
void ClientsPage::refreshCodex() { showCodex(m_client->codexStatus()); }
void ClientsPage::previewCodex() { showCodex(m_client->codexPreview(m_codexModel->currentText())); }
void ClientsPage::applyCodexCatalog() { showCodex(m_client->codexCatalogApply()); }

void ClientsPage::showClaude(const ClientCommandResult &result)
{
    const ClaudeClientState &state = result.claude;
    m_claudePath->setText(display(state.settingsPath));
    m_claudeState->setText(QString("%1 / %2; backup %3; %4 configured models")
                               .arg(display(state.parseState), display(state.syncState),
                                    state.backupExists ? "present" : "absent")
                               .arg(state.configuredModelCount));
    m_claudeError->setText(result.succeeded ? QString() : result.error);
}

void ClientsPage::showClaudeResult(const ClientCommandResult &result)
{
    showClaude(result);
}

void ClientsPage::showConfigurationError(const ConfigResult &result)
{
    if (!result.fieldErrors.isEmpty()) {
        const ConfigFieldError &error = result.fieldErrors.first();
        m_claudeError->setText(error.field + ": " + error.message);
    } else {
        m_claudeError->setText(result.error);
    }
}

void ClientsPage::showCodex(const ClientCommandResult &result)
{
    const CodexClientState &state = result.codex;
    m_codexConfigPath->setText(display(state.configPath));
    m_codexCatalogPaths->setText(QString("OneLLMRouter: %1\nCodex: %2")
                                     .arg(display(state.oneLLMCatalogPath),
                                          display(state.codexCatalogPath)));
    m_codexState->setText(QString("config %1 / catalog %2; %3 models")
                              .arg(display(state.configSyncState),
                                   display(state.catalogSyncState)).arg(state.modelCount));
    m_codexEffective->setText(QString("%1 (provider %2)")
                                  .arg(display(state.effectiveModel),
                                       display(state.effectiveProvider)));
    m_codexSource->setText(display(state.sourceTag));
    m_snippet->setPlainText(state.snippet);
    m_codexError->setText(result.succeeded ? QString() : result.error);
}
