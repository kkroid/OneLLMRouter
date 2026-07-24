package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kkroid/onellm-router/internal/router"
)

const maxCatalogResponseBytes = 4 << 20

type Model struct {
	ID                       string
	Created                  int64
	OwnedBy                  string
	Endpoint                 router.EndpointType
	DefaultReasoningLevel    string
	SupportedReasoningLevels []ReasoningLevel
	HasReasoningMetadata     bool
	ReasoningConfigured      bool
	CodexMetadata            json.RawMessage
}

type ReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type SourceError struct {
	Provider string
	Err      error
}

type Result struct {
	Models []Model
	Errors []SourceError
}

type Service struct {
	clientFor         func(*router.Provider) *http.Client
	reasoningMappings map[string]ReasoningConfig
}

type ReasoningConfig struct {
	DefaultReasoningLevel    string
	SupportedReasoningLevels []string
}

func New(clientFor func(*router.Provider) *http.Client) *Service {
	return &Service{clientFor: clientFor}
}

func (s *Service) SetReasoningMappings(mappings map[string]ReasoningConfig) {
	s.reasoningMappings = make(map[string]ReasoningConfig, len(mappings))
	for model, config := range mappings {
		config.SupportedReasoningLevels = append([]string(nil), config.SupportedReasoningLevels...)
		s.reasoningMappings[model] = config
	}
}

func (s *Service) List(ctx context.Context, providers []router.Provider, endpoint router.EndpointType) Result {
	result := Result{Models: []Model{}}
	seen := make(map[string]bool)

	for index := range providers {
		provider := &providers[index]
		if !provider.SupportsEndpoint(endpoint) {
			continue
		}

		if len(provider.Models) > 0 {
			for _, modelID := range provider.Models {
				s.appendModel(&result, seen, provider, upstreamModel{ID: modelID, Created: 1}, endpoint)
			}
			continue
		}

		models, err := s.discover(ctx, provider, endpoint)
		if err != nil {
			result.Errors = append(result.Errors, SourceError{Provider: provider.Prefix, Err: err})
			continue
		}
		for _, model := range models {
			s.appendModel(&result, seen, provider, model, endpoint)
		}
	}

	return result
}

func (s *Service) appendModel(result *Result, seen map[string]bool, provider *router.Provider, upstream upstreamModel, endpoint router.EndpointType) {
	modelID := upstream.ID
	if modelID == "" {
		modelID = upstream.Slug
	}
	if modelID == "" {
		return
	}
	fullID := provider.Prefix + "/" + modelID
	if seen[fullID] {
		return
	}
	seen[fullID] = true
	model := Model{
		ID:            fullID,
		Created:       upstream.Created,
		OwnedBy:       provider.Prefix,
		Endpoint:      endpoint,
		CodexMetadata: append(json.RawMessage(nil), upstream.CodexMetadata...),
	}
	if upstream.DefaultReasoningLevel != nil || upstream.SupportedReasoningLevels != nil {
		model.HasReasoningMetadata = true
		if upstream.DefaultReasoningLevel != nil {
			model.DefaultReasoningLevel = *upstream.DefaultReasoningLevel
		}
		if upstream.SupportedReasoningLevels != nil {
			model.SupportedReasoningLevels = append([]ReasoningLevel(nil), (*upstream.SupportedReasoningLevels)...)
		}
	}
	if configured, ok := s.reasoningMappings[modelID]; ok {
		model.HasReasoningMetadata = true
		model.ReasoningConfigured = true
		model.DefaultReasoningLevel = configured.DefaultReasoningLevel
		model.SupportedReasoningLevels = reasoningLevels(configured.SupportedReasoningLevels)
	}
	result.Models = append(result.Models, model)
}

type upstreamModel struct {
	ID                       string            `json:"id"`
	Slug                     string            `json:"slug"`
	Created                  int64             `json:"created"`
	DefaultReasoningLevel    *string           `json:"default_reasoning_level"`
	SupportedReasoningLevels *[]ReasoningLevel `json:"supported_reasoning_levels"`
	CodexMetadata            json.RawMessage   `json:"-"`
}

func (s *Service) discover(ctx context.Context, provider *router.Provider, endpoint router.EndpointType) ([]upstreamModel, error) {
	url, authHeader, authValue := sourceFor(provider, endpoint)
	if url == "" {
		return nil, fmt.Errorf("no model-list URL for %s", endpoint)
	}

	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set(authHeader, authValue)

	client := http.DefaultClient
	if s.clientFor != nil {
		client = s.clientFor(provider)
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request catalog: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxCatalogResponseBytes))
		return nil, fmt.Errorf("catalog status %d", response.StatusCode)
	}

	var payload struct {
		Data   []upstreamModel   `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxCatalogResponseBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	if payload.Models != nil {
		models := make([]upstreamModel, 0, len(payload.Models))
		for _, raw := range payload.Models {
			var model upstreamModel
			if err := json.Unmarshal(raw, &model); err != nil {
				return nil, fmt.Errorf("decode Codex catalog model: %w", err)
			}
			model.CodexMetadata = append(json.RawMessage(nil), raw...)
			models = append(models, model)
		}
		return models, nil
	}
	return payload.Data, nil
}

func sourceFor(provider *router.Provider, endpoint router.EndpointType) (url, authHeader, authValue string) {
	switch endpoint {
	case router.EndpointAnthropic:
		return strings.TrimRight(provider.BaseURL, "/") + "/models", "x-api-key", provider.APIKey
	case router.EndpointOpenAI:
		return strings.TrimRight(provider.OpenAIBaseURL, "/") + "/models", "Authorization", "Bearer " + provider.APIKey
	case router.EndpointResponses:
		return strings.TrimRight(provider.ResponsesBaseURL, "/") + "/v1/models", "Authorization", "Bearer " + provider.APIKey
	default:
		return "", "", ""
	}
}
