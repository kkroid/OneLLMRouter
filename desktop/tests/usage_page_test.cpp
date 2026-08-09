#include <QtTest>
#include <QComboBox>
#include <QLabel>
#include <QLineEdit>
#include <QPushButton>
#include <QTableView>

#include "usage/usage_page.h"

class FakeUsageClient : public UsageClient
{
public:
    FakeUsageClient() : UsageClient("unused") {}
    UsageResult next;
    mutable QString period;
    mutable QString label;
    UsageResult load(const QString &selectedPeriod,
                     const QString &selectedLabel) const override
    {
        period = selectedPeriod;
        label = selectedLabel;
        return next;
    }
};

class UsagePageTest : public QObject
{
    Q_OBJECT
private slots:
    void periodControlsAndFiltersUseCoreRows();
    void emptyAndMalformedSourceStatesKeepTableStable();
};

void UsagePageTest::periodControlsAndFiltersUseCoreRows()
{
    FakeUsageClient client;
    client.next = {true, {}, "day", "2026-08-09", 0,
                   {{"alpha", "alpha/a", "a", {1, 0}, {2, 0}, {}, {}, {}},
                    {"beta", "beta/b", "b", {3, 0}, {4, 0}, {}, {}, {}}}};
    UsagePage page(&client);
    page.show();
    auto *period = page.findChild<QComboBox *>("usagePeriod");
    auto *range = page.findChild<QLineEdit *>("usageRange");
    auto *provider = page.findChild<QComboBox *>("usageProvider");
    auto *model = page.findChild<QComboBox *>("usageModel");
    QVERIFY(period && range && provider && model);
    QCOMPARE(page.model()->rowCount(), 2);
    provider->setCurrentIndex(provider->findData("beta"));
    QCOMPARE(page.model()->rowCount(), 1);
    QCOMPARE(page.model()->row(0).requestedModel, QString("beta/b"));
    QCOMPARE(model->findData("alpha/a"), -1);
    period->setCurrentIndex(period->findData("week"));
    range->setText("2026-W32");
    QTest::mouseClick(page.findChild<QPushButton *>("refreshUsage"), Qt::LeftButton);
    QCOMPARE(client.period, QString("week"));
    QCOMPARE(client.label, QString("2026-W32"));
}

void UsagePageTest::emptyAndMalformedSourceStatesKeepTableStable()
{
    FakeUsageClient client;
    client.next = {true, {}, "month", "2026-08", 2, {}};
    UsagePage page(&client);
    page.resize(900, 500);
    page.show();
    const QSize size = page.size();
    auto *table = page.findChild<QTableView *>("usageTable");
    auto *status = page.findChild<QLabel *>("usageStatus");
    QVERIFY(table && status);
    QCOMPARE(table->model()->columnCount(), int(UsageModel::ColumnCount));
    QCOMPARE(table->model()->rowCount(), 0);
    QVERIFY(status->text().contains("No usage records"));
    QVERIFY(status->text().contains("skipped 2 malformed source line(s)"));

    client.next = {false, "Core returned invalid stats JSON"};
    page.refresh();
    QCOMPARE(page.size(), size);
    QCOMPARE(table->model()->columnCount(), int(UsageModel::ColumnCount));
    QCOMPARE(table->model()->rowCount(), 0);
    QCOMPARE(status->text(), QString("Core returned invalid stats JSON"));
}

QTEST_MAIN(UsagePageTest)
#include "usage_page_test.moc"
