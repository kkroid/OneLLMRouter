#pragma once

#include <QString>

enum class ProcessOwnership {
    None,
    Owned,
    External,
};

enum class RouterState {
    Stopped,
    Starting,
    Healthy,
    Degraded,
    Conflict,
    Error,
};

struct RouterHealth {
    bool valid = false;
    QString service;
    QString status;
    QString version;
    QString configPath;
    QString proxySocks5;
    int pid = 0;
    int port = 0;
    int models = 0;
};

constexpr bool canControlRouter(ProcessOwnership ownership)
{
    return ownership == ProcessOwnership::Owned;
}
