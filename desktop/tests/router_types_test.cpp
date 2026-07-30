#include <QtTest>

#include "router_types.h"

class RouterTypesTest : public QObject
{
    Q_OBJECT

private slots:
    void onlyOwnedProcessCanBeControlled();
};

void RouterTypesTest::onlyOwnedProcessCanBeControlled()
{
    QVERIFY(!canControlRouter(ProcessOwnership::None));
    QVERIFY(!canControlRouter(ProcessOwnership::External));
    QVERIFY(canControlRouter(ProcessOwnership::Owned));
}

QTEST_APPLESS_MAIN(RouterTypesTest)
#include "router_types_test.moc"
