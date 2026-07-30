#include <QCoreApplication>
#include <QFileInfo>
#include <QJsonDocument>
#include <QJsonObject>
#include <QTextStream>
#include <QThread>
#include <QTimer>

#include <iostream>
#include <string>
#include <thread>

int main(int argc, char *argv[])
{
    QCoreApplication application(argc, argv);
    const QStringList arguments = application.arguments();
    const int configIndex = arguments.indexOf("--config");
    const QString config = configIndex >= 0 && configIndex + 1 < arguments.size()
                               ? arguments.at(configIndex + 1)
                               : QString();

    if (arguments.contains("config-info")) {
        if (QFileInfo(config).fileName() == "hang") {
            return application.exec();
        }
        const int port = QFileInfo(config).fileName().toInt();
        const QJsonObject result{
            {"service", "onellm-router"}, {"config_path", config},
            {"host", "127.0.0.1"}, {"http_port", port},
            {"log_dir", "C:/tmp/logs"}, {"proxy_socks5", ""},
            {"bell", false}, {"onellm_catalog_path", "C:/tmp/a.json"},
            {"codex_catalog_path", "C:/tmp/b.json"},
        };
        QTextStream(stdout) << QJsonDocument(result).toJson(QJsonDocument::Compact);
        return 0;
    }

    const bool slow = QFileInfo(config).fileName() == "slow";
    std::thread([slow] {
        std::string line;
        while (std::getline(std::cin, line)) {
            if (line == "shutdown") {
                QMetaObject::invokeMethod(
                    QCoreApplication::instance(),
                    [slow] {
                        QTimer::singleShot(slow ? 200 : 0,
                                           QCoreApplication::instance(),
                                           &QCoreApplication::quit);
                    });
                return;
            }
        }
    }).detach();
    return application.exec();
}
