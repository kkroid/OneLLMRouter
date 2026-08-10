#include "main_window.h"
#include "platform/platform.h"

#include <QComboBox>
#include <QFormLayout>
#include <QHBoxLayout>
#include <QLabel>
#include <QLineEdit>
#include <QListWidget>
#include <QPushButton>
#include <QSplitter>
#include <QTabWidget>
#include <QVBoxLayout>

namespace {

QPushButton *button(const QString &text, const QString &name, QWidget *parent)
{
    auto *result = new QPushButton(text, parent);
    result->setObjectName(name);
    return result;
}

QStringList reasoningLevels(const QString &text)
{
    QStringList levels;
    for (const QString &part : text.split(',', Qt::SkipEmptyParts)) {
        const QString level = part.trimmed();
        if (!level.isEmpty()) levels.append(level);
    }
    return levels;
}

} // namespace

MainWindow::MainWindow(ConfigClient *client, bool readOnly, QWidget *parent,
                       UsageClient *usageClient)
    : QMainWindow(parent), m_client(client),
      m_usageClient(usageClient ? usageClient
                                : new UsageClient(Platform::coreExecutablePath(), this)),
      m_readOnly(readOnly)
{
    buildUi();
    load();
    setReadOnly(readOnly);
}

void MainWindow::buildUi()
{
    setWindowTitle("OneLLMRouter Configuration");
    resize(850, 560);
    auto *central = new QWidget(this);
    auto *layout = new QVBoxLayout(central);
    m_tabs = new QTabWidget(central);

    auto *providersPage = new QWidget(m_tabs);
    auto *providersLayout = new QHBoxLayout(providersPage);
    auto *providerSide = new QVBoxLayout;
    m_providerList = new QListWidget(providersPage);
    m_providerList->setObjectName("providerList");
    providerSide->addWidget(m_providerList);
    auto *providerButtons = new QHBoxLayout;
    QPushButton *add = button("Add", "addProvider", providersPage);
    QPushButton *remove = button("Remove", "removeProvider", providersPage);
    providerButtons->addWidget(add);
    providerButtons->addWidget(remove);
    providerSide->addLayout(providerButtons);
    providersLayout->addLayout(providerSide, 1);
    auto *form = new QFormLayout;
    m_name = new QLineEdit(providersPage); m_name->setObjectName("providerName");
    m_prefix = new QLineEdit(providersPage); m_prefix->setObjectName("providerPrefix");
    m_baseUrl = new QLineEdit(providersPage); m_baseUrl->setObjectName("anthropicBaseUrl");
    m_openAIBaseUrl = new QLineEdit(providersPage); m_openAIBaseUrl->setObjectName("openAIBaseUrl");
    m_responsesBaseUrl = new QLineEdit(providersPage); m_responsesBaseUrl->setObjectName("responsesBaseUrl");
    m_apiKey = new QLineEdit(providersPage); m_apiKey->setObjectName("apiKey");
    m_apiKey->setEchoMode(QLineEdit::Password); m_apiKey->setPlaceholderText("Leave blank to preserve existing key");
    m_proxy = new QComboBox(providersPage); m_proxy->setObjectName("proxyPolicy");
    m_proxy->addItems({"Inherit global", "Use proxy", "Direct"});
    form->addRow("Name", m_name); form->addRow("Prefix", m_prefix);
    form->addRow("Anthropic Base URL", m_baseUrl);
    form->addRow("OpenAI Chat Base URL", m_openAIBaseUrl);
    form->addRow("OpenAI Responses Base URL", m_responsesBaseUrl);
    form->addRow("API Key", m_apiKey); form->addRow("Proxy", m_proxy);
    QPushButton *update = button("Update Provider", "updateProvider", providersPage);
    form->addRow(update);
    providersLayout->addLayout(form, 2);
    m_tabs->addTab(providersPage, "Providers");

    auto *modelsPage = new QWidget(m_tabs);
    auto *modelsLayout = new QVBoxLayout(modelsPage);
    m_modelList = new QListWidget(modelsPage); m_modelList->setObjectName("modelList");
    modelsLayout->addWidget(m_modelList);
    auto *modelRow = new QHBoxLayout;
    m_modelName = new QLineEdit(modelsPage); m_modelName->setObjectName("modelName");
    m_modelName->setPlaceholderText("Model ID");
    QPushButton *addModelButton = button("Add", "addModel", modelsPage);
    QPushButton *removeModelButton = button("Remove", "removeModel", modelsPage);
    m_protocol = new QComboBox(modelsPage); m_protocol->setObjectName("discoveryProtocol");
    m_protocol->addItems({"Anthropic", "OpenAI Chat", "OpenAI Responses"});
    QPushButton *discover = button("Discover", "discoverModels", modelsPage);
    modelRow->addWidget(m_modelName); modelRow->addWidget(addModelButton);
    modelRow->addWidget(removeModelButton); modelRow->addWidget(m_protocol); modelRow->addWidget(discover);
    modelsLayout->addLayout(modelRow);
    auto *reasoning = new QFormLayout;
    m_defaultReasoning = new QLineEdit(modelsPage); m_defaultReasoning->setObjectName("defaultReasoning");
    m_supportedReasoning = new QLineEdit(modelsPage); m_supportedReasoning->setObjectName("supportedReasoning");
    reasoning->addRow("Default reasoning", m_defaultReasoning);
    reasoning->addRow("Supported reasoning (comma separated)", m_supportedReasoning);
    modelsLayout->addLayout(reasoning);
    m_tabs->addTab(modelsPage, "Models");

    m_clientsPage = new ClientsPage(m_client, m_tabs);
    m_clientsPage->setObjectName("clientsPage");
    m_tabs->addTab(m_clientsPage, "Clients");

    auto *usagePage = new UsagePage(m_usageClient, m_tabs);
    usagePage->setObjectName("usagePage");
    m_tabs->addTab(usagePage, "Usage");

    layout->addWidget(m_tabs);
    m_status = new QLabel(central); m_status->setObjectName("statusLabel");
    m_save = button("Validate and Save", "saveConfig", central);
    layout->addWidget(m_status); layout->addWidget(m_save);
    setCentralWidget(central);

    m_editControls = {add, remove, update, addModelButton, removeModelButton,
                      discover, m_name, m_prefix, m_baseUrl, m_openAIBaseUrl,
                      m_responsesBaseUrl, m_apiKey, m_proxy, m_modelName,
                      m_protocol, m_defaultReasoning, m_supportedReasoning, m_save};
    connect(m_providerList, &QListWidget::currentRowChanged, this, &MainWindow::selectProvider);
    connect(add, &QPushButton::clicked, this, &MainWindow::addProvider);
    connect(remove, &QPushButton::clicked, this, &MainWindow::removeProvider);
    connect(update, &QPushButton::clicked, this, &MainWindow::saveProvider);
    connect(addModelButton, &QPushButton::clicked, this, &MainWindow::addModel);
    connect(removeModelButton, &QPushButton::clicked, this, &MainWindow::removeModel);
    connect(discover, &QPushButton::clicked, this, &MainWindow::discoverModels);
    connect(m_modelList, &QListWidget::currentTextChanged, this, [this] { refreshReasoning(); });
    connect(m_defaultReasoning, &QLineEdit::textEdited, this,
            [this](const QString &text) {
        if (m_modelList->currentItem())
            m_snapshot.codexModels[m_modelList->currentItem()->text()].defaultLevel =
                text;
    });
    connect(m_supportedReasoning, &QLineEdit::textEdited, this,
            [this](const QString &text) {
        if (m_modelList->currentItem())
            m_snapshot.codexModels[m_modelList->currentItem()->text()].supportedLevels =
                reasoningLevels(text);
    });
    connect(m_clientsPage, &ClientsPage::modelSlotChanged, this,
            [this](const QString &slot, const QString &value) {
                m_snapshot.modelSlots.insert(slot, value);
            });
    connect(m_save, &QPushButton::clicked, this, &MainWindow::save);
    connect(m_client, &ConfigClient::modelsDiscovered, this,
            [this](const QString &providerPrefix, const QStringList &models) {
        int row = -1;
        for (int index = 0; index < m_snapshot.providers.size(); ++index) {
            if (m_snapshot.providers[index].prefix == providerPrefix) {
                row = index;
                break;
            }
        }
        if (row < 0) return;
        for (const QString &model : models)
            if (!m_snapshot.providers[row].models.contains(model)) m_snapshot.providers[row].models.append(model);
        if (row == m_providerList->currentRow()) refreshModels();
        m_status->setText(QString("Discovered %1 models").arg(models.size()));
    });
    connect(m_client, &ConfigClient::discoveryFailed, this,
            [this](const QString &providerPrefix, const QString &message) {
        for (const ProviderConfigSnapshot &provider : m_snapshot.providers) {
            if (provider.prefix == providerPrefix) {
                m_status->setText(message);
                return;
            }
        }
    });
}

void MainWindow::load()
{
    const ConfigResult result = m_client->load(&m_snapshot);
    showResult(result);
    if (result.succeeded) refreshProviders();
}

void MainWindow::setReadOnly(bool readOnly)
{
    m_readOnly = readOnly;
    for (QWidget *control : m_editControls) control->setEnabled(!readOnly);
    m_clientsPage->setReadOnly(readOnly);
    if (readOnly) m_status->setText("Externally managed Core: configuration is read-only");
    else if (m_status->text() == "Externally managed Core: configuration is read-only")
        m_status->clear();
}

void MainWindow::refreshProviders()
{
    const int row = qMax(0, m_providerList->currentRow());
    m_providerList->clear();
    for (const ProviderConfigSnapshot &provider : m_snapshot.providers)
        m_providerList->addItem(provider.name.isEmpty() ? provider.prefix : provider.name);
    if (!m_snapshot.providers.isEmpty()) m_providerList->setCurrentRow(qMin(row, m_snapshot.providers.size() - 1));
    m_clientsPage->setConfiguration(m_snapshot);
}

void MainWindow::selectProvider(int row)
{
    if (row < 0 || row >= m_snapshot.providers.size()) return;
    const ProviderConfigSnapshot &provider = m_snapshot.providers[row];
    m_name->setText(provider.name); m_prefix->setText(provider.prefix);
    m_baseUrl->setText(provider.baseUrl); m_openAIBaseUrl->setText(provider.openAIBaseUrl);
    m_responsesBaseUrl->setText(provider.responsesBaseUrl);
    m_apiKey->setText(m_apiKeys.value(row));
    m_apiKey->setPlaceholderText(provider.apiKeySet ? "Key is set; leave blank to preserve" : "API key required");
    m_proxy->setCurrentIndex(int(provider.proxy));
    refreshModels();
}

void MainWindow::saveProvider()
{
    const int row = m_providerList->currentRow();
    if (row < 0) return;
    ProviderConfigSnapshot &provider = m_snapshot.providers[row];
    provider.name = m_name->text(); provider.prefix = m_prefix->text();
    provider.baseUrl = m_baseUrl->text(); provider.openAIBaseUrl = m_openAIBaseUrl->text();
    provider.responsesBaseUrl = m_responsesBaseUrl->text();
    provider.proxy = ProviderConfigSnapshot::ProxyPolicy(m_proxy->currentIndex());
    if (m_apiKey->text().isEmpty()) m_apiKeys.remove(row);
    else m_apiKeys.insert(row, m_apiKey->text());
    refreshProviders();
}

void MainWindow::addProvider()
{
    m_snapshot.providers.append(ProviderConfigSnapshot{});
    refreshProviders();
    m_providerList->setCurrentRow(m_snapshot.providers.size() - 1);
}

void MainWindow::removeProvider()
{
    const int row = m_providerList->currentRow();
    if (row >= 0) {
        QMap<int, QString> shiftedKeys;
        for (auto iterator = m_apiKeys.cbegin(); iterator != m_apiKeys.cend(); ++iterator) {
            if (iterator.key() < row) shiftedKeys.insert(iterator.key(), iterator.value());
            if (iterator.key() > row) shiftedKeys.insert(iterator.key() - 1, iterator.value());
        }
        m_apiKeys = shiftedKeys;
        m_snapshot.providers.removeAt(row);
        refreshProviders();
    }
}

void MainWindow::refreshModels()
{
    m_modelList->clear();
    const int row = m_providerList->currentRow();
    if (row < 0) return;
    m_snapshot.providers[row].models.sort();
    m_modelList->addItems(m_snapshot.providers[row].models);
    if (m_modelList->count()) m_modelList->setCurrentRow(0);
}

void MainWindow::addModel()
{
    const int row = m_providerList->currentRow();
    const QString model = m_modelName->text().trimmed();
    if (row >= 0 && !model.isEmpty() && !m_snapshot.providers[row].models.contains(model)) {
        m_snapshot.providers[row].models.append(model); m_modelName->clear(); refreshModels();
    }
}

void MainWindow::removeModel()
{
    const int provider = m_providerList->currentRow();
    if (provider >= 0 && m_modelList->currentRow() >= 0) {
        m_snapshot.providers[provider].models.removeAll(m_modelList->currentItem()->text());
        refreshModels();
    }
}

void MainWindow::discoverModels()
{
    saveProvider();
    const int row = m_providerList->currentRow();
    if (row < 0) return;
    const ProviderConfigSnapshot &provider = m_snapshot.providers[row];
    if (provider.apiKeySet && m_apiKey->text().isEmpty()) {
        m_status->setText("Re-enter the API key to discover models");
        return;
    }
    m_client->discoverModels(provider, ModelProtocol(m_protocol->currentIndex()),
                             m_apiKey->text());
}

void MainWindow::refreshReasoning()
{
    const QString model = m_modelList->currentItem() ? m_modelList->currentItem()->text() : QString();
    const CodexReasoningConfig config = m_snapshot.codexModels.value(model);
    m_defaultReasoning->setText(config.defaultLevel);
    m_supportedReasoning->setText(config.supportedLevels.join(','));
}

QMap<int, QString> MainWindow::pendingKeys() const
{
    return m_apiKeys;
}

void MainWindow::save()
{
    saveProvider();
    const QMap<int, QString> keys = pendingKeys();
    ConfigResult result = m_client->validate(m_snapshot, keys);
    if (result.succeeded) result = m_client->apply(m_snapshot, keys);
    showResult(result);
    if (result.succeeded) {
        m_apiKeys.clear();
        m_apiKey->clear();
        m_status->setText("Configuration saved. Restart Core to apply changes.");
        ConfigSnapshot reloaded;
        if (m_client->load(&reloaded).succeeded) {
            m_snapshot = reloaded;
            refreshProviders();
            m_status->setText("Configuration saved. Restart Core to apply changes.");
        }
    }
}

void MainWindow::showResult(const ConfigResult &result)
{
    if (result.succeeded) { m_status->clear(); return; }
    if (!result.fieldErrors.isEmpty()) {
        const ConfigFieldError &error = result.fieldErrors.first();
        m_status->setText(error.field + ": " + error.message);
    } else m_status->setText(result.error);
}
