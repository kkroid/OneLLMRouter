package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseRangeUsesUTCAndISOBoundaries(t *testing.T) {
	now := time.Date(2024, time.December, 31, 20, 0, 0, 0, time.FixedZone("west", -5*60*60))

	day, err := ParseRange(PeriodDay, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if day.Label != "2025-01-01" || !day.Start.Equal(time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day = %+v", day)
	}

	week, err := ParseRange(PeriodWeek, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if week.Label != "2025-W01" || !week.Start.Equal(time.Date(2024, time.December, 30, 0, 0, 0, 0, time.UTC)) ||
		!week.End.Equal(time.Date(2025, time.January, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("week = %+v", week)
	}

	month, err := ParseRange(PeriodMonth, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if month.Label != "2025-01" || !month.End.Equal(time.Date(2025, time.February, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("month = %+v", month)
	}
}

func TestParseRangeValidatesISOWeekYear(t *testing.T) {
	week, err := ParseRange(PeriodWeek, "2020-W53", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !week.Start.Equal(time.Date(2020, time.December, 28, 0, 0, 0, 0, time.UTC)) ||
		!week.End.Equal(time.Date(2021, time.January, 4, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("week = %+v", week)
	}
	for _, value := range []string{"2021-W53", "2025-W1", "2025-01"} {
		if _, err := ParseRange(PeriodWeek, value, time.Time{}); err == nil {
			t.Fatalf("ParseRange(%q) error = nil", value)
		}
	}
}

func TestAggregateDirGroupsAttemptsAndSuccessfulRequests(t *testing.T) {
	dir := t.TempDir()
	selected, err := ParseRange(PeriodDay, "2026-08-09", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	response := SourceResponse
	zero := 0
	records := []Record{
		statsRecord("request-1", 1, StatusError, "provider-a", "provider-a/model", "model", time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC), &response, intPointer(2), nil),
		statsRecord("request-1", 2, StatusError, "provider-a", "provider-a/model", "model", time.Date(2026, 8, 9, 1, 1, 0, 0, time.UTC), nil, nil, nil),
		statsRecord("request-1", 3, StatusSuccess, "provider-a", "provider-a/model", "model", time.Date(2026, 8, 9, 1, 2, 0, 0, time.UTC), &response, intPointer(3), intPointer(4)),
		statsRecord("request-2", 1, StatusCancelled, "provider-b", "provider-b/model", "model", time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC), nil, nil, nil),
		statsRecord("request-3", 1, Status("unexpected"), "provider-a", "provider-a/model", "model", time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC), &response, &zero, &zero),
		statsRecord("before", 1, StatusSuccess, "provider-a", "provider-a/model", "model", time.Date(2026, 8, 8, 23, 59, 59, 0, time.UTC), &response, intPointer(100), nil),
		statsRecord("outside", 1, StatusSuccess, "provider-a", "provider-a/model", "model", time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), &response, intPointer(100), nil),
	}
	records[2].CacheReadTokens = &zero
	writeUsageLines(t, filepath.Join(dir, "a.jsonl"), records, true)
	duplicate := records[0]
	duplicate.InputTokens = intPointer(999)
	writeUsageLines(t, filepath.Join(dir, "b.jsonl"), []Record{duplicate}, false)

	got, err := AggregateDir(dir, selected)
	if err != nil {
		t.Fatal(err)
	}
	if got.MalformedLines != 1 || len(got.Groups) != 2 {
		t.Fatalf("result = %+v", got)
	}
	first := got.Groups[0]
	if first.Provider != "provider-a" || first.AllAttempts.Attempts != 4 || first.AllAttempts.RetryAttempts != 2 ||
		first.AllAttempts.RetriedRequests != 1 || first.AllAttempts.UnknownRecords != 1 ||
		first.AllAttempts.Tokens.Input != 5 || first.AllAttempts.Tokens.Output != 4 || first.AllAttempts.Tokens.CacheRead != 0 {
		t.Fatalf("provider-a attempts = %+v", first.AllAttempts)
	}
	if first.AllAttempts.UnknownTokens != (TokenUnknownCounts{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Reasoning: 4}) {
		t.Fatalf("provider-a unknown tokens = %+v", first.AllAttempts.UnknownTokens)
	}
	if first.RequestOutcomes != (OutcomeCounts{Total: 2, Success: 1, Unknown: 1}) {
		t.Fatalf("provider-a outcomes = %+v", first.RequestOutcomes)
	}
	if first.SuccessfulRequestUsage.Requests != 1 || first.SuccessfulRequestUsage.Tokens.Input != 3 ||
		first.SuccessfulRequestUsage.Tokens.Output != 4 || first.SuccessfulRequestUsage.UnknownRecords != 0 ||
		first.SuccessfulRequestUsage.UnknownTokens != (TokenUnknownCounts{CacheWrite: 1, Reasoning: 1}) {
		t.Fatalf("provider-a successful usage = %+v", first.SuccessfulRequestUsage)
	}
	second := got.Groups[1]
	if second.Provider != "provider-b" || second.AllAttempts.UnknownRecords != 1 ||
		second.AllAttempts.UnknownTokens != (TokenUnknownCounts{Input: 1, Output: 1, CacheRead: 1, CacheWrite: 1, Reasoning: 1}) ||
		second.RequestOutcomes.Total != 1 || second.RequestOutcomes.Cancelled != 1 || second.SuccessfulRequestUsage.Requests != 0 {
		t.Fatalf("provider-b group = %+v", second)
	}

	again, err := AggregateDir(dir, selected)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("second aggregation changed result:\nfirst:  %+v\nsecond: %+v", got, again)
	}
}

func TestAggregateDirHonorsISOWeekYearRecordBoundaries(t *testing.T) {
	dir := t.TempDir()
	selected, err := ParseRange(PeriodWeek, "2025-W01", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	source := SourceResponse
	records := []Record{
		statsRecord("before", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2024, 12, 29, 23, 59, 59, 0, time.UTC), &source, intPointer(100), nil),
		statsRecord("start", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2024, 12, 30, 0, 0, 0, 0, time.UTC), &source, intPointer(1), nil),
		statsRecord("last", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2025, 1, 5, 23, 59, 59, 0, time.UTC), &source, intPointer(2), nil),
		statsRecord("end", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), &source, intPointer(100), nil),
	}
	writeUsageLines(t, filepath.Join(dir, "records.jsonl"), records, false)

	got, err := AggregateDir(dir, selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || got.Groups[0].AllAttempts.Attempts != 2 ||
		got.Groups[0].AllAttempts.Tokens.Input != 3 || got.Groups[0].RequestOutcomes.Total != 2 {
		t.Fatalf("result = %+v", got)
	}
}

func TestAggregateDirHonorsMonthRecordBoundaries(t *testing.T) {
	dir := t.TempDir()
	selected, err := ParseRange(PeriodMonth, "2024-02", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	source := SourceResponse
	records := []Record{
		statsRecord("before", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC), &source, intPointer(100), nil),
		statsRecord("start", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), &source, intPointer(1), nil),
		statsRecord("last", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC), &source, intPointer(2), nil),
		statsRecord("end", 1, StatusSuccess, "provider", "provider/model", "model", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), &source, intPointer(100), nil),
	}
	writeUsageLines(t, filepath.Join(dir, "records.jsonl"), records, false)

	got, err := AggregateDir(dir, selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || got.Groups[0].AllAttempts.Attempts != 2 ||
		got.Groups[0].AllAttempts.Tokens.Input != 3 || got.Groups[0].RequestOutcomes.Total != 2 {
		t.Fatalf("result = %+v", got)
	}
}

func TestAggregateDirMissingDirectoryReturnsEmptyResult(t *testing.T) {
	selected, err := ParseRange(PeriodMonth, "2026-08", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := AggregateDir(filepath.Join(t.TempDir(), "missing"), selected)
	if err != nil {
		t.Fatal(err)
	}
	if got.Groups == nil || len(got.Groups) != 0 || got.MalformedLines != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func statsRecord(requestID string, attempt int, status Status, provider, requestedModel, upstreamModel string, at time.Time, source *Source, input, output *int) Record {
	return Record{
		Time:            at,
		RequestID:       requestID,
		Provider:        provider,
		RequestedModel:  requestedModel,
		UpstreamModel:   upstreamModel,
		InputTokens:     input,
		OutputTokens:    output,
		UsageSource:     source,
		UpstreamAttempt: attempt,
		Status:          status,
	}
}

func intPointer(value int) *int {
	return &value
}

func writeUsageLines(t *testing.T, path string, records []Record, malformed bool) {
	t.Helper()
	var output strings.Builder
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(data)
		output.WriteByte('\n')
	}
	if malformed {
		output.WriteString("not json\n")
	}
	if err := os.WriteFile(path, []byte(output.String()), 0600); err != nil {
		t.Fatal(err)
	}
}
