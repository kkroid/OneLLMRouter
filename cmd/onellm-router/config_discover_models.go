package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kkroid/onellm-router/internal/catalog"
	"github.com/kkroid/onellm-router/internal/config"
	"github.com/kkroid/onellm-router/internal/router"
	"github.com/spf13/cobra"
)

type modelDiscoveryResult struct {
	OK       bool     `json:"ok"`
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
	Error    string   `json:"error,omitempty"`
}

func configDiscoverModelsCmd() *cobra.Command {
	var providerIndex int
	var protocol string
	var stdinJSON bool
	cmd := &cobra.Command{
		Use:   "config-discover-models",
		Short: "Discover models using a secret-preserving configuration snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stdinJSON {
				return fmt.Errorf("config-discover-models requires --stdin-json")
			}
			endpoint, validProtocol := discoveryEndpoint(protocol)
			if !validProtocol {
				return writeModelDiscoveryResult(cmd, modelDiscoveryResult{Error: "Unsupported model protocol"})
			}
			path, err := filepath.Abs(configPath())
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}
			existing, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			snapshot, err := config.DecodeSnapshot(cmd.InOrStdin())
			if err != nil {
				return writeModelDiscoveryResult(cmd, modelDiscoveryResult{Error: "Invalid configuration snapshot"})
			}
			resolved, validationErrors := snapshot.Resolve(existing)
			if len(validationErrors) != 0 {
				return writeModelDiscoveryResult(cmd, modelDiscoveryResult{
					Error: "Configuration is invalid: " + validationErrors[0].Field,
				})
			}
			if providerIndex < 0 || providerIndex >= len(resolved.Providers) {
				return writeModelDiscoveryResult(cmd, modelDiscoveryResult{Error: "Provider is no longer available"})
			}

			provider := router.FromConfig(resolved.Providers)[providerIndex]
			if !provider.SupportsEndpoint(endpoint) {
				return writeModelDiscoveryResult(cmd, modelDiscoveryResult{
					Provider: provider.Prefix,
					Error:    "The selected protocol has no Base URL",
				})
			}
			models, err := discoverProviderModels(cmd.Context(), resolved, provider, endpoint)
			if err != nil {
				message := "Model discovery failed"
				if strings.Contains(err.Error(), "catalog status 401") ||
					strings.Contains(err.Error(), "catalog status 403") {
					message = "Provider authentication failed"
				}
				return writeModelDiscoveryResult(cmd, modelDiscoveryResult{
					Provider: provider.Prefix,
					Error:    message,
				})
			}
			return writeModelDiscoveryResult(cmd, modelDiscoveryResult{
				OK: true, Provider: provider.Prefix, Models: models,
			})
		},
	}
	cmd.Flags().BoolVar(&stdinJSON, "stdin-json", false, "read JSON configuration from stdin")
	cmd.Flags().IntVar(&providerIndex, "provider-index", -1, "provider index in the configuration snapshot")
	cmd.Flags().StringVar(&protocol, "protocol", "", "model protocol: anthropic, openai, or responses")
	return cmd
}

func discoveryEndpoint(protocol string) (router.EndpointType, bool) {
	switch protocol {
	case string(router.EndpointAnthropic):
		return router.EndpointAnthropic, true
	case string(router.EndpointOpenAI):
		return router.EndpointOpenAI, true
	case string(router.EndpointResponses):
		return router.EndpointResponses, true
	default:
		return "", false
	}
}

func discoverProviderModels(ctx context.Context, cfg *config.Config, provider router.Provider, endpoint router.EndpointType) ([]string, error) {
	proxyClient, err := makeHTTPClient(cfg.Proxy.Socks5)
	if err != nil {
		return nil, err
	}
	directClient, err := makeHTTPClient("")
	if err != nil {
		return nil, err
	}
	service := catalog.New(func(selected *router.Provider) *http.Client {
		if selected.ShouldUseProxy() {
			return proxyClient
		}
		return directClient
	})
	provider.ModelRoutes = nil
	result := service.List(ctx, []router.Provider{provider}, endpoint)
	if len(result.Errors) != 0 {
		return nil, result.Errors[0].Err
	}
	prefix := provider.Prefix + "/"
	models := make([]string, 0, len(result.Models))
	for _, model := range result.Models {
		models = append(models, strings.TrimPrefix(model.ID, prefix))
	}
	sort.Strings(models)
	return models, nil
}

func writeModelDiscoveryResult(cmd *cobra.Command, result modelDiscoveryResult) error {
	if result.Models == nil {
		result.Models = []string{}
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
}
