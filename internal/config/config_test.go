package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSnapshotOmitsStoredAPIKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []ProviderConfig{{Prefix: "alpha", APIKey: "old-" + "secret"}}

	data, err := json.Marshal(NewSnapshot(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(cfg.Providers[0].APIKey)) || bytes.Contains(data, []byte(`"api_key":`)) {
		t.Fatalf("snapshot exposed API key: %s", data)
	}
	if !bytes.Contains(data, []byte(`"api_key_set":true`)) {
		t.Fatalf("snapshot omitted api_key_set: %s", data)
	}
}

func TestSnapshotResolvePreservesKeyByPrefix(t *testing.T) {
	existing := DefaultConfig()
	existing.Providers = []ProviderConfig{{Prefix: "alpha", BaseURL: "https://example.invalid", APIKey: "old-" + "secret"}}
	snapshot := NewSnapshot(existing)

	resolved, errors := snapshot.Resolve(existing)
	if len(errors) != 0 {
		t.Fatalf("Resolve() errors = %+v", errors)
	}
	if got := resolved.Providers[0].APIKey; got != existing.Providers[0].APIKey {
		t.Fatalf("preserved API key = %q", got)
	}

	snapshot.Providers[0].Prefix = "beta"
	_, errors = snapshot.Resolve(existing)
	if len(errors) == 0 || errors[0].Field != "providers[0].api_key" {
		t.Fatalf("changed prefix errors = %+v", errors)
	}
}

func TestSnapshotAppliesOnlyCodexOverwriteCatalogAndModels(t *testing.T) {
	for _, test := range []struct {
		name     string
		existing bool
		proposed *bool
		want     bool
	}{
		{name: "omitted preserves true", existing: true, proposed: nil, want: true},
		{name: "explicit true applies", existing: false, proposed: boolPointer(true), want: true},
		{name: "explicit false applies", existing: true, proposed: boolPointer(false), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			existing := DefaultConfig()
			existing.Codex.OverwriteCatalog = test.existing
			existing.Providers = []ProviderConfig{{Prefix: "alpha", BaseURL: "https://example.invalid", APIKey: "secret"}}
			snapshot := NewSnapshot(existing)
			snapshot.Codex.OverwriteCatalog = test.proposed

			resolved, errors := snapshot.Resolve(existing)
			if len(errors) != 0 {
				t.Fatalf("Resolve() errors = %+v", errors)
			}
			if resolved.Codex.OverwriteCatalog != test.want {
				t.Fatalf("overwrite_catalog = %v, want %v", resolved.Codex.OverwriteCatalog, test.want)
			}
			if !reflect.DeepEqual(resolved.Server, existing.Server) || !reflect.DeepEqual(resolved.Retry, existing.Retry) {
				t.Fatal("Resolve() changed a closed configuration section")
			}
		})
	}
}

func TestRenderUpdatePreservesCommentsAndUnknownFields(t *testing.T) {
	original := []byte(`# top comment
future_setting: keep
server:
  host: 127.0.0.1 # host comment
retry:
  max_attempts: 4
providers:
  - prefix: alpha
    base_url: https://old.invalid
    api_key: old-secret
    future_provider: keep
codex:
  overwrite_catalog: false
  future_codex: keep
  models:
    old: # model comment
      default_reasoning_level: low
      supported_reasoning_levels: [low]
      future_model: keep
`)
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	proposed, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	proposed.Providers[0].BaseURL = "https://new.invalid"
	proposed.Codex.Models["old"] = CodexModelConfig{DefaultReasoningLevel: "high", SupportedReasoningLevels: []string{"high"}}
	proposed.Codex.OverwriteCatalog = true

	updated, err := RenderUpdate(original, proposed)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# top comment", "# host comment", "# model comment", "future_setting: keep", "future_provider: keep", "future_codex: keep", "future_model: keep", "host: 127.0.0.1", "max_attempts: 4", "overwrite_catalog: true", "https://new.invalid"} {
		if !bytes.Contains(updated, []byte(expected)) {
			t.Errorf("updated YAML omitted %q:\n%s", expected, updated)
		}
	}
}

func TestApplyCreatesExactBackupAndPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	original := []byte("# original\nproviders:\n  - prefix: alpha\n    base_url: https://old.invalid\n    api_key: old-secret\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	proposed, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	proposed.Providers[0].APIKey = "new-" + "secret"
	if err := Apply(path, proposed); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatalf("backup differs from pre-image:\n%s", backup)
	}
	if bytes.Contains(backup, []byte(proposed.Providers[0].APIKey)) {
		t.Fatal("backup contains proposed API key")
	}
	backupInfo, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := backupInfo.Mode().Perm(), originalInfo.Mode().Perm(); got != want {
		t.Fatalf("backup mode = %o, want original mode %o", got, want)
	}
}

func TestRenderUpdateAddsMissingManagedSections(t *testing.T) {
	original := []byte("server:\n  host: 127.0.0.1\n")
	proposed := DefaultConfig()
	proposed.Providers = []ProviderConfig{{Prefix: "alpha", BaseURL: "https://example.invalid", APIKey: "secret"}}
	proposed.ModelSlots.Default = "alpha/model"

	updated, err := RenderUpdate(original, proposed)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"host: 127.0.0.1", "providers:", "prefix: alpha", "codex:", "model_slots:"} {
		if !bytes.Contains(updated, []byte(expected)) {
			t.Errorf("updated YAML omitted %q:\n%s", expected, updated)
		}
	}
}

func TestDefaultConfigOverwritesCodexCatalog(t *testing.T) {
	if !DefaultConfig().Codex.OverwriteCatalog {
		t.Fatal("codex catalog overwrite must default to enabled")
	}
}

func TestValidateRequiresProviderEndpointRegardlessOfPrefix(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []ProviderConfig{{
		Name: "ordinary cp prefix", Prefix: "cp", Models: []string{"m1"},
	}}

	if err := cfg.Validate(); err == nil {
		t.Fatal("provider without an API endpoint must fail validation")
	}
}

func TestLoadPreservesExplicitCodexCatalogDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	data := []byte("codex:\n  overwrite_catalog: false\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Codex.OverwriteCatalog {
		t.Fatal("explicit codex catalog overwrite disable was ignored")
	}
}

func TestDefaultConfigIncludesKnownCodexReasoningModels(t *testing.T) {
	want := map[string]CodexModelConfig{
		"gpt-5.5": {
			DefaultReasoningLevel:    "medium",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh"},
		},
		"gpt-5.6-sol": {
			DefaultReasoningLevel:    "low",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
		},
		"gpt-5.6-terra": {
			DefaultReasoningLevel:    "medium",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
		},
		"gpt-5.6-luna": {
			DefaultReasoningLevel:    "medium",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"},
		},
	}
	if got := DefaultConfig().Codex.Models; !reflect.DeepEqual(got, want) {
		t.Fatalf("Codex model mappings = %#v, want %#v", got, want)
	}
}

func TestLoadOverridesDefaultCodexReasoningModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	data := []byte(`codex:
  models:
    gpt-5.6-sol:
      default_reasoning_level: high
      supported_reasoning_levels: [high, xhigh]
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := CodexModelConfig{
		DefaultReasoningLevel:    "high",
		SupportedReasoningLevels: []string{"high", "xhigh"},
	}
	if got := cfg.Codex.Models["gpt-5.6-sol"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("configured mapping = %#v, want %#v", got, want)
	}
}

func TestDefaultConfigIncludesRetryDefaults(t *testing.T) {
	want := RetryConfig{
		Enabled:         true,
		MaxAttempts:     15,
		StatusCodes:     []int{408, 409, 425, 429, 500, 502, 503, 504},
		InitialDelay:    Duration(time.Second),
		MaxDelay:        Duration(30 * time.Second),
		MaxElapsed:      Duration(5 * time.Minute),
		Jitter:          0.2,
		HonorRetryAfter: true,
	}
	if got := DefaultConfig().Retry; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry defaults = %#v, want %#v", got, want)
	}
}

func TestLoadOverridesRetryStatusCodes(t *testing.T) {
	cfg := loadTestConfig(t, "retry:\n  status_codes: [403, 429]\n")

	if got, want := cfg.Retry.StatusCodes, []int{403, 429}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry status codes = %v, want %v", got, want)
	}
}

func TestLoadPreservesExplicitEmptyRetryStatusCodes(t *testing.T) {
	cfg := loadTestConfig(t, "retry:\n  status_codes: []\n")

	if cfg.Retry.StatusCodes == nil || len(cfg.Retry.StatusCodes) != 0 {
		t.Fatalf("retry status codes = %#v, want explicit empty list", cfg.Retry.StatusCodes)
	}
}

func TestLoadPreservesRetryDefaultsWhenBlockMissing(t *testing.T) {
	cfg := loadTestConfig(t, "log:\n  level: debug\n")

	if got, want := cfg.Retry, DefaultConfig().Retry; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry config = %#v, want defaults %#v", got, want)
	}
}

func TestLoadPreservesIndividuallyMissingRetryFields(t *testing.T) {
	cfg := loadTestConfig(t, "retry:\n  max_attempts: 3\n")
	want := DefaultConfig().Retry
	want.MaxAttempts = 3

	if !reflect.DeepEqual(cfg.Retry, want) {
		t.Fatalf("retry config = %#v, want %#v", cfg.Retry, want)
	}
}

func TestLoadPreservesExplicitRetryDisable(t *testing.T) {
	cfg := loadTestConfig(t, "retry:\n  enabled: false\n")

	if cfg.Retry.Enabled {
		t.Fatal("explicit retry disable was ignored")
	}
}

func TestLoadAcceptsStringDurations(t *testing.T) {
	cfg := loadTestConfig(t, `retry:
  initial_delay: 250ms
  max_delay: 30s
  max_elapsed: 5m
`)

	if got, want := time.Duration(cfg.Retry.InitialDelay), 250*time.Millisecond; got != want {
		t.Fatalf("initial_delay = %s, want %s", got, want)
	}
	if got, want := time.Duration(cfg.Retry.MaxDelay), 30*time.Second; got != want {
		t.Fatalf("max_delay = %s, want %s", got, want)
	}
	if got, want := time.Duration(cfg.Retry.MaxElapsed), 5*time.Minute; got != want {
		t.Fatalf("max_elapsed = %s, want %s", got, want)
	}
}

func TestLoadRejectsBareNumberDuration(t *testing.T) {
	path := writeTestConfig(t, "retry:\n  initial_delay: 250\n")

	if _, err := Load(path); err == nil {
		t.Fatal("bare numeric duration must fail to load")
	}
}

func TestLoadAndValidateRejectsExplicitInvalidRetryConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		field string
	}{
		{name: "zero max attempts", yaml: "retry:\n  max_attempts: 0\n", field: "retry.max_attempts"},
		{name: "negative max attempts", yaml: "retry:\n  max_attempts: -1\n", field: "retry.max_attempts"},
		{name: "zero initial delay", yaml: "retry:\n  initial_delay: 0s\n", field: "retry.initial_delay"},
		{name: "negative initial delay", yaml: "retry:\n  initial_delay: -1s\n", field: "retry.initial_delay"},
		{name: "max delay below initial delay", yaml: "retry:\n  initial_delay: 2s\n  max_delay: 1s\n", field: "retry.max_delay"},
		{name: "zero max elapsed", yaml: "retry:\n  max_elapsed: 0s\n", field: "retry.max_elapsed"},
		{name: "negative max elapsed while disabled", yaml: "retry:\n  enabled: false\n  max_elapsed: -1s\n", field: "retry.max_elapsed"},
		{name: "jitter below zero", yaml: "retry:\n  jitter: -0.01\n", field: "retry.jitter"},
		{name: "jitter above one", yaml: "retry:\n  jitter: 1.01\n", field: "retry.jitter"},
		{name: "jitter NaN", yaml: "retry:\n  jitter: .nan\n", field: "retry.jitter"},
		{name: "success status", yaml: "retry:\n  status_codes: [200]\n", field: "retry.status_codes"},
		{name: "status below range", yaml: "retry:\n  status_codes: [99]\n", field: "retry.status_codes"},
		{name: "status above range", yaml: "retry:\n  status_codes: [600]\n", field: "retry.status_codes"},
		{name: "duplicate status", yaml: "retry:\n  status_codes: [429, 429]\n", field: "retry.status_codes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadTestConfig(t, tt.yaml)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted invalid %s", tt.field)
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Validate() error = %q, want field %q", err, tt.field)
			}
		})
	}
}

func TestValidateAcceptsRetryJitterBounds(t *testing.T) {
	for _, jitter := range []float64{0, 1} {
		cfg := DefaultConfig()
		cfg.Retry.Jitter = jitter
		cfg.Providers = []ProviderConfig{{Prefix: "test", BaseURL: "https://example.com"}}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() rejected jitter %v: %v", jitter, err)
		}
	}
}

func TestExampleDocumentsRetryPolicy(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "onellm-router.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"全局上游重试",
		"默认启用",
		"包含首次调用",
		"整个错误恢复预算",
		"单次等待上限",
		"status_codes",
		"未列出的 HTTP 状态不会重试",
	} {
		if !strings.Contains(string(data), statement) {
			t.Errorf("example retry comments do not state %q", statement)
		}
	}
}

func loadTestConfig(t *testing.T, data string) *Config {
	t.Helper()
	path := writeTestConfig(t, data)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func writeTestConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
