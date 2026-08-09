#include <QtTest>

#include "platform/platform.h"

class PlatformTest : public QObject
{
    Q_OBJECT

private slots:
    void usesPlatformCoreExecutableName();
    void reportsPlatformCapabilities();
    void comparesPathsWithPlatformSemantics();
};

void PlatformTest::usesPlatformCoreExecutableName()
{
#ifdef Q_OS_WIN
    QCOMPARE(Platform::coreExecutableName(),
             QStringLiteral("onellm-router-core.exe"));
#else
    QCOMPARE(Platform::coreExecutableName(),
             QStringLiteral("onellm-router-core"));
#endif
    QVERIFY(Platform::coreExecutablePath().endsWith(
        Platform::coreExecutableName()));
}

void PlatformTest::reportsPlatformCapabilities()
{
#ifdef Q_OS_WIN
    QVERIFY(Platform::autoStartSupported());
    QVERIFY(Platform::applicationRestartSupported());
#else
    QVERIFY(!Platform::autoStartSupported());
    QVERIFY(!Platform::applicationRestartSupported());

    QString error;
    QVERIFY(!Platform::setAutoStart(true, QStringLiteral("command"), &error));
    QVERIFY(error.contains(QStringLiteral("unsupported")));
    error.clear();
    QVERIFY(!Platform::registerApplicationRestart({}, &error));
    QVERIFY(error.contains(QStringLiteral("unsupported")));
#endif
}

void PlatformTest::comparesPathsWithPlatformSemantics()
{
    QVERIFY(Platform::pathsEqual(QStringLiteral("same/path"),
                                 QStringLiteral("same/path")));
#ifdef Q_OS_WIN
    QVERIFY(Platform::pathsEqual(QStringLiteral("C:/TMP/router.yaml"),
                                 QStringLiteral("c:/tmp/router.yaml")));
#else
    QVERIFY(!Platform::pathsEqual(QStringLiteral("/TMP/router.yaml"),
                                  QStringLiteral("/tmp/router.yaml")));
#endif
}

QTEST_APPLESS_MAIN(PlatformTest)
#include "platform_test.moc"
