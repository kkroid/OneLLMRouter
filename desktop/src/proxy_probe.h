#pragma once

#include <QObject>
#include <QString>
#include <QTcpSocket>
#include <QTimer>

#include <optional>

struct ProxyEndpoint {
    QString host;
    quint16 port = 0;
};

std::optional<ProxyEndpoint> parseProxyEndpoint(const QString &address);

class ProxyProbe : public QObject
{
    Q_OBJECT

public:
    explicit ProxyProbe(QObject *parent = nullptr);
    void probe(const QString &address);

signals:
    void probeFinished(const QString &address, bool reachable);

private:
    void finish(bool reachable);

    QTcpSocket m_socket;
    QTimer m_timer;
    QString m_address;
    bool m_active = false;
};
