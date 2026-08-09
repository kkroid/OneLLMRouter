package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/kkroid/onellm-router/internal/config"
	"github.com/spf13/cobra"
)

type configValidationResult struct {
	Valid  bool                `json:"valid"`
	Errors []config.FieldError `json:"errors"`
}

func configValidateCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "config-validate",
		Short: "Validate a configuration snapshot from stdin",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !asJSON {
				return fmt.Errorf("config-validate requires --json")
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
				return writeValidationResult(cmd, []config.FieldError{{Field: "$", Message: err.Error()}})
			}
			_, validationErrors := snapshot.Resolve(existing)
			return writeValidationResult(cmd, validationErrors)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON validation result")
	return cmd
}

func writeValidationResult(cmd *cobra.Command, validationErrors []config.FieldError) error {
	if validationErrors == nil {
		validationErrors = []config.FieldError{}
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(configValidationResult{
		Valid: len(validationErrors) == 0, Errors: validationErrors,
	})
}
