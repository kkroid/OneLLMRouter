package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kkroid/onellm-router/internal/claudeconfig"
	"github.com/kkroid/onellm-router/internal/config"
)

func TestPrintClaudeCodeSettingsOnlyIncludesManagedEnvironment(t *testing.T) {
	const secret = "upstream-secret-must-not-be-printed"
	cfg := &config.Config{
		Server: config.ServerConfig{HTTPPort: 4567},
		Providers: []config.ProviderConfig{
			{Prefix: "alpha", APIKey: secret},
		},
		ModelSlots: config.ModelSlotsConfig{
			Default: "alpha/default",
			Opus:    "alpha/opus",
			Sonnet:  "alpha/sonnet",
			Haiku:   "alpha/haiku",
			Fable:   "alpha/fable",
		},
	}

	var output bytes.Buffer
	printClaudeCodeSettings(&output, cfg)

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &settings); err != nil {
		t.Fatalf("decode startup settings: %v", err)
	}
	if len(settings) != 1 {
		t.Fatalf("top-level settings = %v, want only env", settings)
	}
	envJSON, ok := settings["env"]
	if !ok {
		t.Fatal("startup settings do not contain env")
	}

	var env map[string]string
	if err := json.Unmarshal(envJSON, &env); err != nil {
		t.Fatalf("decode managed environment: %v", err)
	}
	if len(env) != claudeconfig.ManagedKeyCount {
		t.Fatalf("managed environment keys = %d, want %d", len(env), claudeconfig.ManagedKeyCount)
	}
	want := map[string]string{
		"ANTHROPIC_BASE_URL":             "http://localhost:4567/anthropic",
		"ANTHROPIC_AUTH_TOKEN":           "x",
		"ANTHROPIC_MODEL":                "alpha/default",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "alpha/opus",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "alpha/sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "alpha/haiku",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  "alpha/fable",
	}
	for key, value := range want {
		if env[key] != value {
			t.Errorf("env[%q] = %q, want %q", key, env[key], value)
		}
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("startup settings contain an upstream API key")
	}
}
