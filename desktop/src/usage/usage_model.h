#pragma once

#include <QAbstractTableModel>
#include <QList>
#include <QStringList>

struct UsageTokenValue {
    qint64 total = 0;
    int unknown = 0;
};

struct UsageRow {
    QString provider;
    QString requestedModel;
    QString upstreamModel;
    UsageTokenValue input;
    UsageTokenValue output;
    UsageTokenValue cacheRead;
    UsageTokenValue cacheWrite;
    UsageTokenValue reasoning;
};

struct UsageResult {
    bool succeeded = false;
    QString error;
    QString period;
    QString label;
    int malformedLines = 0;
    QList<UsageRow> rows;
};

class UsageModel : public QAbstractTableModel
{
    Q_OBJECT
public:
    enum Column {
        Provider,
        RequestedModel,
        UpstreamModel,
        InputTokens,
        OutputTokens,
        CacheReadTokens,
        CacheWriteTokens,
        ReasoningTokens,
        ColumnCount,
    };

    explicit UsageModel(QObject *parent = nullptr);

    int rowCount(const QModelIndex &parent = {}) const override;
    int columnCount(const QModelIndex &parent = {}) const override;
    QVariant data(const QModelIndex &index, int role = Qt::DisplayRole) const override;
    QVariant headerData(int section, Qt::Orientation orientation,
                        int role = Qt::DisplayRole) const override;

    void setResult(const UsageResult &result);
    void setFilters(const QString &provider, const QString &model);
    QStringList providers() const;
    QStringList models(const QString &provider = {}) const;
    const UsageRow &row(int index) const;

    static UsageResult parse(const QByteArray &json);
    static QString displayToken(const UsageTokenValue &value);

private:
    void refreshVisibleRows();

    QList<UsageRow> m_rows;
    QList<int> m_visibleRows;
    QString m_providerFilter;
    QString m_modelFilter;
};
