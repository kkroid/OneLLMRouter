package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/kkroid/onellm-router/internal/upstream"
)

func TestRetryActivityTrackerTracksConcurrentModels(t *testing.T) {
	tracker := newRetryActivityTracker()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return now }
	flash := upstream.RetryEvent{Metadata: upstream.Metadata{RequestedModel: "ds/flash"}, Active: true}
	pro := upstream.RetryEvent{Metadata: upstream.Metadata{RequestedModel: "ds/pro"}, Active: true}

	tracker.Observe(pro)
	tracker.Observe(flash)
	tracker.Observe(flash)
	if got, want := tracker.Models(), []string{"ds/flash", "ds/pro"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Models() = %v, want %v", got, want)
	}

	flash.Active = false
	tracker.Observe(flash)
	if got := tracker.Models(); !reflect.DeepEqual(got, []string{"ds/flash", "ds/pro"}) {
		t.Fatalf("first completion removed concurrent model: %v", got)
	}
	tracker.Observe(flash)
	pro.Active = false
	tracker.Observe(pro)
	if got := tracker.Models(); !reflect.DeepEqual(got, []string{"ds/flash", "ds/pro"}) {
		t.Fatalf("Models() during visibility window = %v", got)
	}
	now = now.Add(tracker.visibleFor)
	if got := tracker.Models(); len(got) != 0 {
		t.Fatalf("Models() after visibility window = %v", got)
	}
}

func TestRetryDisplayModelUsesRequestedModel(t *testing.T) {
	metadata := upstream.Metadata{
		Provider:       "ds",
		Model:          "deepseek-v4-flash[1m]",
		RequestedModel: "ds/deepseek-v4-flash-1m",
	}
	if got := retryDisplayModel(metadata); got != metadata.RequestedModel {
		t.Fatalf("retryDisplayModel() = %q", got)
	}
	if got := retryDisplayModel(upstream.Metadata{Provider: "ds", Model: "fallback"}); got != "ds/fallback" {
		t.Fatalf("fallback retryDisplayModel() = %q", got)
	}
}
