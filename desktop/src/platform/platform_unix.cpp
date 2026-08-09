#include "platform.h"

#include <QCoreApplication>
#include <QDir>

namespace {

const autoStartError = QStringLiteral(
    "Desktop autostart is unsupported on this platform");
const applicationRestartError = QStringLiteral(
    "Application restart integration is unsupported on this platform");

void setError(QString *error, const QString &message)
{
    if (error) {
        *error = message;
    }
}

} // namespace

namespace Platform {

QString coreExecutableName()
{
    return QStringLiteral("onellm-router-core");
}

QString coreExecutablePath()
{
    return QDir(QCoreApplication::applicationDirPath())
        .filePath(coreExecutableName());
}

bool pathsEqual(const QString &left, const QString &right)
{
    return left == right;
}

void configureChildProcess(QProcess &)
{
}

bool autoStartSupported()
{
    return false;
}

bool setAutoStart(bool, const QString &, QString *error)
{
    setError(error, autoStartError);
    return false;
}

bool autoStartEnabled(const QString &, QString *error)
{
    setError(error, autoStartError);
    return false;
}

bool migrateLegacyAutoStart(const QString &, QString *error)
{
    setError(error, autoStartError);
    return false;
}

bool applicationRestartSupported()
{
    return false;
}

bool registerApplicationRestart(const QString &, QString *error)
{
    setError(error, applicationRestartError);
    return false;
}

} // namespace Platform
