package router

import (
	"strings"
	"sync"
)

// Resolver maps model IDs to providers.
type Resolver struct {
	mu        sync.RWMutex
	providers []Provider
}

// NewResolver creates a Resolver from a provider list.
func NewResolver(providers []Provider) *Resolver {
	r := &Resolver{}
	r.Reload(providers)
	return r
}

// Reload rebuilds the model map from a new provider list.
func (r *Resolver) Reload(providers []Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers = providers
}

// ResolveResult holds the resolved provider and stripped model name.
type ResolveResult struct {
	Provider *Provider
	Model    string // canonical model name for API call
}

// Resolve finds the provider for a given full model identifier.
// Supports:
//   - "provider/model" — exact match
//   - "provider/model[1m]" — exact match
//   - "provider" — prefix-only match, first model
//
// Returns nil if no provider matches.
func (r *Resolver) Resolve(fullName string) *ResolveResult {
	return r.ResolveForEndpoint(fullName, "")
}

// ResolveForEndpoint resolves a client-visible model for one upstream protocol.
func (r *Resolver) ResolveForEndpoint(fullName string, endpoint EndpointType) *ResolveResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := range r.providers {
		provider := &r.providers[i]
		if endpoint != "" && !provider.SupportsEndpoint(endpoint) {
			continue
		}
		if fullName == provider.Prefix {
			for _, route := range provider.ConfiguredModelRoutes() {
				if route.SupportsEndpoint(endpoint) {
					return resolvedRoute(provider, route, route.ID)
				}
			}
			return nil
		}
	}

	for i := range r.providers {
		provider := &r.providers[i]
		if endpoint != "" && !provider.SupportsEndpoint(endpoint) {
			continue
		}
		for _, separator := range []string{"/", "-"} {
			prefix := provider.Prefix + separator
			if !strings.HasPrefix(fullName, prefix) {
				continue
			}
			model := strings.TrimPrefix(fullName, prefix)
			if model == "" {
				return nil
			}
			routes := provider.ConfiguredModelRoutes()
			if len(routes) == 0 {
				return &ResolveResult{Provider: provider, Model: model}
			}
			if route, ok := findModelRoute(routes, endpoint, model); ok {
				return resolvedRoute(provider, route, model)
			}
			return nil
		}
	}

	// Bare model name: search configured model lists only.
	for i := range r.providers {
		provider := &r.providers[i]
		if endpoint != "" && !provider.SupportsEndpoint(endpoint) {
			continue
		}
		if route, ok := findModelRoute(provider.ConfiguredModelRoutes(), endpoint, fullName); ok {
			return resolvedRoute(provider, route, fullName)
		}
	}

	return nil
}

func findModelRoute(routes []ModelRoute, endpoint EndpointType, requested string) (ModelRoute, bool) {
	for _, route := range routes {
		if route.SupportsEndpoint(endpoint) && route.ID == requested {
			return route, true
		}
	}
	return ModelRoute{}, false
}

func resolvedRoute(provider *Provider, route ModelRoute, requested string) *ResolveResult {
	upstreamModel := route.UpstreamModel
	if upstreamModel == "" {
		upstreamModel = requested
	}
	return &ResolveResult{Provider: provider, Model: upstreamModel}
}

// AllModelIDs returns canonical model IDs (no aliases).
func (r *Resolver) AllModelIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var ids []string
	for index := range r.providers {
		provider := &r.providers[index]
		for _, route := range provider.ConfiguredModelRoutes() {
			id := provider.Prefix + "/" + route.ID
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// Providers returns a copy of the provider list.
func (r *Resolver) Providers() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := make([]Provider, len(r.providers))
	copy(cp, r.providers)
	return cp
}

// ModelEntry holds a model with its provider metadata, for model list responses.
type ModelEntry struct {
	ID            string         `json:"id"`
	Object        string         `json:"object"`
	Created       int64          `json:"created"`
	OwnedBy       string         `json:"owned_by"`
	EndpointTypes []EndpointType `json:"supported_endpoint_types"`
}
