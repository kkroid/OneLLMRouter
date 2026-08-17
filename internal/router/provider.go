package router

import (
	"github.com/kkroid/onellm-router/internal/config"
)

// Provider represents a configured model provider.
type Provider struct {
	Name             string
	Prefix           string
	BaseURL          string
	OpenAIBaseURL    string
	ResponsesBaseURL string
	APIKey           string
	Models           []string
	ModelRoutes      []ModelRoute
	UseProxy         *bool // nil=inherit global, true=proxy, false=direct
}

type ModelRoute struct {
	ID            string
	Endpoints     []EndpointType
	UpstreamModel string
}

// FromConfig converts provider configs from the YAML config to router providers.
func FromConfig(providers []config.ProviderConfig) []Provider {
	result := make([]Provider, 0, len(providers))
	for _, p := range providers {
		configuredModels := p.ModelConfigs
		modelRoutes := make([]ModelRoute, len(configuredModels))
		for index, model := range configuredModels {
			endpoints := make([]EndpointType, len(model.Endpoints))
			for endpointIndex, endpoint := range model.Endpoints {
				endpoints[endpointIndex] = EndpointType(endpoint)
			}
			modelRoutes[index] = ModelRoute{
				ID: model.ID, Endpoints: endpoints, UpstreamModel: model.UpstreamModel,
			}
		}
		result = append(result, Provider{
			Name:             p.Name,
			Prefix:           p.Prefix,
			BaseURL:          p.BaseURL,
			OpenAIBaseURL:    p.OpenAIBaseURL,
			ResponsesBaseURL: p.ResponsesBaseURL,
			APIKey:           p.APIKey,
			ModelRoutes:      modelRoutes,
			UseProxy:         p.Proxy,
		})
	}
	return result
}

func (p *Provider) ConfiguredModelRoutes() []ModelRoute {
	if len(p.ModelRoutes) != 0 {
		return p.ModelRoutes
	}
	routes := make([]ModelRoute, len(p.Models))
	for index, model := range p.Models {
		routes[index] = ModelRoute{ID: model}
	}
	return routes
}

func (m ModelRoute) SupportsEndpoint(endpoint EndpointType) bool {
	if endpoint == "" || len(m.Endpoints) == 0 {
		return true
	}
	for _, configured := range m.Endpoints {
		if configured == endpoint {
			return true
		}
	}
	return false
}

// ShouldUseProxy returns whether this provider should use the proxy.
func (p *Provider) ShouldUseProxy() bool {
	if p.UseProxy != nil {
		return *p.UseProxy
	}
	return true // default: use proxy (backward compatible)
}

// EndpointType describes a supported API protocol.
type EndpointType string

const (
	EndpointAnthropic EndpointType = "anthropic"
	EndpointOpenAI    EndpointType = "openai"
	EndpointResponses EndpointType = "responses"
)

// EndpointTypes returns which API protocols this provider supports.
// "anthropic" if base_url is set, "openai" if openai_base_url is set,
// and "responses" if responses_base_url is set.
func (p *Provider) EndpointTypes() []EndpointType {
	var types []EndpointType
	if p.SupportsEndpoint(EndpointAnthropic) {
		types = append(types, EndpointAnthropic)
	}
	if p.SupportsEndpoint(EndpointOpenAI) {
		types = append(types, EndpointOpenAI)
	}
	if p.SupportsEndpoint(EndpointResponses) {
		types = append(types, EndpointResponses)
	}
	return types
}

func (p *Provider) SupportsEndpoint(endpoint EndpointType) bool {
	switch endpoint {
	case EndpointAnthropic:
		return p.BaseURL != ""
	case EndpointOpenAI:
		return p.OpenAIBaseURL != ""
	case EndpointResponses:
		return p.ResponsesBaseURL != ""
	default:
		return false
	}
}
