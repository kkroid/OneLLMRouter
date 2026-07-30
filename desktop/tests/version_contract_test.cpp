#include <QtTest>

class VersionContractTest : public QObject
{
    Q_OBJECT

private slots:
    void compileDefinitionExpandsCacheValue();
};

void VersionContractTest::compileDefinitionExpandsCacheValue()
{
    QCOMPARE(QStringLiteral(ONELLM_VERSION), QStringLiteral("1.4.0"));
}

QTEST_APPLESS_MAIN(VersionContractTest)
#include "version_contract_test.moc"
