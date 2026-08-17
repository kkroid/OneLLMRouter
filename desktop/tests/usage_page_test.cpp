#include <QtTest>
#include <QCalendarWidget>
#include <QComboBox>
#include <QLabel>
#include <QMenu>
#include <QPushButton>
#include <QTableView>

#include "usage/usage_page.h"

class FakeUsageClient : public UsageClient
{
public:
    FakeUsageClient() : UsageClient("unused") {}
    UsageResult next;
    mutable QString period;
    mutable QString start;
    mutable QString end;
    mutable int loads = 0;
    UsageResult load(const QString &selectedPeriod,
                     const QString &selectedStart,
                     const QString &selectedEnd) const override
    {
        period = selectedPeriod;
        start = selectedStart;
        end = selectedEnd;
        ++loads;
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
    page.resize(2048, 320);
    page.show();
    auto *period = page.findChild<QComboBox *>("usagePeriod");
    auto *range = page.findChild<QWidget *>("usageCustomRange");
    auto *start = page.findChild<QPushButton *>("usageStartDate");
    auto *end = page.findChild<QPushButton *>("usageEndDate");
    auto *startCalendar = page.findChild<QCalendarWidget *>("usageStartCalendar");
    auto *endCalendar = page.findChild<QCalendarWidget *>("usageEndCalendar");
    auto *provider = page.findChild<QComboBox *>("usageProvider");
    auto *model = page.findChild<QComboBox *>("usageModel");
    QLabel *providerLabel = nullptr;
    QLabel *modelLabel = nullptr;
    for (QLabel *label : page.findChildren<QLabel *>()) {
        if (label->text() == "Provider") providerLabel = label;
        if (label->text() == "Model") modelLabel = label;
    }
    QVERIFY(period && range && start && end && startCalendar && endCalendar && provider && model);
    QVERIFY(providerLabel && modelLabel);
    QCOMPARE(period->count(), 3);
    QCOMPARE(period->findData("week"), -1);
    QVERIFY(!range->isVisible());
    QVERIFY(!page.findChild<QWidget *>("refreshUsage"));
    QCOMPARE(client.period, QString("day"));
    QCOMPARE(client.loads, 1);
    QCOMPARE(page.model()->rowCount(), 2);
    provider->setCurrentIndex(provider->findData("beta"));
    QCOMPARE(page.model()->rowCount(), 1);
    QCOMPARE(page.model()->row(0).requestedModel, QString("beta/b"));
    QCOMPARE(model->findData("alpha/a"), -1);

    period->setCurrentIndex(period->findData("month"));
    QCOMPARE(client.period, QString("month"));
    QCOMPARE(client.start, QString());
    QCOMPARE(client.end, QString());
    QCOMPARE(client.loads, 2);
    QVERIFY(!range->isVisible());

    period->setCurrentIndex(period->findData("custom"));
    QCOMPARE(client.loads, 2);
    QVERIFY(range->isVisible());
    QApplication::processEvents();
    QVERIFY(provider->x() - providerLabel->geometry().right() < 24);
    QVERIFY(model->x() - modelLabel->geometry().right() < 24);
    QTest::mouseClick(start, Qt::LeftButton);
    QTRY_VERIFY(start->menu()->isVisible());
    const QDate startDate(2026, 8, 1);
    QVERIFY(QMetaObject::invokeMethod(startCalendar, "clicked",
                                      Qt::DirectConnection, Q_ARG(QDate, startDate)));
    QVERIFY(!start->menu()->isVisible());
    QCOMPARE(client.loads, 2);
    const QDate endDate(2026, 8, 9);
    QTest::mouseClick(end, Qt::LeftButton);
    QTRY_VERIFY(end->menu()->isVisible());
    QVERIFY(QMetaObject::invokeMethod(endCalendar, "clicked",
                                      Qt::DirectConnection, Q_ARG(QDate, endDate)));
    QVERIFY(!end->menu()->isVisible());
    QCOMPARE(client.period, QString("range"));
    QCOMPARE(client.start, QString("2026-08-01"));
    QCOMPARE(client.end, QString("2026-08-09"));
    QCOMPARE(client.loads, 3);

    const QDate secondEndDate(2026, 8, 10);
    QTest::mouseClick(end, Qt::LeftButton);
    QTRY_VERIFY(end->menu()->isVisible());
    QVERIFY(QMetaObject::invokeMethod(endCalendar, "clicked",
                                      Qt::DirectConnection, Q_ARG(QDate, secondEndDate)));
    QCOMPARE(client.end, QString("2026-08-10"));
    QCOMPARE(client.loads, 4);
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
