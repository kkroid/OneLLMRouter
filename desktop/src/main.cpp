#include <QApplication>
#include <QCommandLineParser>
#include <QDir>
#include <QFileInfo>
#include <QTimer>

#include "smoke_mode.h"
#include "tray_application.h"

int main(int argc, char *argv[])
{
    QApplication application(argc, argv);
    QApplication::setQuitOnLastWindowClosed(false);
    QCoreApplication::setApplicationName("OneLLMRouter Desktop");
    QCoreApplication::setApplicationVersion(ONELLM_VERSION);

    QCommandLineParser parser;
    parser.addHelpOption();
    parser.addVersionOption();
    QCommandLineOption configOption("config", "Router configuration path.",
                                    "path",
                                    QDir::home().filePath(
                                        ".onellm/onellm-router.yaml"));
    QCommandLineOption smokeOption("smoke-test", "Write smoke result JSON.",
                                   "result-json");
    smokeOption.setFlags(QCommandLineOption::HiddenFromHelp);
    parser.addOption(configOption);
    parser.addOption(smokeOption);
    parser.process(application);

    const QString configPath =
        QFileInfo(parser.value(configOption)).absoluteFilePath();
    if (parser.isSet(smokeOption)) {
        const QString resultPath =
            QFileInfo(parser.value(smokeOption)).absoluteFilePath();
        SmokeRunner smoke(configPath, resultPath);
        QTimer::singleShot(0, &smoke, &SmokeRunner::start);
        return application.exec();
    }

    TrayApplication tray(configPath);
    return application.exec();
}
