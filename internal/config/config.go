package config

import (
	"fmt"
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
	DefaultReasoningLevel    string   `yaml:"default_reasoning_level"`
	SupportedReasoningLevels []string `yaml:"supported_reasoning_levels"`
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
	Default string `yaml:"default"`
	Opus    string `yaml:"opus"`
	Sonnet  string `yaml:"sonnet"`
	Haiku   string `yaml:"haiku"`
	Fable   string `yaml:"fable"`
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
