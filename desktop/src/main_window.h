#pragma once

#include "config/config_client.h"
#include "clients/clients_page.h"
#include "usage/usage_page.h"

#include <QMainWindow>

class QComboBox;
class QLabel;
class QLineEdit;
class QListWidget;
class QPushButton;
class QTabWidget;

class MainWindow : public QMainWindow
{
    Q_OBJECT
public:
    explicit MainWindow(ConfigClient *client, bool readOnly = false,
                        QWidget *parent = nullptr,
                        UsageClient *usageClient = nullptr);
    void setReadOnly(bool readOnly);

private slots:
    void selectProvider(int row);
    void saveProvider();
    void addProvider();
    void removeProvider();
    void addModel();
    void removeModel();
    void discoverModels();
    void save();

private:
    void buildUi();
    void load();
    void refreshProviders();
    void refreshModels();
    void refreshReasoning();
    void showResult(const ConfigResult &result);
    QMap<int, QString> pendingKeys() const;

    ConfigClient *m_client;
    UsageClient *m_usageClient;
    ConfigSnapshot m_snapshot;
    bool m_readOnly = false;
    QTabWidget *m_tabs;
    QListWidget *m_providerList;
    QListWidget *m_modelList;
    QLineEdit *m_name;
    QLineEdit *m_prefix;
    QLineEdit *m_baseUrl;
    QLineEdit *m_openAIBaseUrl;
    QLineEdit *m_responsesBaseUrl;
    QLineEdit *m_apiKey;
    QComboBox *m_proxy;
    QComboBox *m_protocol;
    QLineEdit *m_modelName;
    QLineEdit *m_defaultReasoning;
    QLineEdit *m_supportedReasoning;
    ClientsPage *m_clientsPage;
    QLabel *m_status;
    QPushButton *m_save;
    QList<QWidget *> m_editControls;
    QMap<int, QString> m_apiKeys;
};
