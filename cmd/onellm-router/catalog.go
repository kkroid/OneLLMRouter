package main

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/kkroid/onellm-router/internal/catalog"
	"github.com/kkroid/onellm-router/internal/config"
	"github.com/kkroid/onellm-router/internal/router"
)

func codexReasoningMappings(models map[string]config.CodexModelConfig) map[string]catalog.ReasoningConfig {
	mappings := make(map[string]catalog.ReasoningConfig, len(models))
	for model, configured := range models {
		mappings[model] = catalog.ReasoningConfig{
			DefaultReasoningLevel:    configured.DefaultReasoningLevel,
			SupportedReasoningLevels: append([]string(nil), configured.SupportedReasoningLevels...),
		}
	}
	return mappings
}

func codexCatalogOptions(userHome string, overwrite bool) catalog.GenerateOptions {
	return catalog.GenerateOptions{
		OneLLMPath:     filepath.Join(userHome, ".onellm", "model-catalog.json"),
		CodexPath:      filepath.Join(userHome, ".codex", "model-catalog.json"),
		OverwriteCodex: overwrite,
	}
}

func generateCodexCatalog(ctx context.Context, service *catalog.Service, providers []router.Provider, options catalog.GenerateOptions, logger *slog.Logger) {
	result, err := service.GenerateCodex(ctx, providers, options)
	for _, sourceErr := range result.SourceErrors {
		logger.Warn("Codex catalog source failed", "provider", sourceErr.Provider, "error", sourceErr.Err)
	}
	if err != nil {
		logger.Error("Codex catalog generation failed", "error", err)
		return
	}
	logger.Info("Codex catalog generated", "models", result.ModelCount, "paths", result.WrittenPaths)
}
