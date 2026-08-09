package usage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

type Period string

const (
	PeriodDay   Period = "day"
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
)

type Range struct {
	Period Period    `json:"period"`
	Label  string    `json:"label"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
}

type TokenTotals struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_tokens"`
	CacheWrite int64 `json:"cache_write_tokens"`
	Reasoning  int64 `json:"reasoning_tokens"`
}

type TokenUnknownCounts struct {
	Input      int `json:"input_tokens"`
	Output     int `json:"output_tokens"`
	CacheRead  int `json:"cache_read_tokens"`
	CacheWrite int `json:"cache_write_tokens"`
	Reasoning  int `json:"reasoning_tokens"`
}

type AttemptStats struct {
	Attempts        int                `json:"attempts"`
	RetryAttempts   int                `json:"retry_attempts"`
	RetriedRequests int                `json:"retried_requests"`
	UnknownRecords  int                `json:"unknown_records"`
	Tokens          TokenTotals        `json:"tokens"`
	UnknownTokens   TokenUnknownCounts `json:"unknown_tokens"`
}

type OutcomeCounts struct {
	Total     int `json:"total"`
	Success   int `json:"success"`
	Error     int `json:"error"`
	Cancelled int `json:"cancelled"`
	Unknown   int `json:"unknown"`
}

type SuccessfulRequestUsage struct {
	Requests       int                `json:"requests"`
	UnknownRecords int                `json:"unknown_records"`
	Tokens         TokenTotals        `json:"tokens"`
	UnknownTokens  TokenUnknownCounts `json:"unknown_tokens"`
}

type StatsGroup struct {
	Provider               string                 `json:"provider"`
	RequestedModel         string                 `json:"requested_model"`
	UpstreamModel          string                 `json:"upstream_model"`
	AllAttempts            AttemptStats           `json:"all_attempts"`
	RequestOutcomes        OutcomeCounts          `json:"request_outcomes"`
	SuccessfulRequestUsage SuccessfulRequestUsage `json:"successful_request_usage"`
}

type StatsResult struct {
	Range          Range        `json:"range"`
	MalformedLines int          `json:"malformed_lines"`
	Groups         []StatsGroup `json:"groups"`
}

var weekPattern = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

func ParseRange(period Period, value string, now time.Time) (Range, error) {
	now = now.UTC()
	switch period {
	case PeriodDay:
		if value == "" {
			value = now.Format("2006-01-02")
		}
		start, err := time.Parse("2006-01-02", value)
		if err != nil {
			return Range{}, fmt.Errorf("invalid day %q: expected YYYY-MM-DD", value)
		}
		return Range{Period: period, Label: value, Start: start, End: start.AddDate(0, 0, 1)}, nil
	case PeriodWeek:
		if value == "" {
			year, week := now.ISOWeek()
			value = fmt.Sprintf("%04d-W%02d", year, week)
		}
		matches := weekPattern.FindStringSubmatch(value)
		if matches == nil {
			return Range{}, fmt.Errorf("invalid week %q: expected YYYY-Www", value)
		}
		year, _ := strconv.Atoi(matches[1])
		week, _ := strconv.Atoi(matches[2])
		jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
		start := jan4.AddDate(0, 0, -int(jan4.Weekday()+6)%7+(week-1)*7)
		actualYear, actualWeek := start.ISOWeek()
		if actualYear != year || actualWeek != week {
			return Range{}, fmt.Errorf("invalid ISO week %q", value)
		}
		return Range{Period: period, Label: value, Start: start, End: start.AddDate(0, 0, 7)}, nil
	case PeriodMonth:
		if value == "" {
			value = now.Format("2006-01")
		}
		start, err := time.Parse("2006-01", value)
		if err != nil {
			return Range{}, fmt.Errorf("invalid month %q: expected YYYY-MM", value)
		}
		return Range{Period: period, Label: value, Start: start, End: start.AddDate(0, 1, 0)}, nil
	default:
		return Range{}, fmt.Errorf("unsupported stats period %q", period)
	}
}

func AggregateDir(baseDir string, selected Range) (StatsResult, error) {
	if baseDir == "" {
		baseDir = defaultBaseDir()
	}
	result := StatsResult{Range: selected, Groups: []StatsGroup{}}
	entries, err := os.ReadDir(baseDir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return StatsResult{}, fmt.Errorf("read usage directory: %w", err)
	}

	groups := make(map[groupKey]*StatsGroup)
	retriedRequests := make(map[groupKey]map[string]struct{})
	records := make([]Record, 0)
	seen := make(map[attemptKey]struct{})
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		readResult, readErr := ReadFile(filepath.Join(baseDir, entry.Name()))
		if readErr != nil {
			return StatsResult{}, fmt.Errorf("read usage file %s: %w", entry.Name(), readErr)
		}
		result.MalformedLines += len(readResult.Malformed)
		for _, record := range readResult.Records {
			key := attemptKey{requestID: record.RequestID, attempt: record.UpstreamAttempt}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			if record.Time.Before(selected.Start) || !record.Time.Before(selected.End) {
				continue
			}
			records = append(records, record)
			group := getGroup(groups, record)
			group.AllAttempts.Attempts++
			if record.UpstreamAttempt > 1 {
				group.AllAttempts.RetryAttempts++
				key := keyForRecord(record)
				if retriedRequests[key] == nil {
					retriedRequests[key] = make(map[string]struct{})
				}
				retriedRequests[key][record.RequestID] = struct{}{}
			}
			addUsage(&group.AllAttempts.Tokens, &group.AllAttempts.UnknownTokens, &group.AllAttempts.UnknownRecords, record)
		}
	}

	finalRecords := make(map[string]Record)
	for _, record := range records {
		previous, exists := finalRecords[record.RequestID]
		if !exists || record.UpstreamAttempt > previous.UpstreamAttempt {
			finalRecords[record.RequestID] = record
		}
	}
	for _, record := range finalRecords {
		group := getGroup(groups, record)
		addOutcome(&group.RequestOutcomes, record.Status)
		if record.Status == StatusSuccess {
			group.SuccessfulRequestUsage.Requests++
			addUsage(&group.SuccessfulRequestUsage.Tokens, &group.SuccessfulRequestUsage.UnknownTokens, &group.SuccessfulRequestUsage.UnknownRecords, record)
		}
	}

	for key, group := range groups {
		group.AllAttempts.RetriedRequests = len(retriedRequests[key])
		result.Groups = append(result.Groups, *group)
	}
	sort.Slice(result.Groups, func(i, j int) bool {
		left, right := result.Groups[i], result.Groups[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.RequestedModel != right.RequestedModel {
			return left.RequestedModel < right.RequestedModel
		}
		return left.UpstreamModel < right.UpstreamModel
	})
	return result, nil
}

type groupKey struct {
	provider       string
	requestedModel string
	upstreamModel  string
}

type attemptKey struct {
	requestID string
	attempt   int
}

func getGroup(groups map[groupKey]*StatsGroup, record Record) *StatsGroup {
	key := keyForRecord(record)
	group := groups[key]
	if group == nil {
		group = &StatsGroup{Provider: record.Provider, RequestedModel: record.RequestedModel, UpstreamModel: record.UpstreamModel}
		groups[key] = group
	}
	return group
}

func keyForRecord(record Record) groupKey {
	return groupKey{record.Provider, record.RequestedModel, record.UpstreamModel}
}

func addUsage(totals *TokenTotals, unknownTokens *TokenUnknownCounts, unknownRecords *int, record Record) {
	if record.UsageSource == nil {
		(*unknownRecords)++
	}
	addToken(&totals.Input, &unknownTokens.Input, record.InputTokens)
	addToken(&totals.Output, &unknownTokens.Output, record.OutputTokens)
	addToken(&totals.CacheRead, &unknownTokens.CacheRead, record.CacheReadTokens)
	addToken(&totals.CacheWrite, &unknownTokens.CacheWrite, record.CacheWriteTokens)
	addToken(&totals.Reasoning, &unknownTokens.Reasoning, record.ReasoningTokens)
}

func addToken(total *int64, unknown *int, value *int) {
	if value != nil {
		*total += int64(*value)
		return
	}
	(*unknown)++
}

func addOutcome(outcomes *OutcomeCounts, status Status) {
	outcomes.Total++
	switch status {
	case StatusSuccess:
		outcomes.Success++
	case StatusError:
		outcomes.Error++
	case StatusCancelled:
		outcomes.Cancelled++
	default:
		outcomes.Unknown++
	}
}
