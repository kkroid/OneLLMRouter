package router

import (
	"github.com/kkroid/onecc-router/internal/config"
)

// Provider represents a configured model provider.
type Provider struct {
	Name     string
	Prefix   string
	BaseURL  string
	APIKey   string
	Models   []string
	UseProxy *bool // nil=inherit global, true=proxy, false=direct
}

// FromConfig converts provider configs from the YAML config to router providers.
func FromConfig(providers []config.ProviderConfig) []Provider {
	result := make([]Provider, 0, len(providers))
	for _, p := range providers {
		result = append(result, Provider{
			Name:     p.Name,
			Prefix:   p.Prefix,
			BaseURL:  p.BaseURL,
			APIKey:   p.APIKey,
			Models:   p.Models,
			UseProxy: p.Proxy,
		})
	}
	return result
}

// ShouldUseProxy returns whether this provider should use the proxy.
func (p *Provider) ShouldUseProxy() bool {
	if p.UseProxy != nil {
		return *p.UseProxy
	}
	return true // default: use proxy (backward compatible)
}
