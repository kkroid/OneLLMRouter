package codexconfig

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const SourceTag = "OneLLMRouter"

type Options struct {
	ConfigPath        string
	OneLLMCatalogPath string
	CodexCatalogPath  string
	ExpectedBaseURL   string
	OverwriteCatalog  bool
}

type Result struct {
	ConfigPath              string   `json:"config_path"`
	ConfigExists            bool     `json:"config_exists"`
	ConfigParseState        string   `json:"config_parse_state"`
	ConfigSyncState         string   `json:"config_sync_state"`
	OneLLMCatalogPath       string   `json:"onellm_catalog_path"`
	OneLLMCatalogExists     bool     `json:"onellm_catalog_exists"`
	CodexCatalogPath        string   `json:"codex_catalog_path"`
	CodexCatalogExists      bool     `json:"codex_catalog_exists"`
	CatalogSyncState        string   `json:"catalog_sync_state"`
	CatalogGeneratedAt      *string  `json:"catalog_generated_at"`
	ModelCount              int      `json:"model_count"`
	EffectiveModel          *string  `json:"effective_model"`
	EffectiveProvider       *string  `json:"effective_provider"`
	ConfiguredModelProvider *string  `json:"configured_model_provider"`
	SourceProviders         []string `json:"source_providers"`
	SourceTag               string   `json:"source_tag"`
	OverwriteCatalog        bool     `json:"overwrite_catalog"`
	ConfigWriteSupported    bool     `json:"config_write_supported"`
	WrittenPaths            []string `json:"written_paths"`
	Snippet                 *string  `json:"snippet"`
}

type configDocument struct {
	Model            *string                   `toml:"model"`
	ModelProvider    *string                   `toml:"model_provider"`
	ModelCatalogJSON *string                   `toml:"model_catalog_json"`
	ModelProviders   map[string]providerConfig `toml:"model_providers"`
}

type providerConfig struct {
	Name               *string `toml:"name"`
	BaseURL            *string `toml:"base_url"`
	WireAPI            *string `toml:"wire_api"`
	RequiresOpenAIAuth *bool   `toml:"requires_openai_auth"`
}

func Inspect(options Options) Result {
	result := Result{
		ConfigPath: options.ConfigPath, OneLLMCatalogPath: options.OneLLMCatalogPath,
		CodexCatalogPath: options.CodexCatalogPath, SourceTag: SourceTag,
		OverwriteCatalog: options.OverwriteCatalog, SourceProviders: []string{}, WrittenPaths: []string{},
	}
	inspectConfig(&result, options)
	inspectCatalogs(&result)
	return result
}

func CatalogHasModel(path, selected string) bool {
	data, state := readFile(path)
	if state != "valid" {
		return false
	}
	models, valid := decodeCatalog(data)
	if !valid {
		return false
	}
	for _, model := range models {
		if model == selected {
			return true
		}
	}
	return false
}

func inspectConfig(result *Result, options Options) {
	data, state := readFile(options.ConfigPath)
	result.ConfigExists = state != "absent"
	if state != "valid" {
		result.ConfigParseState = state
		result.ConfigSyncState = state
		return
	}
	var document configDocument
	if err := toml.Unmarshal(data, &document); err != nil {
		result.ConfigParseState = "invalid"
		result.ConfigSyncState = "invalid"
		return
	}
	result.ConfigParseState = "valid"
	result.EffectiveModel = document.Model
	result.ConfiguredModelProvider = document.ModelProvider
	if document.Model != nil {
		if provider, _, found := strings.Cut(*document.Model, "/"); found && provider != "" {
			result.EffectiveProvider = &provider
		}
	}
	provider, exists := document.ModelProviders["onellm"]
	current := document.ModelProvider != nil && *document.ModelProvider == "onellm" &&
		document.ModelCatalogJSON != nil && samePath(*document.ModelCatalogJSON, options.OneLLMCatalogPath) && exists &&
		provider.Name != nil && *provider.Name == SourceTag && provider.BaseURL != nil && *provider.BaseURL == options.ExpectedBaseURL &&
		provider.WireAPI != nil && *provider.WireAPI == "responses" && provider.RequiresOpenAIAuth != nil && *provider.RequiresOpenAIAuth
	if current {
		result.ConfigSyncState = "current"
	} else {
		result.ConfigSyncState = "different"
	}
}

func inspectCatalogs(result *Result) {
	oneLLMData, oneLLMState := readFile(result.OneLLMCatalogPath)
	result.OneLLMCatalogExists = oneLLMState != "absent"
	codexData, codexState := readFile(result.CodexCatalogPath)
	result.CodexCatalogExists = codexState != "absent"

	if oneLLMState != "valid" {
		result.CatalogSyncState = oneLLMState
		return
	}
	models, valid := decodeCatalog(oneLLMData)
	if !valid {
		result.CatalogSyncState = "invalid"
		return
	}
	result.ModelCount = len(models)
	providers := make(map[string]struct{})
	for _, model := range models {
		if provider, _, found := strings.Cut(model, "/"); found && provider != "" {
			providers[provider] = struct{}{}
		}
	}
	for provider := range providers {
		result.SourceProviders = append(result.SourceProviders, provider)
	}
	sort.Strings(result.SourceProviders)
	if info, err := os.Stat(result.OneLLMCatalogPath); err == nil {
		generated := info.ModTime().UTC().Format(time.RFC3339)
		result.CatalogGeneratedAt = &generated
	}

	switch codexState {
	case "absent":
		result.CatalogSyncState = "absent"
	case "unreadable":
		result.CatalogSyncState = "unreadable"
	default:
		if _, valid := decodeCatalog(codexData); !valid {
			result.CatalogSyncState = "invalid"
		} else if bytes.Equal(oneLLMData, codexData) {
			result.CatalogSyncState = "current"
		} else {
			result.CatalogSyncState = "different"
		}
	}
}

func readFile(path string) ([]byte, string) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, "absent"
	}
	if err != nil {
		return nil, "unreadable"
	}
	return data, "valid"
}

func decodeCatalog(data []byte) ([]string, bool) {
	var document struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil || document.Models == nil {
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, false
	}
	models := make([]string, len(document.Models))
	for index, model := range document.Models {
		if model.Slug == "" {
			return nil, false
		}
		models[index] = model.Slug
	}
	return models, true
}

func samePath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}
