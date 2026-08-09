package main

import (
	"fmt"
	"path/filepath"

	"github.com/kkroid/onellm-router/internal/config"
	"github.com/spf13/cobra"
)

func configApplyCmd() *cobra.Command {
	var stdinJSON bool
	cmd := &cobra.Command{
		Use:   "config-apply",
		Short: "Validate and atomically apply a configuration snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stdinJSON {
				return fmt.Errorf("config-apply requires --stdin-json")
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
			proposed, validationErrors := snapshot.Resolve(existing)
			if len(validationErrors) != 0 {
				return writeValidationResult(cmd, validationErrors)
			}
			if err := config.Apply(path, proposed); err != nil {
				return fmt.Errorf("apply config: %w", err)
			}
			return writeValidationResult(cmd, nil)
		},
	}
	cmd.Flags().BoolVar(&stdinJSON, "stdin-json", false, "read JSON configuration from stdin")
	return cmd
}
