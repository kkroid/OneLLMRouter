#pragma once

#include <QLocale>
#include <QString>

struct Strings {
    QString stopped, starting, healthy, degraded, conflict, error;
    QString modelsPort, proxy, proxyUnknown, proxyDisabled, reachable, unreachable;
    QString start, stop, restart, externallyManaged, gracefulStopTimedOut;
    QString openConfig, openLogs, autoStart, quit;
};

inline Strings stringsForLocale(const QLocale &locale)
{
    if (locale.language() == QLocale::Chinese) {
        return {
            QString::fromUtf8("已停止"), QString::fromUtf8("正在启动"),
            QString::fromUtf8("健康"), QString::fromUtf8("降级"),
            QString::fromUtf8("端口冲突"), QString::fromUtf8("错误"),
            QString::fromUtf8("模型：%1 | 端口：%2"),
            QString::fromUtf8("代理：%1 - %2"), QString::fromUtf8("代理：未知"),
            QString::fromUtf8("代理：未配置"),
            QString::fromUtf8("可达"), QString::fromUtf8("不可达"),
            QString::fromUtf8("启动路由器"), QString::fromUtf8("停止路由器"),
            QString::fromUtf8("重启路由器"), QString::fromUtf8("由其他进程管理"),
            QString::fromUtf8("优雅停止超时"),
            QString::fromUtf8("打开配置"), QString::fromUtf8("打开日志"),
            QString::fromUtf8("登录时启动"), QString::fromUtf8("退出"),
        };
    }
    return {
        "Stopped", "Starting", "Healthy", "Degraded", "Port conflict", "Error",
        "Models: %1 | Port: %2", "Proxy: %1 - %2", "Proxy: Unknown",
        "Proxy: Not configured",
        "Reachable", "Unreachable",
        "Start Router", "Stop Router", "Restart Router",
        "Managed by another process", "Graceful stop timed out",
        "Open Configuration", "Open Logs",
        "Start on login", "Quit",
    };
}
