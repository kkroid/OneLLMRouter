#include "proxy_probe.h"

#include <QHostAddress>

std::optional<ProxyEndpoint> parseProxyEndpoint(const QString &address)
{
    const QString input = address.trimmed();
    QString host;
    QString portText;

    if (input.startsWith('[')) {
        const qsizetype closingBracket = input.indexOf(']');
        if (closingBracket <= 1 || closingBracket + 1 >= input.size() ||
            input.at(closingBracket + 1) != ':') {
            return std::nullopt;
        }
        host = input.mid(1, closingBracket - 1);
        portText = input.mid(closingBracket + 2);
    } else {
        const qsizetype colon = input.indexOf(':');
        if (colon <= 0 || colon != input.lastIndexOf(':')) {
            return std::nullopt;
        }
        host = input.left(colon);
        portText = input.mid(colon + 1);
    }

    bool portOk = false;
    const uint port = portText.toUInt(&portOk);
    if (!portOk || port == 0 || port > 65535) {
        return std::nullopt;
    }

    bool loopback = host.compare("localhost", Qt::CaseInsensitive) == 0;
    if (!loopback) {
        QHostAddress ip;
        loopback = ip.setAddress(host) && ip.isLoopback();
    }
    if (!loopback) {
        return std::nullopt;
    }

    return ProxyEndpoint{host, quint16(port)};
}

ProxyProbe::ProxyProbe(QObject *parent)
    : QObject(parent)
{
    m_timer.setSingleShot(true);
    m_timer.setInterval(1000);
    connect(&m_socket, &QTcpSocket::connected, this,
            [this] { finish(true); });
    connect(&m_socket, &QTcpSocket::errorOccurred, this,
            [this](QAbstractSocket::SocketError) { finish(false); });
    connect(&m_timer, &QTimer::timeout, this, [this] {
        if (!m_active) {
            return;
        }
        m_active = false;
        m_socket.abort();
        emit probeFinished(m_address, false);
    });
}

void ProxyProbe::probe(const QString &address)
{
    if (m_active) {
        m_active = false;
        m_timer.stop();
        m_socket.abort();
    }

    m_address = address;
    const auto endpoint = parseProxyEndpoint(address);
    if (!endpoint) {
        emit probeFinished(address, false);
        return;
    }

    m_active = true;
    m_timer.start();
    m_socket.connectToHost(endpoint->host, endpoint->port);
}

void ProxyProbe::finish(bool reachable)
{
    if (!m_active) {
        return;
    }
    m_active = false;
    m_timer.stop();
    if (reachable) {
        m_socket.disconnectFromHost();
    }
    emit probeFinished(m_address, reachable);
}
