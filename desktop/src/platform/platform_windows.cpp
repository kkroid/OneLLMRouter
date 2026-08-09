#include "platform.h"

#include <QCoreApplication>
#include <QDir>
#include <QProcess>
#include <QSettings>
#include <qt_windows.h>

namespace {

const QString autoStartValueName = QStringLiteral("OneLLMRouter Desktop");
const QString autoStartLegacyValueName = QStringLiteral("OneLLMRouter");
const QString registryPath = QStringLiteral(
    "HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Run");

QSettings autoStartSettings()
{
    return QSettings(registryPath, QSettings::NativeFormat);
}

} // namespace

namespace Platform {

QString coreExecutableName()
{
    return QStringLiteral("onellm-router-core.exe");
}

QString coreExecutablePath()
{
    return QDir(QCoreApplication::applicationDirPath())
        .filePath(coreExecutableName());
}

bool pathsEqual(const QString &left, const QString &right)
{
    return left.compare(right, Qt::CaseInsensitive) == 0;
}

void configureChildProcess(QProcess &process)
{
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *arguments) {
            arguments->flags |= CREATE_NO_WINDOW;
        });
}

bool autoStartSupported()
{
    return true;
}

bool setAutoStart(bool enabled, const QString &command, QString *)
{
    QSettings settings = autoStartSettings();
    settings.remove(autoStartLegacyValueName);
    if (enabled) {
        settings.setValue(autoStartValueName, command);
    } else {
        settings.remove(autoStartValueName);
    }
    return settings.status() == QSettings::NoError;
}

bool autoStartEnabled(const QString &command, QString *)
{
    const QSettings settings = autoStartSettings();
    return settings.value(autoStartValueName).toString() == command;
}

bool migrateLegacyAutoStart(const QString &command, QString *)
{
    QSettings settings = autoStartSettings();
    if (!settings.contains(autoStartLegacyValueName)) {
        return false;
    }
    settings.remove(autoStartLegacyValueName);
    settings.setValue(autoStartValueName, command);
    return settings.status() == QSettings::NoError;
}

bool applicationRestartSupported()
{
    return true;
}

bool registerApplicationRestart(const QString &arguments, QString *)
{
    return SUCCEEDED(::RegisterApplicationRestart(
        reinterpret_cast<PCWSTR>(arguments.utf16()),
        RESTART_NO_CRASH | RESTART_NO_HANG | RESTART_NO_REBOOT));
}

} // namespace Platform
