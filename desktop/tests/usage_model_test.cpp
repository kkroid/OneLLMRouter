#include <QtTest>
#include <QDir>
#include <QFile>
#include <QProcess>
#include <QProcessEnvironment>
#include <QTemporaryDir>

#include "usage/usage_model.h"

class UsageModelTest : public QObject
{
    Q_OBJECT
private slots:
    void sharedFixtureMatchesCoreJson();
    void partialAndUnknownValuesStayDistinct();
    void rejectsMalformedJsonSource();
};

void UsageModelTest::sharedFixtureMatchesCoreJson()
{
    const QString core = qEnvironmentVariable("ONELLM_TEST_CORE");
    QVERIFY2(!core.isEmpty(), "CMake must configure ONELLM_TEST_CORE");
    QTemporaryDir home;
    QVERIFY(home.isValid());
    QVERIFY(QDir().mkpath(home.filePath(".onellm/usage")));
    QFile fixture(home.filePath(".onellm/usage/fixture.jsonl"));
    QVERIFY(fixture.open(QIODevice::WriteOnly));
    fixture.write(
        "{\"time\":\"2026-08-09T12:00:00Z\",\"request_id\":\"r1\","
        "\"provider\":\"openai\",\"requested_model\":\"openai/gpt-5\","
        "\"upstream_model\":\"gpt-5\",\"protocol\":\"openai_responses\","
        "\"input_tokens\":12,\"output_tokens\":7,\"cache_read_tokens\":null,"
        "\"cache_write_tokens\":0,\"reasoning_tokens\":3,\"usage_source\":\"response\","
        "\"upstream_attempt\":1,\"status\":\"success\"}\n");
    fixture.close();

    QProcess process;
    QProcessEnvironment environment = QProcessEnvironment::systemEnvironment();
    environment.insert("USERPROFILE", QDir::toNativeSeparators(home.path()));
    environment.insert("HOME", QDir::toNativeSeparators(home.path()));
    process.setProcessEnvironment(environment);
    process.start(core, {"stats", "day", "2026-08-09", "--json"});
    QVERIFY(process.waitForFinished(10000));
    QCOMPARE(process.exitCode(), 0);
    const QByteArray cliJson = process.readAllStandardOutput();
    const UsageResult result = UsageModel::parse(cliJson);
    QVERIFY2(result.succeeded, qPrintable(result.error));
    QCOMPARE(result.rows.size(), 1);
    QCOMPARE(result.rows.first().provider, QString("openai"));
    QCOMPARE(result.rows.first().requestedModel, QString("openai/gpt-5"));
    QCOMPARE(result.rows.first().input.total, 12);
    QCOMPARE(result.rows.first().output.total, 7);
    QCOMPARE(result.rows.first().cacheRead.unknown, 1);
    QCOMPARE(result.rows.first().cacheWrite.total, 0);
    QCOMPARE(result.rows.first().reasoning.total, 3);
}

void UsageModelTest::partialAndUnknownValuesStayDistinct()
{
    const QByteArray json = R"({"range":{"period":"day","label":"2026-08-09"},"malformed_lines":0,"groups":[{"provider":"p","requested_model":"p/m","upstream_model":"m","tokens":{"input_tokens":0,"output_tokens":4,"cache_read_tokens":0,"cache_write_tokens":0,"reasoning_tokens":0},"unknown_tokens":{"input_tokens":0,"output_tokens":2,"cache_read_tokens":1,"cache_write_tokens":0,"reasoning_tokens":0}}]})";
    const UsageResult result = UsageModel::parse(json);
    QVERIFY(result.succeeded);
    UsageModel model;
    model.setResult(result);
    QCOMPARE(model.data(model.index(0, UsageModel::InputTokens)).toString(), QString("0"));
    QCOMPARE(model.data(model.index(0, UsageModel::OutputTokens)).toString(), QString("4 + unknown (2)"));
    QCOMPARE(model.data(model.index(0, UsageModel::CacheReadTokens)).toString(), QString("Unknown (1)"));
    QVERIFY(model.data(model.index(0, UsageModel::CacheReadTokens), Qt::ForegroundRole).isValid());
}

void UsageModelTest::rejectsMalformedJsonSource()
{
    QVERIFY(!UsageModel::parse("not json").succeeded);
    QVERIFY(!UsageModel::parse(R"({"range":{},"groups":[]})").succeeded);
}

QTEST_MAIN(UsageModelTest)
#include "usage_model_test.moc"
