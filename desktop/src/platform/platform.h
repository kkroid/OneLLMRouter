#pragma once

#include <QString>

class QProcess;

namespace Platform {

QString coreExecutableName();
QString coreExecutablePath();
bool pathsEqual(const QString &left, const QString &right);
void configureChildProcess(QProcess &process);

bool autoStartSupported();
bool setAutoStart(bool enabled, const QString &command, QString *error = nullptr);
bool autoStartEnabled(const QString &command, QString *error = nullptr);
bool migrateLegacyAutoStart(const QString &command, QString *error = nullptr);

bool applicationRestartSupported();
bool registerApplicationRestart(const QString &arguments,
                                QString *error = nullptr);

} // namespace Platform
