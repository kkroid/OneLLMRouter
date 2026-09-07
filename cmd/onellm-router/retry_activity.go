package main

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kkroid/onellm-router/internal/upstream"
)

type retryActivityTracker struct {
	mu         sync.Mutex
	models     map[string]retryActivity
	now        func() time.Time
	visibleFor time.Duration
}

type retryActivity struct {
	active       int
	visibleUntil time.Time
}

func newRetryActivityTracker() *retryActivityTracker {
	return &retryActivityTracker{
		models:     make(map[string]retryActivity),
		now:        time.Now,
		visibleFor: 3 * time.Second,
	}
}

func (t *retryActivityTracker) Observe(event upstream.RetryEvent) {
	model := retryDisplayModel(event.Metadata)
	if model == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	activity := t.models[model]
	if event.Active {
		activity.active++
		activity.visibleUntil = time.Time{}
		t.models[model] = activity
		return
	}
	if activity.active == 0 {
		return
	}
	activity.active--
	if activity.active == 0 {
		activity.visibleUntil = t.now().Add(t.visibleFor)
	}
	t.models[model] = activity
}

func (t *retryActivityTracker) Models() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	models := make([]string, 0, len(t.models))
	now := t.now()
	for model, activity := range t.models {
		if activity.active == 0 && !now.Before(activity.visibleUntil) {
			delete(t.models, model)
			continue
		}
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

func retryDisplayModel(metadata upstream.Metadata) string {
	if model := strings.TrimSpace(metadata.RequestedModel); model != "" {
		return model
	}
	model := strings.TrimSpace(metadata.Model)
	provider := strings.TrimSpace(metadata.Provider)
	if model == "" || provider == "" || strings.Contains(model, "/") {
		return model
	}
	return provider + "/" + model
}
