#include "usage_model.h"

#include <QBrush>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSet>

#include <climits>

namespace {

bool integer(const QJsonObject &object, const QString &name, qint64 *value)
{
    const QJsonValue field = object.value(name);
    if (!field.isDouble()) return false;
    *value = field.toInteger();
    return double(*value) == field.toDouble() && *value >= 0;
}

bool unknownCount(const QJsonObject &object, const QString &name, int *value)
{
    qint64 parsed = 0;
    if (!integer(object, name, &parsed) || parsed > INT_MAX) return false;
    *value = int(parsed);
    return true;
}

bool tokenValue(const QJsonObject &tokens, const QJsonObject &unknown,
                const QString &name, UsageTokenValue *value)
{
    return integer(tokens, name, &value->total) &&
           unknownCount(unknown, name, &value->unknown);
}

const UsageTokenValue *tokenAt(const UsageRow &row, int column)
{
    switch (column) {
    case UsageModel::InputTokens: return &row.input;
    case UsageModel::OutputTokens: return &row.output;
    case UsageModel::CacheReadTokens: return &row.cacheRead;
    case UsageModel::CacheWriteTokens: return &row.cacheWrite;
    case UsageModel::ReasoningTokens: return &row.reasoning;
    default: return nullptr;
    }
}

} // namespace

UsageModel::UsageModel(QObject *parent) : QAbstractTableModel(parent) {}

int UsageModel::rowCount(const QModelIndex &parent) const
{
    return parent.isValid() ? 0 : m_visibleRows.size();
}

int UsageModel::columnCount(const QModelIndex &parent) const
{
    return parent.isValid() ? 0 : ColumnCount;
}

QVariant UsageModel::data(const QModelIndex &index, int role) const
{
    if (!index.isValid() || index.row() >= m_visibleRows.size()) return {};
    const UsageRow &usage = m_rows.at(m_visibleRows.at(index.row()));
    if (role == Qt::DisplayRole) {
        if (index.column() == Provider) return usage.provider;
        if (index.column() == RequestedModel) return usage.requestedModel;
        if (index.column() == UpstreamModel) return usage.upstreamModel;
        if (const UsageTokenValue *value = tokenAt(usage, index.column()))
            return displayToken(*value);
    }
    if (role == Qt::ForegroundRole) {
        const UsageTokenValue *value = tokenAt(usage, index.column());
        if (value && value->unknown > 0) return QBrush(Qt::darkYellow);
    }
    if (role == Qt::ToolTipRole) {
        const UsageTokenValue *value = tokenAt(usage, index.column());
        if (value && value->unknown > 0)
            return QString("%1 record(s) did not report this token value")
                .arg(value->unknown);
    }
    return {};
}

QVariant UsageModel::headerData(int section, Qt::Orientation orientation,
                                int role) const
{
    if (orientation != Qt::Horizontal || role != Qt::DisplayRole) return {};
    static const QStringList headers{
        "Provider", "Requested model", "Upstream model", "Input", "Output",
        "Cache read", "Cache write", "Reasoning",
    };
    return headers.value(section);
}

void UsageModel::setResult(const UsageResult &result)
{
    beginResetModel();
    m_rows = result.rows;
    m_visibleRows.clear();
    for (int index = 0; index < m_rows.size(); ++index) {
        const UsageRow &usage = m_rows.at(index);
        if ((!m_providerFilter.isEmpty() && usage.provider != m_providerFilter) ||
            (!m_modelFilter.isEmpty() && usage.requestedModel != m_modelFilter))
            continue;
        m_visibleRows.append(index);
    }
    endResetModel();
}

void UsageModel::setFilters(const QString &provider, const QString &model)
{
    m_providerFilter = provider;
    m_modelFilter = model;
    refreshVisibleRows();
}

QStringList UsageModel::providers() const
{
    QSet<QString> values;
    for (const UsageRow &usage : m_rows) values.insert(usage.provider);
    QStringList result(values.begin(), values.end());
    result.sort();
    return result;
}

QStringList UsageModel::models(const QString &provider) const
{
    QSet<QString> values;
    for (const UsageRow &usage : m_rows)
        if (provider.isEmpty() || usage.provider == provider)
            values.insert(usage.requestedModel);
    QStringList result(values.begin(), values.end());
    result.sort();
    return result;
}

const UsageRow &UsageModel::row(int index) const
{
    return m_rows.at(m_visibleRows.at(index));
}

UsageResult UsageModel::parse(const QByteArray &json)
{
    QJsonParseError error;
    const QJsonDocument document = QJsonDocument::fromJson(json, &error);
    if (error.error != QJsonParseError::NoError || !document.isObject())
        return {false, "Core returned invalid stats JSON"};
    const QJsonObject root = document.object();
    const QJsonObject range = root.value("range").toObject();
    if (range.isEmpty() || !root.value("groups").isArray())
        return {false, "Core returned incomplete stats JSON"};

    UsageResult result;
    result.period = range.value("period").toString();
    result.label = range.value("label").toString();
    qint64 malformed = 0;
    if (result.period.isEmpty() || result.label.isEmpty() ||
        !integer(root, "malformed_lines", &malformed) || malformed > INT_MAX)
        return {false, "Core returned incomplete stats JSON"};
    result.malformedLines = int(malformed);

    for (const QJsonValue &value : root.value("groups").toArray()) {
        if (!value.isObject()) return {false, "Core returned malformed stats groups"};
        const QJsonObject group = value.toObject();
        const QJsonObject tokens = group.value("tokens").toObject();
        const QJsonObject unknown = group.value("unknown_tokens").toObject();
        UsageRow row;
        row.provider = group.value("provider").toString();
        row.requestedModel = group.value("requested_model").toString();
        row.upstreamModel = group.value("upstream_model").toString();
        if (row.provider.isEmpty() || row.requestedModel.isEmpty() ||
            row.upstreamModel.isEmpty() ||
            !tokenValue(tokens, unknown, "input_tokens", &row.input) ||
            !tokenValue(tokens, unknown, "output_tokens", &row.output) ||
            !tokenValue(tokens, unknown, "cache_read_tokens", &row.cacheRead) ||
            !tokenValue(tokens, unknown, "cache_write_tokens", &row.cacheWrite) ||
            !tokenValue(tokens, unknown, "reasoning_tokens", &row.reasoning))
            return {false, "Core returned malformed stats groups"};
        result.rows.append(row);
    }
    result.succeeded = true;
    return result;
}

QString UsageModel::displayToken(const UsageTokenValue &value)
{
    if (value.unknown == 0) return QString::number(value.total);
    if (value.total == 0) return QString("Unknown (%1)").arg(value.unknown);
    return QString("%1 + unknown (%2)").arg(value.total).arg(value.unknown);
}

void UsageModel::refreshVisibleRows()
{
    UsageResult result;
    result.rows = m_rows;
    setResult(result);
}
