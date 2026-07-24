package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kkroid/onellm-router/internal/router"
)

type GenerateOptions struct {
	OneLLMPath     string
	CodexPath      string
	OverwriteCodex bool
}

type GenerateResult struct {
	ModelCount   int
	SourceErrors []SourceError
	WrittenPaths []string
}

func (s *Service) GenerateCodex(ctx context.Context, providers []router.Provider, options GenerateOptions) (GenerateResult, error) {
	result := GenerateResult{}
	if options.OneLLMPath == "" {
		return result, fmt.Errorf("OneLLM catalog path is empty")
	}
	if options.OverwriteCodex && options.CodexPath == "" {
		return result, fmt.Errorf("Codex catalog path is empty")
	}

	discovery := s.List(ctx, providers, router.EndpointResponses)
	result.ModelCount = len(discovery.Models)
	result.SourceErrors = discovery.Errors
	if len(discovery.Errors) > 0 {
		return result, fmt.Errorf("incomplete Responses model discovery: %d source(s) failed", len(discovery.Errors))
	}
	if len(discovery.Models) == 0 {
		return result, fmt.Errorf("no Responses models available")
	}

	data, err := MarshalCodex(discovery.Models)
	if err != nil {
		return result, fmt.Errorf("encode Codex catalog: %w", err)
	}
	if err := writeFileAtomic(options.OneLLMPath, data); err != nil {
		return result, fmt.Errorf("write OneLLM catalog: %w", err)
	}
	result.WrittenPaths = append(result.WrittenPaths, options.OneLLMPath)

	if options.OverwriteCodex {
		if err := writeFileAtomic(options.CodexPath, data); err != nil {
			return result, fmt.Errorf("write Codex catalog: %w", err)
		}
		result.WrittenPaths = append(result.WrittenPaths, options.CodexPath)
	}
	return result, nil
}

func writeFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
