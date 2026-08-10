#pragma once

#include "../config/config_client.h"

#include <QMap>
#include <QWidget>

class QComboBox;
class QLabel;
class QPlainTextEdit;
class QPushButton;

class ClientsPage : public QWidget
{
    Q_OBJECT
public:
    explicit ClientsPage(ConfigClient *client, QWidget *parent = nullptr);
    void setConfiguration(const ConfigSnapshot &snapshot);
    void setReadOnly(bool readOnly);

signals:
    void modelSlotChanged(const QString &slot, const QString &model);

private:
    void refreshClaude();
    void applyClaude();
    void restoreClaude();
    void refreshCodex();
    void previewCodex();
    void applyCodexCatalog();
    void showClaude(const ClientCommandResult &result);
    void showCodex(const ClientCommandResult &result);
    static QString display(const QString &value);

    ConfigClient *m_client;
    QMap<QString, QComboBox *> m_slots;
    QComboBox *m_codexModel;
    QLabel *m_claudePath;
    QLabel *m_claudeState;
    QLabel *m_claudeKeys;
    QLabel *m_claudeError;
    QLabel *m_codexConfigPath;
    QLabel *m_codexCatalogPaths;
    QLabel *m_codexState;
    QLabel *m_codexEffective;
    QLabel *m_codexSource;
    QLabel *m_codexError;
    QPlainTextEdit *m_snippet;
    QPushButton *m_claudeApply;
    QPushButton *m_claudeRestore;
    QPushButton *m_codexPreview;
    QPushButton *m_codexApply;
};
