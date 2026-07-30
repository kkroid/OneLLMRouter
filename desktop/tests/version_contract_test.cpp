#include <QtTest>

class VersionContractTest : public QObject
{
    Q_OBJECT

private slots:
    void compileDefinitionExpandsCacheValue();
};

void VersionContractTest::compileDefinitionExpandsCacheValue()
{
    const QString version = QStringLiteral(ONELLM_VERSION);
    QVERIFY(!version.isEmpty());
    QVERIFY(version != QStringLiteral("ONELLM_VERSION"));
}

QTEST_APPLESS_MAIN(VersionContractTest)
#include "version_contract_test.moc"
