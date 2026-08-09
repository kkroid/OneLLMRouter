#pragma once

#include "usage_model.h"

#include <QObject>
#include <QWidget>

class QComboBox;
class QLabel;
class QLineEdit;
class QTableView;

class UsageClient : public QObject
{
    Q_OBJECT
public:
    explicit UsageClient(QString executable, QObject *parent = nullptr);
    virtual UsageResult load(const QString &period, const QString &label) const;

private:
    QString m_executable;
};

class UsagePage : public QWidget
{
    Q_OBJECT
public:
    explicit UsagePage(UsageClient *client, QWidget *parent = nullptr);
    UsageModel *model() const;

public slots:
    void refresh();

private:
    void updateRangePlaceholder();
    void updateFilters();
    void applyFilters();

    UsageClient *m_client;
    UsageModel *m_model;
    QComboBox *m_period;
    QLineEdit *m_range;
    QComboBox *m_provider;
    QComboBox *m_requestedModel;
    QLabel *m_status;
    QTableView *m_table;
};
