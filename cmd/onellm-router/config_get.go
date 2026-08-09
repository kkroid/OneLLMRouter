package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/kkroid/onellm-router/internal/config"
	"github.com/spf13/cobra"
)

func configGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "config-get",
		Short: "Print a secret-safe configuration snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !asJSON {
				return fmt.Errorf("config-get requires --json")
			}
			path, err := filepath.Abs(configPath())
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}
			loaded, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(config.NewSnapshot(loaded))
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
