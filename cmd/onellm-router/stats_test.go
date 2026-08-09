package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkroid/onellm-router/internal/usage"
)

func TestStatsCommandJSONAndTableUseSameAggregation(t *testing.T) {
	dir := t.TempDir()
	source := usage.SourceResponse
	input, output := 12, 7
	record := usage.Record{
		Time:            time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
		RequestID:       "request-1",
		Provider:        "openai",
		RequestedModel:  "openai/gpt-5",
		UpstreamModel:   "gpt-5",
		InputTokens:     &input,
		OutputTokens:    &output,
		UsageSource:     &source,
		UpstreamAttempt: 1,
		Status:          usage.StatusSuccess,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unexpected-name.jsonl"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, time.August, 9, 23, 0, 0, 0, time.UTC) }

	var jsonOutput bytes.Buffer
	jsonCmd := newStatsCmd(dir, now)
	jsonCmd.SetArgs([]string{"day", "--json"})
	jsonCmd.SetOut(&jsonOutput)
	if err := jsonCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"tokens"`, `"unknown_tokens"`} {
		if !bytes.Contains(jsonOutput.Bytes(), []byte(field)) {
			t.Fatalf("JSON missing %s: %s", field, jsonOutput.Bytes())
		}
	}
	for _, removed := range []string{`"all_attempts"`, `"request_outcomes"`, `"successful_request_usage"`, `"retry_attempts"`} {
		if bytes.Contains(jsonOutput.Bytes(), []byte(removed)) {
			t.Fatalf("JSON retained removed field %s: %s", removed, jsonOutput.Bytes())
		}
	}
	var result usage.StatsResult
	if err := json.Unmarshal(jsonOutput.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 1 || result.Groups[0].Tokens.Input != 12 ||
		result.Groups[0].Tokens.Output != 7 || result.Groups[0].UnknownTokens.CacheRead != 1 ||
		result.Groups[0].UnknownTokens.Reasoning != 1 {
		t.Fatalf("JSON result = %+v", result)
	}

	var tableOutput bytes.Buffer
	tableCmd := newStatsCmd(dir, now)
	tableCmd.SetArgs([]string{"day", "2026-08-09"})
	tableCmd.SetOut(&tableOutput)
	if err := tableCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	table := tableOutput.String()
	for _, value := range []string{
		"Period: day 2026-08-09 (UTC)", "openai/gpt-5", "gpt-5", "12", "7",
		"UNKNOWN INPUT", "UNKNOWN OUTPUT", "UNKNOWN CACHE READ", "UNKNOWN CACHE WRITE", "UNKNOWN REASONING",
	} {
		if !strings.Contains(table, value) {
			t.Fatalf("table missing %q:\n%s", value, table)
		}
	}
}

func TestStatsCommandEmptyDataAndMalformedInputSucceed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.jsonl"), []byte("not json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := newStatsCmd(dir, func() time.Time {
		return time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	})
	cmd.SetArgs([]string{"month", "--json"})
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got usage.StatsResult
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.MalformedLines != 1 || got.Groups == nil || len(got.Groups) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestStatsCommandRejectsInvalidRange(t *testing.T) {
	cmd := newStatsCmd(t.TempDir(), time.Now)
	cmd.SetArgs([]string{"week", "2026-W99"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid ISO week") {
		t.Fatalf("error = %v", err)
	}
}

func TestRootCommandIncludesStatsPeriods(t *testing.T) {
	root := newRootCmd()
	for _, args := range [][]string{{"stats", "day"}, {"stats", "week"}, {"stats", "month"}} {
		command, _, err := root.Find(args)
		if err != nil || command.Name() != args[1] {
			t.Fatalf("Find(%v) = %v, %v", args, command, err)
		}
	}
}
