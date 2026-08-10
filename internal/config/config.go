package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the onellm-router configuration.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Log        LogConfig        `yaml:"log"`
	Proxy      ProxyConfig      `yaml:"proxy"`
	Retry      RetryConfig      `yaml:"retry"`
	Codex      CodexConfig      `yaml:"codex"`
	Providers  []ProviderConfig `yaml:"providers"`
	ModelSlots ModelSlotsConfig `yaml:"model_slots"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host     string `yaml:"host"`
	HTTPPort int    `yaml:"http_port"`
	Bell     *bool  `yaml:"bell,omitempty"` // nil or true = beep on error, false = silent
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level      string `yaml:"level"`
	Dir        string `yaml:"dir"`
	MaxAgeDays int    `yaml:"max_age_days"`
}

// ProxyConfig holds proxy settings for outbound requests.
type ProxyConfig struct {
	Socks5 string `yaml:"socks5"`
}

// Duration is a time.Duration decoded from a YAML string.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("duration must be a Go duration string, got %s", node.ShortTag())
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

// RetryConfig holds the global upstream retry settings.
type RetryConfig struct {
	Enabled         bool     `yaml:"enabled"`
	MaxAttempts     int      `yaml:"max_attempts"`
	StatusCodes     []int    `yaml:"status_codes"`
	InitialDelay    Duration `yaml:"initial_delay"`
	MaxDelay        Duration `yaml:"max_delay"`
	MaxElapsed      Duration `yaml:"max_elapsed"`
	Jitter          float64  `yaml:"jitter"`
	HonorRetryAfter bool     `yaml:"honor_retry_after"`
}

type CodexConfig struct {
	OverwriteCatalog bool                        `yaml:"overwrite_catalog"`
	Models           map[string]CodexModelConfig `yaml:"models"`
}

type CodexModelConfig struct {
	DefaultReasoningLevel    string   `yaml:"default_reasoning_level" json:"default_reasoning_level"`
	SupportedReasoningLevels []string `yaml:"supported_reasoning_levels" json:"supported_reasoning_levels"`
}

// ProviderConfig represents a single model provider.
type ProviderConfig struct {
	Name             string   `yaml:"name"`
	Prefix           string   `yaml:"prefix"`
	BaseURL          string   `yaml:"base_url"`
	OpenAIBaseURL    string   `yaml:"openai_base_url"`
	ResponsesBaseURL string   `yaml:"responses_base_url"` // OpenAI Responses API base (for Codex CLI direct passthrough)
	APIKey           string   `yaml:"api_key"`
	Models           []string `yaml:"models"`
	Proxy            *bool    `yaml:"proxy,omitempty"` // nil=inherit global, true=use proxy, false=direct
}

// ModelSlotsConfig maps Claude Code model slots to "prefix/model" identifiers.
type ModelSlotsConfig struct {
	Default string `yaml:"default" json:"default"`
	Opus    string `yaml:"opus" json:"opus"`
	Sonnet  string `yaml:"sonnet" json:"sonnet"`
	Haiku   string `yaml:"haiku" json:"haiku"`
	Fable   string `yaml:"fable" json:"fable"`
}

type Snapshot struct {
	Providers  []ProviderSnapshot `json:"providers"`
	Codex      SnapshotCodex      `json:"codex"`
	ModelSlots ModelSlotsConfig   `json:"model_slots"`
}

type SnapshotCodex struct {
	OverwriteCatalog *bool                       `json:"overwrite_catalog" yaml:"overwrite_catalog"`
	Models           map[string]CodexModelConfig `json:"models" yaml:"models"`
}

type ProviderSnapshot struct {
	Name             string   `json:"name"`
	Prefix           string   `json:"prefix"`
	BaseURL          string   `json:"base_url"`
	OpenAIBaseURL    string   `json:"openai_base_url"`
	ResponsesBaseURL string   `json:"responses_base_url"`
	APIKey           *string  `json:"api_key,omitempty"`
	APIKeySet        bool     `json:"api_key_set"`
	Models           []string `json:"models"`
	Proxy            *bool    `json:"proxy"`
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func NewSnapshot(cfg *Config) Snapshot {
	providers := make([]ProviderSnapshot, len(cfg.Providers))
	for index, provider := range cfg.Providers {
		providers[index] = ProviderSnapshot{
			Name: provider.Name, Prefix: provider.Prefix, BaseURL: provider.BaseURL,
			OpenAIBaseURL: provider.OpenAIBaseURL, ResponsesBaseURL: provider.ResponsesBaseURL,
			APIKeySet: provider.APIKey != "", Models: provider.Models, Proxy: provider.Proxy,
		}
	}
	return Snapshot{
		Providers:  providers,
		Codex:      SnapshotCodex{OverwriteCatalog: boolPointer(cfg.Codex.OverwriteCatalog), Models: cfg.Codex.Models},
		ModelSlots: cfg.ModelSlots,
	}
}

func DecodeSnapshot(reader io.Reader) (Snapshot, error) {
	var snapshot Snapshot
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("invalid JSON configuration")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Snapshot{}, fmt.Errorf("invalid JSON configuration")
	}
	return snapshot, nil
}

func (s Snapshot) Resolve(existing *Config) (*Config, []FieldError) {
	keys := make(map[string]string, len(existing.Providers))
	for _, provider := range existing.Providers {
		keys[provider.Prefix] = provider.APIKey
	}
	overwriteCatalog := existing.Codex.OverwriteCatalog
	if s.Codex.OverwriteCatalog != nil {
		overwriteCatalog = *s.Codex.OverwriteCatalog
	}
	resolved := &Config{
		Server: existing.Server, Log: existing.Log, Proxy: existing.Proxy, Retry: existing.Retry,
		Codex:      CodexConfig{OverwriteCatalog: overwriteCatalog, Models: s.Codex.Models},
		ModelSlots: s.ModelSlots, Providers: make([]ProviderConfig, len(s.Providers)),
	}
	var errors []FieldError
	for index, provider := range s.Providers {
		apiKey := ""
		if provider.APIKey != nil {
			apiKey = *provider.APIKey
		} else if oldKey, ok := keys[provider.Prefix]; ok {
			apiKey = oldKey
		} else {
			errors = append(errors, FieldError{
				Field:   fmt.Sprintf("providers[%d].api_key", index),
				Message: "api_key is required for a new or changed prefix",
			})
		}
		resolved.Providers[index] = ProviderConfig{
			Name: provider.Name, Prefix: provider.Prefix, BaseURL: provider.BaseURL,
			OpenAIBaseURL: provider.OpenAIBaseURL, ResponsesBaseURL: provider.ResponsesBaseURL,
			APIKey: apiKey, Models: provider.Models, Proxy: provider.Proxy,
		}
	}
	if err := resolved.Validate(); err != nil {
		errors = append(errors, fieldError(err))
	}
	return resolved, errors
}

func fieldError(err error) FieldError {
	message := err.Error()
	field := "$"
	if separator := strings.IndexAny(message, ": "); separator > 0 {
		field = strings.TrimSuffix(message[:separator], ":")
	}
	return FieldError{Field: field, Message: message}
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:     "127.0.0.1",
			HTTPPort: 3456,
		},
		Log: LogConfig{
			Level:      "info",
			Dir:        "~/.onellm/logs",
			MaxAgeDays: 30,
		},
		Proxy: ProxyConfig{
			Socks5: "127.0.0.1:1082",
		},
		Retry: RetryConfig{
			Enabled:         true,
			MaxAttempts:     15,
			StatusCodes:     []int{408, 409, 425, 429, 500, 502, 503, 504},
			InitialDelay:    Duration(time.Second),
			MaxDelay:        Duration(30 * time.Second),
			MaxElapsed:      Duration(5 * time.Minute),
			Jitter:          0.2,
			HonorRetryAfter: true,
		},
		Codex: CodexConfig{
			OverwriteCatalog: true,
			Models: map[string]CodexModelConfig{
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
			},
		},
	}
}

// Load reads and parses the config file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("配置文件不存在: %s\n  复制模板: cp onellm-router.example.yaml onellm-router.yaml", path)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("YAML 解析错误 — %w", err)
	}

	cfg.Log.Dir = expandHome(cfg.Log.Dir)
	return cfg, nil
}

// Validate checks the config for correctness.
func (c *Config) Validate() error {
	if c.Retry.MaxAttempts < 1 {
		return fmt.Errorf("retry.max_attempts must be at least 1")
	}
	if c.Retry.InitialDelay <= 0 {
		return fmt.Errorf("retry.initial_delay must be greater than 0")
	}
	if c.Retry.MaxDelay < c.Retry.InitialDelay {
		return fmt.Errorf("retry.max_delay must be greater than or equal to retry.initial_delay")
	}
	if c.Retry.MaxElapsed <= 0 {
		return fmt.Errorf("retry.max_elapsed must be greater than 0")
	}
	if math.IsNaN(c.Retry.Jitter) || c.Retry.Jitter < 0 || c.Retry.Jitter > 1 {
		return fmt.Errorf("retry.jitter must be between 0 and 1")
	}
	seenRetryStatuses := make(map[int]struct{}, len(c.Retry.StatusCodes))
	for _, status := range c.Retry.StatusCodes {
		if status < 100 || status > 599 || (status >= 200 && status < 300) {
			return fmt.Errorf("retry.status_codes must contain only non-2xx HTTP status codes from 100 to 599")
		}
		if _, exists := seenRetryStatuses[status]; exists {
			return fmt.Errorf("retry.status_codes contains duplicate status %d", status)
		}
		seenRetryStatuses[status] = struct{}{}
	}

	if len(c.Providers) == 0 {
		return fmt.Errorf("至少需要一个 provider（在 providers: 下配置）")
	}
	for i, p := range c.Providers {
		if p.Prefix == "" {
			return fmt.Errorf("providers[%d]: prefix 不能为空", i)
		}
		if p.BaseURL == "" && p.OpenAIBaseURL == "" && p.ResponsesBaseURL == "" {
			return fmt.Errorf("providers[%d] (%s): 至少需要一个 API 端点", i, p.Prefix)
		}
	}

	// Build set of valid model IDs for slot validation
	valid := make(map[string]bool)
	for _, p := range c.Providers {
		for _, m := range p.Models {
			valid[p.Prefix+"/"+m] = true
			// Also check [1m]-stripped variant
			if strings.HasSuffix(m, "[1m]") {
				valid[p.Prefix+"/"+strings.TrimSuffix(m, "[1m]")] = true
			}
		}
	}

	checkSlot := func(name, value string) {
		if value != "" && !valid[value] {
			// Also try without [1m]
			if !valid[value] {
				alt := value + "[1m]"
				if !valid[alt] {
					fmt.Fprintf(os.Stderr, "⚠️  model_slots.%s: %s 未在 providers 中配置\n", name, value)
				}
			}
		}
	}
	checkSlot("default", c.ModelSlots.Default)
	checkSlot("opus", c.ModelSlots.Opus)
	checkSlot("sonnet", c.ModelSlots.Sonnet)
	checkSlot("haiku", c.ModelSlots.Haiku)
	checkSlot("fable", c.ModelSlots.Fable)

	return nil
}

func RenderUpdate(original []byte, proposed *Config) ([]byte, error) {
	var originalDocument yaml.Node
	if err := yaml.Unmarshal(original, &originalDocument); err != nil {
		return nil, fmt.Errorf("parse existing YAML: %w", err)
	}
	managed := struct {
		Providers  []ProviderConfig `yaml:"providers"`
		Codex      SnapshotCodex    `yaml:"codex"`
		ModelSlots ModelSlotsConfig `yaml:"model_slots"`
	}{
		Providers:  proposed.Providers,
		Codex:      SnapshotCodex{OverwriteCatalog: boolPointer(proposed.Codex.OverwriteCatalog), Models: proposed.Codex.Models},
		ModelSlots: proposed.ModelSlots,
	}
	proposedData, err := yaml.Marshal(managed)
	if err != nil {
		return nil, fmt.Errorf("encode proposed YAML: %w", err)
	}
	var proposedDocument yaml.Node
	if err := yaml.Unmarshal(proposedData, &proposedDocument); err != nil {
		return nil, fmt.Errorf("parse proposed YAML: %w", err)
	}
	originalRoot := originalDocument.Content[0]
	proposedRoot := proposedDocument.Content[0]
	originalProviders := mappingValue(originalRoot, "providers")
	proposedProviders := mappingValue(proposedRoot, "providers")
	if originalProviders == nil {
		appendMapping(originalRoot, "providers", proposedProviders)
	} else {
		setProviders(originalProviders, proposedProviders)
	}
	setCodex(originalRoot, proposedRoot)
	setMappingSection(originalRoot, proposedRoot, "model_slots", []string{"default", "opus", "sonnet", "haiku", "fable"})
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(originalDocument.Content[0]); err != nil {
		return nil, fmt.Errorf("encode merged YAML: %w", err)
	}
	return output.Bytes(), nil
}

func boolPointer(value bool) *bool {
	return &value
}

func Apply(path string, proposed *Config) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config: %w", err)
	}
	updated, err := RenderUpdate(original, proposed)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	configTemp, err := writeTemporary(directory, filepath.Base(path), updated, info.Mode())
	if err != nil {
		return fmt.Errorf("stage config: %w", err)
	}
	defer os.Remove(configTemp)
	backupTemp, err := writeTemporary(directory, filepath.Base(path)+".bak", original, info.Mode())
	if err != nil {
		return fmt.Errorf("stage backup: %w", err)
	}
	defer os.Remove(backupTemp)
	if err := os.Rename(backupTemp, path+".bak"); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	if err := os.Rename(configTemp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func writeTemporary(directory, name string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(directory, "."+name+"-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	failed := true
	defer func() {
		file.Close()
		if failed {
			os.Remove(path)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	failed = false
	return path, nil
}

func setProviders(original, proposed *yaml.Node) {
	if original == nil || proposed == nil {
		return
	}
	byPrefix := make(map[string]*yaml.Node, len(original.Content))
	for _, provider := range original.Content {
		if prefix := mappingValue(provider, "prefix"); prefix != nil {
			byPrefix[prefix.Value] = provider
		}
	}
	merged := make([]*yaml.Node, 0, len(proposed.Content))
	for _, provider := range proposed.Content {
		prefix := mappingValue(provider, "prefix")
		if prefix != nil {
			if old := byPrefix[prefix.Value]; old != nil {
				setMappingFields(old, provider, []string{
					"name", "prefix", "base_url", "openai_base_url", "responses_base_url", "api_key", "models", "proxy",
				})
				merged = append(merged, old)
				continue
			}
		}
		merged = append(merged, provider)
	}
	original.Content = merged
}

func setCodex(originalRoot, proposedRoot *yaml.Node) {
	originalCodex := mappingValue(originalRoot, "codex")
	proposedCodex := mappingValue(proposedRoot, "codex")
	if proposedCodex == nil {
		return
	}
	if originalCodex == nil {
		appendMapping(originalRoot, "codex", proposedCodex)
		return
	}
	setMappingFields(originalCodex, proposedCodex, []string{"overwrite_catalog"})
	originalModels := mappingValue(originalCodex, "models")
	proposedModels := mappingValue(proposedCodex, "models")
	if originalModels == nil {
		appendMapping(originalCodex, "models", proposedModels)
		return
	}
	type modelNodes struct {
		key   *yaml.Node
		value *yaml.Node
	}
	oldModels := make(map[string]modelNodes, len(originalModels.Content)/2)
	for index := 0; index < len(originalModels.Content); index += 2 {
		oldModels[originalModels.Content[index].Value] = modelNodes{
			key: originalModels.Content[index], value: originalModels.Content[index+1],
		}
	}
	for index := 0; index < len(proposedModels.Content); index += 2 {
		if old, exists := oldModels[proposedModels.Content[index].Value]; exists {
			setMappingFields(old.value, proposedModels.Content[index+1], []string{"default_reasoning_level", "supported_reasoning_levels"})
			proposedModels.Content[index] = old.key
			proposedModels.Content[index+1] = old.value
		}
	}
	originalModels.Content = proposedModels.Content
}

func setMappingSection(originalRoot, proposedRoot *yaml.Node, name string, fields []string) {
	proposed := mappingValue(proposedRoot, name)
	if proposed == nil {
		return
	}
	original := mappingValue(originalRoot, name)
	if original == nil {
		appendMapping(originalRoot, name, proposed)
		return
	}
	setMappingFields(original, proposed, fields)
}

func setMappingFields(original, proposed *yaml.Node, fields []string) {
	for _, field := range fields {
		proposedValue := mappingValue(proposed, field)
		if proposedValue == nil {
			removeMapping(original, field)
			continue
		}
		originalValue := mappingValue(original, field)
		if originalValue == nil {
			appendMapping(original, field, proposedValue)
			continue
		}
		proposedValue.HeadComment = originalValue.HeadComment
		proposedValue.LineComment = originalValue.LineComment
		proposedValue.FootComment = originalValue.FootComment
		*originalValue = *proposedValue
	}
}

func mappingValue(mapping *yaml.Node, name string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == name {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func appendMapping(mapping *yaml.Node, name string, value *yaml.Node) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, value)
}

func removeMapping(mapping *yaml.Node, name string) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == name {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}

// DefaultUserDir returns the OneLLMRouter user data directory.
func DefaultUserDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".onellm"
	}
	return filepath.Join(home, ".onellm")
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
