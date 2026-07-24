package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMarshalCodexPreservesModelOrderAndRequiredFields(t *testing.T) {
	data, err := MarshalCodex([]Model{
		{ID: "c78/gpt-5.6-sol"},
		{ID: "mars/gpt-5.6-terra"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(document.Models))
	}

	for index, wantSlug := range []string{"c78/gpt-5.6-sol", "mars/gpt-5.6-terra"} {
		var slug string
		if err := json.Unmarshal(document.Models[index]["slug"], &slug); err != nil {
			t.Fatal(err)
		}
		if slug != wantSlug {
			t.Fatalf("model %d slug = %q, want %q", index, slug, wantSlug)
		}
		for _, field := range []string{
			"display_name",
			"supported_reasoning_levels",
			"shell_type",
			"visibility",
			"supported_in_api",
			"priority",
			"base_instructions",
			"truncation_policy",
			"supports_parallel_tool_calls",
			"experimental_supported_tools",
		} {
			if _, ok := document.Models[index][field]; !ok {
				t.Errorf("model %d missing %q", index, field)
			}
		}
	}
}

func TestMarshalCodexUsesReasoningPresetObjects(t *testing.T) {
	data, err := MarshalCodex([]Model{{
		ID:                    "c78/gpt-5.6-sol",
		DefaultReasoningLevel: "low",
		SupportedReasoningLevels: []ReasoningLevel{
			{Effort: "low", Description: "Fast responses"},
			{Effort: "medium", Description: "Balanced"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Models []struct {
			DefaultReasoningLevel    string           `json:"default_reasoning_level"`
			SupportedReasoningLevels []ReasoningLevel `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(document.Models))
	}
	model := document.Models[0]
	if model.DefaultReasoningLevel != "low" {
		t.Fatalf("default reasoning level = %q, want low", model.DefaultReasoningLevel)
	}
	if len(model.SupportedReasoningLevels) != 2 || model.SupportedReasoningLevels[0].Effort != "low" {
		t.Fatalf("reasoning levels = %#v", model.SupportedReasoningLevels)
	}
}

func TestMarshalCodexFallsBackToCommonReasoningLevels(t *testing.T) {
	data, err := MarshalCodex([]Model{{ID: "custom/future-model"}})
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Models []struct {
			DefaultReasoningLevel    string           `json:"default_reasoning_level"`
			SupportedReasoningLevels []ReasoningLevel `json:"supported_reasoning_levels"`
			Description              any              `json:"description"`
			AvailabilityNux          any              `json:"availability_nux"`
			AdditionalSpeedTiers     []string         `json:"additional_speed_tiers"`
			ServiceTiers             []any            `json:"service_tiers"`
			DefaultServiceTier       any              `json:"default_service_tier"`
			AutoReviewModelOverride  any              `json:"auto_review_model_override"`
			PreferWebSockets         bool             `json:"prefer_websockets"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	model := document.Models[0]
	if model.DefaultReasoningLevel != "medium" {
		t.Fatalf("fallback default = %q, want medium", model.DefaultReasoningLevel)
	}
	want := []string{"low", "medium", "high", "xhigh"}
	got := make([]string, len(model.SupportedReasoningLevels))
	for i, level := range model.SupportedReasoningLevels {
		got[i] = level.Effort
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback levels = %#v, want %#v", got, want)
	}
	if model.Description != nil || model.AvailabilityNux != nil || len(model.AdditionalSpeedTiers) != 0 || len(model.ServiceTiers) != 0 || model.DefaultServiceTier != nil || model.AutoReviewModelOverride != nil {
		t.Fatalf("unknown model inherited model-specific availability metadata: %+v", model)
	}
	if model.PreferWebSockets {
		t.Fatal("unknown model must not prefer WebSockets")
	}
}

func TestMarshalCodexPreservesUpstreamProviderMetadataButDisablesWebSockets(t *testing.T) {
	data, err := MarshalCodex([]Model{{
		ID: "custom/future-model",
		CodexMetadata: json.RawMessage(`{
			"description":"upstream description",
			"availability_nux":{"message":"upstream"},
			"additional_speed_tiers":["fast"],
			"service_tiers":[{"id":"priority"}],
			"default_service_tier":"priority",
			"auto_review_model_override":"review-model",
			"prefer_websockets":true
		}`),
	}})
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Models []struct {
			Description             string            `json:"description"`
			AvailabilityNux         map[string]string `json:"availability_nux"`
			AdditionalSpeedTiers    []string          `json:"additional_speed_tiers"`
			ServiceTiers            []map[string]any  `json:"service_tiers"`
			DefaultServiceTier      string            `json:"default_service_tier"`
			AutoReviewModelOverride string            `json:"auto_review_model_override"`
			PreferWebSockets        bool              `json:"prefer_websockets"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	model := document.Models[0]
	if model.Description != "upstream description" || model.AvailabilityNux["message"] != "upstream" || len(model.AdditionalSpeedTiers) != 1 || len(model.ServiceTiers) != 1 || model.DefaultServiceTier != "priority" || model.AutoReviewModelOverride != "review-model" {
		t.Fatalf("upstream provider metadata was not preserved: %+v", model)
	}
	if model.PreferWebSockets {
		t.Fatal("WebSockets must remain disabled even when upstream enables them")
	}
}

func TestMarshalCodexKnownModelUsesOfficialCapabilities(t *testing.T) {
	data, err := MarshalCodex([]Model{{ID: "c78/gpt-5.6-sol"}})
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Models []struct {
			ShellType                  string `json:"shell_type"`
			ApplyPatchToolType         string `json:"apply_patch_tool_type"`
			SupportsParallelToolCalls  bool   `json:"supports_parallel_tool_calls"`
			SupportsReasoningSummaries bool   `json:"supports_reasoning_summaries"`
			SupportVerbosity           bool   `json:"support_verbosity"`
			ContextWindow              int64  `json:"context_window"`
			BaseInstructions           string `json:"base_instructions"`
			TruncationPolicy           struct {
				Mode  string `json:"mode"`
				Limit int64  `json:"limit"`
			} `json:"truncation_policy"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	model := document.Models[0]
	if model.ShellType != "shell_command" || model.ApplyPatchToolType != "freeform" {
		t.Fatalf("official tool capabilities were not preserved: %+v", model)
	}
	if !model.SupportsParallelToolCalls || !model.SupportsReasoningSummaries || !model.SupportVerbosity {
		t.Fatalf("official capability flags were not preserved: %+v", model)
	}
	if model.TruncationPolicy.Mode != "tokens" || model.TruncationPolicy.Limit != 10_000 {
		t.Fatalf("truncation policy = %+v", model.TruncationPolicy)
	}
	if model.ContextWindow != 272_000 {
		t.Fatalf("context window = %d, want 272000", model.ContextWindow)
	}
	if len(model.BaseInstructions) < 10_000 {
		t.Fatalf("base instructions are unexpectedly short: %d", len(model.BaseInstructions))
	}
}

func TestMarshalCodexCompletesPartialReasoningMetadata(t *testing.T) {
	data, err := MarshalCodex([]Model{
		{
			ID:                    "custom/default-only",
			DefaultReasoningLevel: "high",
			HasReasoningMetadata:  true,
		},
		{
			ID: "custom/levels-only",
			SupportedReasoningLevels: []ReasoningLevel{
				{Effort: "low", Description: "Low"},
				{Effort: "medium", Description: "Medium"},
			},
			HasReasoningMetadata: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Models []struct {
			DefaultReasoningLevel    string           `json:"default_reasoning_level"`
			SupportedReasoningLevels []ReasoningLevel `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if got := document.Models[0]; got.DefaultReasoningLevel != "high" || len(got.SupportedReasoningLevels) != 4 {
		t.Fatalf("default-only metadata was not completed: %+v", got)
	}
	if got := document.Models[1]; got.DefaultReasoningLevel != "medium" || len(got.SupportedReasoningLevels) != 2 {
		t.Fatalf("levels-only metadata was not completed: %+v", got)
	}
}
