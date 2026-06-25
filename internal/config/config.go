package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the onellm-router configuration.
type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Log       LogConfig        `yaml:"log"`
	Proxy     ProxyConfig      `yaml:"proxy"`
	Providers []ProviderConfig `yaml:"providers"`
	ModelSlots ModelSlotsConfig `yaml:"model_slots"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host     string `yaml:"host"`
	HTTPPort int    `yaml:"http_port"`
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

// ProviderConfig represents a single model provider.
type ProviderConfig struct {
	Name          string   `yaml:"name"`
	Prefix        string   `yaml:"prefix"`
	BaseURL       string   `yaml:"base_url"`
	OpenAIBaseURL string   `yaml:"openai_base_url"`
	APIKey        string   `yaml:"api_key"`
	Models        []string `yaml:"models"`
	Proxy         *bool    `yaml:"proxy,omitempty"` // nil=inherit global, true=use proxy, false=direct
}

// ModelSlotsConfig maps Claude Code model slots to "prefix/model" identifiers.
type ModelSlotsConfig struct {
	Default string `yaml:"default"`
	Opus   string `yaml:"opus"`
	Sonnet string `yaml:"sonnet"`
	Haiku  string `yaml:"haiku"`
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
	if len(c.Providers) == 0 {
		return fmt.Errorf("至少需要一个 provider（在 providers: 下配置）")
	}
	for i, p := range c.Providers {
		if p.Prefix == "" {
			return fmt.Errorf("providers[%d]: prefix 不能为空", i)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("providers[%d] (%s): 至少需要一个模型（在 models: 下配置）", i, p.Prefix)
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

// DefaultTokenFile returns the default path for the GitHub token.
func DefaultTokenFile() string {
	return filepath.Join(DefaultUserDir(), "github_token")
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
