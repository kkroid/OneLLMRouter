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
	PeriodRange Period = "range"
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

type StatsGroup struct {
	Provider       string             `json:"provider"`
	RequestedModel string             `json:"requested_model"`
	UpstreamModel  string             `json:"upstream_model"`
	Tokens         TokenTotals        `json:"tokens"`
	UnknownTokens  TokenUnknownCounts `json:"unknown_tokens"`
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

func ParseDateRange(startValue, endValue string) (Range, error) {
	start, err := time.Parse("2006-01-02", startValue)
	if err != nil {
		return Range{}, fmt.Errorf("invalid range start %q: expected YYYY-MM-DD", startValue)
	}
	end, err := time.Parse("2006-01-02", endValue)
	if err != nil {
		return Range{}, fmt.Errorf("invalid range end %q: expected YYYY-MM-DD", endValue)
	}
	if end.Before(start) {
		return Range{}, fmt.Errorf("invalid date range: end date must not be before start date")
	}
	return Range{
		Period: PeriodRange,
		Label:  startValue + " to " + endValue,
		Start:  start,
		End:    end.AddDate(0, 0, 1),
	}, nil
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
			group := getGroup(groups, record)
			addUsage(&group.Tokens, &group.UnknownTokens, record)
		}
	}

	for _, group := range groups {
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

func addUsage(totals *TokenTotals, unknownTokens *TokenUnknownCounts, record Record) {
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
