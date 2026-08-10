package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kkroid/onellm-router/internal/catalog"
	"github.com/kkroid/onellm-router/internal/codexconfig"
	"github.com/kkroid/onellm-router/internal/config"
	"github.com/kkroid/onellm-router/internal/router"
	"github.com/spf13/cobra"
)

type clientEnvelope struct {
	SchemaVersion int                `json:"schema_version"`
	Client        string             `json:"client"`
	Operation     string             `json:"operation"`
	OK            bool               `json:"ok"`
	Errors        []clientError      `json:"errors"`
	Result        codexconfig.Result `json:"result"`
}

type codexCommandOptions struct {
	asJSON        bool
	model         string
	configPath    string
	oneLLMCatalog string
	codexCatalog  string
}

func clientCodexCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "codex", Short: "Inspect Codex configuration and catalogs"}
	cmd.AddCommand(newCodexOperationCmd("status"))
	cmd.AddCommand(newCodexOperationCmd("preview"))
	cmd.AddCommand(newCodexOperationCmd("catalog-apply"))
	return cmd
}

func newCodexOperationCmd(operation string) *cobra.Command {
	options := &codexCommandOptions{}
	cmd := &cobra.Command{
		Use:           operation,
		Short:         "Run Codex " + operation,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodexOperation(cmd, operation, options, args)
		},
	}
	cmd.Flags().BoolVar(&options.asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&options.configPath, "codex-config", "", "Codex config.toml path")
	cmd.Flags().StringVar(&options.oneLLMCatalog, "onellm-catalog", "", "OneLLMRouter catalog path")
	cmd.Flags().StringVar(&options.codexCatalog, "codex-catalog", "", "legacy Codex catalog path")
	if operation == "preview" {
		cmd.Flags().StringVar(&options.model, "model", "", "configured provider/model")
	}
	return cmd
}

func runCodexOperation(cmd *cobra.Command, operation string, options *codexCommandOptions, args []string) error {
	paths, pathErr := resolveCodexPaths(options)
	result := codexconfig.Inspect(paths)
	if !options.asJSON || len(args) != 0 || (operation == "preview" && options.model == "") {
		return writeCodexFailure(cmd, operation, result, "invalid_arguments", "command requires --json, valid flags, and no positional arguments", nil)
	}

	selectedConfigPath, err := filepath.Abs(configPath())
	if err != nil {
		return writeCodexFailure(cmd, operation, result, "router_config_error", "could not resolve Router configuration", nil)
	}
	if pathErr != nil {
		return writeCodexFailure(cmd, operation, result, "unsafe_path", "could not resolve a selected client path", nil)
	}
	loaded, err := config.Load(selectedConfigPath)
	if err != nil {
		return writeCodexFailure(cmd, operation, result, "router_config_error", "could not load Router configuration", &selectedConfigPath)
	}
	paths.ExpectedBaseURL = fmt.Sprintf("http://localhost:%d/openai/v1", loaded.Server.HTTPPort)
	paths.OverwriteCatalog = loaded.Codex.OverwriteCatalog
	result = codexconfig.Inspect(paths)

	switch operation {
	case "preview":
		if !configuredResponsesModel(loaded, paths.OneLLMCatalogPath, options.model) {
			return writeCodexFailure(cmd, operation, result, "invalid_arguments", "model must be a configured namespaced Responses model", nil)
		}
		snippet := codexSnippet(options.model, paths.OneLLMCatalogPath, paths.ExpectedBaseURL)
		result.Snippet = &snippet
	case "catalog-apply":
		generated, generationErr := applyCodexCatalog(cmd.Context(), loaded, paths)
		result.WrittenPaths = append([]string(nil), generated.WrittenPaths...)
		if result.WrittenPaths == nil {
			result.WrittenPaths = []string{}
		}
		if generationErr != nil {
			code := "catalog_generation_failed"
			if strings.Contains(generationErr.Error(), "write ") {
				code = "catalog_write_failed"
			}
			return writeCodexFailure(cmd, operation, result, code, "catalog generation did not complete", nil)
		}
		result = codexconfig.Inspect(paths)
		result.WrittenPaths = append([]string(nil), generated.WrittenPaths...)
	}
	return writeCodexEnvelope(cmd, clientEnvelope{
		SchemaVersion: 1, Client: "codex", Operation: operation, OK: true,
		Errors: []clientError{}, Result: result,
	})
}

func resolveCodexPaths(options *codexCommandOptions) (codexconfig.Options, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return codexconfig.Options{}, err
	}
	selected := []string{options.configPath, options.oneLLMCatalog, options.codexCatalog}
	defaults := []string{
		filepath.Join(userHome, ".codex", "config.toml"),
		filepath.Join(userHome, ".onellm", "model-catalog.json"),
		filepath.Join(userHome, ".codex", "model-catalog.json"),
	}
	for index := range selected {
		if selected[index] == "" {
			selected[index] = defaults[index]
		}
		selected[index], err = filepath.Abs(selected[index])
		if err != nil {
			return codexconfig.Options{}, err
		}
	}
	return codexconfig.Options{
		ConfigPath: selected[0], OneLLMCatalogPath: selected[1], CodexCatalogPath: selected[2],
	}, nil
}

func configuredResponsesModel(cfg *config.Config, catalogPath, selected string) bool {
	prefix, model, found := strings.Cut(selected, "/")
	if !found || prefix == "" || model == "" {
		return false
	}
	for _, provider := range cfg.Providers {
		if provider.Prefix != prefix || provider.ResponsesBaseURL == "" {
			continue
		}
		for _, configured := range provider.Models {
			if configured == model {
				return true
			}
		}
		return codexconfig.CatalogHasModel(catalogPath, selected)
	}
	return false
}

func codexSnippet(model, catalogPath, baseURL string) string {
	return "model = " + tomlString(model) + "\n" +
		"model_provider = \"onellm\"\n" +
		"model_catalog_json = " + tomlString(catalogPath) + "\n\n" +
		"[model_providers.onellm]\n" +
		"name = \"OneLLMRouter\"\n" +
		"base_url = " + tomlString(baseURL) + "\n" +
		"wire_api = \"responses\"\n" +
		"requires_openai_auth = true\n"
}

func tomlString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
		case '\\':
			builder.WriteString(`\\`)
		case '"':
			builder.WriteString(`\"`)
		case '\b':
			builder.WriteString(`\b`)
		case '\t':
			builder.WriteString(`\t`)
		case '\n':
			builder.WriteString(`\n`)
		case '\f':
			builder.WriteString(`\f`)
		case '\r':
			builder.WriteString(`\r`)
		default:
			if character < 0x20 || character == 0x7f {
				fmt.Fprintf(&builder, `\u%04X`, character)
			} else {
				builder.WriteRune(character)
			}
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func applyCodexCatalog(ctx context.Context, cfg *config.Config, options codexconfig.Options) (catalog.GenerateResult, error) {
	proxyClient, err := makeHTTPClient(cfg.Proxy.Socks5)
	if err != nil {
		return catalog.GenerateResult{}, err
	}
	directClient, err := makeHTTPClient("")
	if err != nil {
		return catalog.GenerateResult{}, err
	}
	service := catalog.New(func(provider *router.Provider) *http.Client {
		if provider.ShouldUseProxy() {
			return proxyClient
		}
		return directClient
	})
	service.SetReasoningMappings(codexReasoningMappings(cfg.Codex.Models))
	providers := router.FromConfig(cfg.Providers)
	return service.GenerateCodex(ctx, providers, catalog.GenerateOptions{
		OneLLMPath: options.OneLLMCatalogPath, CodexPath: options.CodexCatalogPath,
		OverwriteCodex: options.OverwriteCatalog,
	})
}

func writeCodexFailure(cmd *cobra.Command, operation string, result codexconfig.Result, code, message string, path *string) error {
	if result.SourceProviders == nil {
		result.SourceProviders = []string{}
	}
	if result.WrittenPaths == nil {
		result.WrittenPaths = []string{}
	}
	if err := writeCodexEnvelope(cmd, clientEnvelope{
		SchemaVersion: 1, Client: "codex", Operation: operation, OK: false,
		Errors: []clientError{{Code: code, Message: message, Path: path}}, Result: result,
	}); err != nil {
		return err
	}
	return errMachineOutput
}

func writeCodexEnvelope(cmd *cobra.Command, envelope clientEnvelope) error {
	sort.Strings(envelope.Result.WrittenPaths)
	return json.NewEncoder(cmd.OutOrStdout()).Encode(envelope)
}
