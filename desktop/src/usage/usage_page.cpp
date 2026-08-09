#include "usage_page.h"

#include <QComboBox>
#include <QHeaderView>
#include <QHBoxLayout>
#include <QLabel>
#include <QLineEdit>
#include <QProcess>
#include <QPushButton>
#include <QTableView>
#include <QVBoxLayout>

UsageClient::UsageClient(QString executable, QObject *parent)
    : QObject(parent), m_executable(std::move(executable))
{
}

UsageResult UsageClient::load(const QString &period, const QString &label) const
{
    QProcess process;
    QStringList arguments{"stats", period};
    if (!label.trimmed().isEmpty()) arguments.append(label.trimmed());
    arguments.append("--json");
    process.start(m_executable, arguments);
    if (!process.waitForStarted(5000)) return {false, "Core stats command could not start"};
    if (!process.waitForFinished(10000)) {
        process.kill();
        process.waitForFinished();
        return {false, "Core stats command timed out"};
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0)
        return {false, "Core stats command failed"};
    return UsageModel::parse(process.readAllStandardOutput());
}

UsagePage::UsagePage(UsageClient *client, QWidget *parent)
    : QWidget(parent), m_client(client), m_model(new UsageModel(this))
{
    auto *layout = new QVBoxLayout(this);
    auto *controls = new QHBoxLayout;
    m_period = new QComboBox(this); m_period->setObjectName("usagePeriod");
    m_period->addItem("Day", "day");
    m_period->addItem("Week", "week");
    m_period->addItem("Month", "month");
    m_range = new QLineEdit(this); m_range->setObjectName("usageRange");
    auto *refreshButton = new QPushButton("Refresh", this);
    refreshButton->setObjectName("refreshUsage");
    m_provider = new QComboBox(this); m_provider->setObjectName("usageProvider");
    m_requestedModel = new QComboBox(this); m_requestedModel->setObjectName("usageModel");
    controls->addWidget(new QLabel("Period", this)); controls->addWidget(m_period);
    controls->addWidget(m_range); controls->addWidget(refreshButton);
    controls->addSpacing(12);
    controls->addWidget(new QLabel("Provider", this)); controls->addWidget(m_provider);
    controls->addWidget(new QLabel("Model", this)); controls->addWidget(m_requestedModel);
    layout->addLayout(controls);

    m_status = new QLabel(this); m_status->setObjectName("usageStatus");
    layout->addWidget(m_status);
    m_table = new QTableView(this); m_table->setObjectName("usageTable");
    m_table->setModel(m_model);
    m_table->setAlternatingRowColors(true);
    m_table->horizontalHeader()->setSectionResizeMode(QHeaderView::ResizeToContents);
    m_table->horizontalHeader()->setStretchLastSection(true);
    layout->addWidget(m_table);

    connect(m_period, qOverload<int>(&QComboBox::currentIndexChanged), this,
            [this] { updateRangePlaceholder(); });
    connect(refreshButton, &QPushButton::clicked, this, &UsagePage::refresh);
    connect(m_provider, qOverload<int>(&QComboBox::currentIndexChanged), this,
            [this] { updateFilters(); applyFilters(); });
    connect(m_requestedModel, qOverload<int>(&QComboBox::currentIndexChanged),
            this, &UsagePage::applyFilters);
    updateRangePlaceholder();
    refresh();
}

UsageModel *UsagePage::model() const { return m_model; }

void UsagePage::refresh()
{
    const UsageResult result = m_client->load(
        m_period->currentData().toString(), m_range->text());
    if (!result.succeeded) {
        m_model->setResult({});
        m_status->setText(result.error);
        return;
    }
    m_model->setResult(result);
    updateFilters();
    applyFilters();
    if (result.rows.isEmpty()) {
        QString message = QString("No usage records for %1 %2 (UTC)")
                              .arg(result.period, result.label);
        if (result.malformedLines > 0)
            message += QString("; skipped %1 malformed source line(s)")
                           .arg(result.malformedLines);
        m_status->setText(message);
    }
    else if (result.malformedLines > 0)
        m_status->setText(QString("Showing %1 %2 (UTC); skipped %3 malformed source line(s)")
                              .arg(result.period, result.label)
                              .arg(result.malformedLines));
    else
        m_status->setText(QString("Showing %1 %2 (UTC)").arg(result.period, result.label));
}

void UsagePage::updateRangePlaceholder()
{
    const QString period = m_period->currentData().toString();
    m_range->setPlaceholderText(period == "day" ? "YYYY-MM-DD (current UTC day)"
        : period == "week" ? "YYYY-Www (current ISO week)"
                           : "YYYY-MM (current UTC month)");
}

void UsagePage::updateFilters()
{
    const QString selectedProvider = m_provider->currentData().toString();
    const QString selectedModel = m_requestedModel->currentData().toString();
    m_provider->blockSignals(true);
    m_provider->clear(); m_provider->addItem("All providers", "");
    for (const QString &provider : m_model->providers()) m_provider->addItem(provider, provider);
    int providerIndex = m_provider->findData(selectedProvider);
    m_provider->setCurrentIndex(providerIndex < 0 ? 0 : providerIndex);
    m_provider->blockSignals(false);

    m_requestedModel->blockSignals(true);
    m_requestedModel->clear(); m_requestedModel->addItem("All models", "");
    for (const QString &model : m_model->models(m_provider->currentData().toString()))
        m_requestedModel->addItem(model, model);
    int modelIndex = m_requestedModel->findData(selectedModel);
    m_requestedModel->setCurrentIndex(modelIndex < 0 ? 0 : modelIndex);
    m_requestedModel->blockSignals(false);
}

void UsagePage::applyFilters()
{
    m_model->setFilters(m_provider->currentData().toString(),
                        m_requestedModel->currentData().toString());
}
