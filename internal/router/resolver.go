package router

import (
	"strings"
	"sync"
)

// Resolver maps model IDs to providers.
type Resolver struct {
	mu        sync.RWMutex
	providers []Provider
	modelMap  map[string]*Provider // "cp/claude-opus-4.8" → Provider
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
	r.modelMap = make(map[string]*Provider, len(providers)*4)

	for i := range r.providers {
		p := &r.providers[i]
		for _, m := range p.Models {
			r.modelMap[p.Prefix+"/"+m] = p
			// Also register alias without [1m] suffix (Claude Code strips it)
			if strings.HasSuffix(m, "[1m]") {
				alias := strings.TrimSuffix(m, "[1m]")
				r.modelMap[p.Prefix+"/"+alias] = p
				r.modelMap[p.Prefix+"/"+m] = p // keep original as canonical
			}
		}
	}
}

// ResolveResult holds the resolved provider and stripped model name.
type ResolveResult struct {
	Provider *Provider
	Model    string // canonical model name for API call
}

// Resolve finds the provider for a given full model identifier.
// Supports:
//   - "cp/claude-opus-4.8" — exact match
//   - "cp/claude-opus-4.8[1m]" — exact match (canonical)
//   - "cp" — prefix-only match, first model
//   - Auto-maps [1m]-less names to canonical [1m] names
//
// Returns nil if no provider matches.
func (r *Resolver) Resolve(fullName string) *ResolveResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Exact match
	if p, ok := r.modelMap[fullName]; ok {
		model := canonicalModelName(p, fullName)
		return &ResolveResult{Provider: p, Model: model}
	}

	// Prefix-only match: "cp" → first model
	for i := range r.providers {
		if fullName == r.providers[i].Prefix && len(r.providers[i].Models) > 0 {
			return &ResolveResult{
				Provider: &r.providers[i],
				Model:    r.providers[i].Models[0],
			}
		}
	}

	for i := range r.providers {
		provider := &r.providers[i]
		for _, separator := range []string{"/", "-"} {
			prefix := provider.Prefix + separator
			if !strings.HasPrefix(fullName, prefix) {
				continue
			}
			model := strings.TrimPrefix(fullName, prefix)
			if model == "" {
				return nil
			}
			if len(provider.Models) == 0 || configuredModel(provider.Models, model) {
				return &ResolveResult{Provider: provider, Model: model}
			}
			return nil
		}
	}

	// Bare model name: search configured model lists only.
	for i := range r.providers {
		for _, m := range r.providers[i].Models {
			if m == fullName || strings.TrimSuffix(m, "[1m]") == fullName {
				return &ResolveResult{
					Provider: &r.providers[i],
					Model:    fullName,
				}
			}
		}
	}

	return nil
}

func configuredModel(models []string, requested string) bool {
	for _, model := range models {
		if model == requested || strings.TrimSuffix(model, "[1m]") == requested {
			return true
		}
	}
	return false
}

// canonicalModelName returns the model name to use for the API call.
// Passes through the requested name as-is (transparent proxy).
func canonicalModelName(p *Provider, fullName string) string {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return fullName
}

// AllModelIDs returns canonical model IDs (no aliases).
func (r *Resolver) AllModelIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var ids []string
	for _, p := range r.providers {
		for _, m := range p.Models {
			id := p.Prefix + "/" + m
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// CopilotProvider returns the "cp" provider, or nil if not configured.
func (r *Resolver) CopilotProvider() *Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := range r.providers {
		if r.providers[i].Prefix == "cp" {
			return &r.providers[i]
		}
	}
	return nil
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
