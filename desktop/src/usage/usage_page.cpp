#include "usage_page.h"

#include <QCalendarWidget>
#include <QComboBox>
#include <QDateTime>
#include <QHeaderView>
#include <QHBoxLayout>
#include <QLabel>
#include <QMenu>
#include <QProcess>
#include <QPushButton>
#include <QSizePolicy>
#include <QTableView>
#include <QVBoxLayout>
#include <QWidgetAction>

UsageClient::UsageClient(QString executable, QObject *parent)
    : QObject(parent), m_executable(std::move(executable))
{
}

UsageResult UsageClient::load(const QString &period, const QString &start,
                              const QString &end) const
{
    QProcess process;
    QStringList arguments{"stats", period};
    if (!start.isEmpty()) arguments.append(start);
    if (!end.isEmpty()) arguments.append(end);
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
    m_period->addItem("Today", "today");
    m_period->addItem("This month", "month");
    m_period->addItem("Custom range", "custom");
    m_customRange = new QWidget(this); m_customRange->setObjectName("usageCustomRange");
    m_customRange->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Preferred);
    auto *rangeLayout = new QHBoxLayout(m_customRange);
    rangeLayout->setContentsMargins(0, 0, 0, 0);
    const QDate today = QDateTime::currentDateTimeUtc().date();
    const QDate monthStart(today.year(), today.month(), 1);
    m_startDate = new QPushButton(monthStart.toString("yyyy-MM-dd"), m_customRange);
    m_startDate->setObjectName("usageStartDate");
    m_endDate = new QPushButton(today.toString("yyyy-MM-dd"), m_customRange);
    m_endDate->setObjectName("usageEndDate");
    m_startCalendar = new QCalendarWidget(m_startDate);
    m_startCalendar->setObjectName("usageStartCalendar");
    m_startCalendar->setMaximumDate(today);
    m_startCalendar->setSelectedDate(monthStart);
    m_endCalendar = new QCalendarWidget(m_endDate);
    m_endCalendar->setObjectName("usageEndCalendar");
    m_endCalendar->setDateRange(monthStart, today);
    m_endCalendar->setSelectedDate(today);
    const auto attachCalendar = [](QPushButton *button, QCalendarWidget *calendar) {
        auto *menu = new QMenu(button);
        auto *action = new QWidgetAction(menu);
        action->setDefaultWidget(calendar);
        menu->addAction(action);
        button->setMenu(menu);
        button->setMinimumWidth(105);
    };
    attachCalendar(m_startDate, m_startCalendar);
    attachCalendar(m_endDate, m_endCalendar);
    rangeLayout->addWidget(new QLabel("From", m_customRange));
    rangeLayout->addWidget(m_startDate);
    rangeLayout->addWidget(new QLabel("To", m_customRange));
    rangeLayout->addWidget(m_endDate);
    m_customRange->setVisible(false);
    m_provider = new QComboBox(this); m_provider->setObjectName("usageProvider");
    m_requestedModel = new QComboBox(this); m_requestedModel->setObjectName("usageModel");
    controls->addWidget(new QLabel("Period", this)); controls->addWidget(m_period);
    controls->addWidget(m_customRange);
    controls->addSpacing(12);
    controls->addWidget(new QLabel("Provider", this)); controls->addWidget(m_provider);
    controls->addWidget(new QLabel("Model", this)); controls->addWidget(m_requestedModel);
    controls->addStretch();
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
            [this] {
                updateDateControl();
                if (m_period->currentData().toString() != "custom") refresh();
            });
    connect(m_startCalendar, &QCalendarWidget::clicked, this,
            [this](const QDate &date) {
                m_startCalendar->setSelectedDate(date);
                m_startDate->setText(date.toString("yyyy-MM-dd"));
                m_startDate->menu()->hide();
                m_endCalendar->setMinimumDate(date);
                if (m_endCalendar->selectedDate() < date) {
                    m_endCalendar->setSelectedDate(date);
                    m_endDate->setText(date.toString("yyyy-MM-dd"));
                }
            });
    connect(m_endCalendar, &QCalendarWidget::clicked, this,
            [this](const QDate &date) {
                m_endCalendar->setSelectedDate(date);
                m_endDate->setText(date.toString("yyyy-MM-dd"));
                m_endDate->menu()->hide();
                if (m_period->currentData().toString() == "custom") refresh();
            });
    connect(m_provider, qOverload<int>(&QComboBox::currentIndexChanged), this,
            [this] { updateFilters(); applyFilters(); });
    connect(m_requestedModel, qOverload<int>(&QComboBox::currentIndexChanged),
            this, &UsagePage::applyFilters);
    updateDateControl();
    refresh();
}

UsageModel *UsagePage::model() const { return m_model; }

void UsagePage::refresh()
{
    const QString mode = m_period->currentData().toString();
    const QString period = mode == "month" ? "month"
        : mode == "custom" ? "range" : "day";
    const QString start = mode == "custom"
        ? m_startCalendar->selectedDate().toString("yyyy-MM-dd") : QString();
    const QString end = mode == "custom"
        ? m_endCalendar->selectedDate().toString("yyyy-MM-dd") : QString();
    const UsageResult result = m_client->load(period, start, end);
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

void UsagePage::updateDateControl()
{
    m_customRange->setVisible(m_period->currentData().toString() == "custom");
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
