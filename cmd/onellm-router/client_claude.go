package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kkroid/onellm-router/internal/claudeconfig"
	"github.com/kkroid/onellm-router/internal/config"
	"github.com/spf13/cobra"
)

type clientError struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Path    *string `json:"path"`
}

type claudeEnvelope struct {
	SchemaVersion int                 `json:"schema_version"`
	Client        string              `json:"client"`
	Operation     string              `json:"operation"`
	OK            bool                `json:"ok"`
	Errors        []clientError       `json:"errors"`
	Result        claudeconfig.Result `json:"result"`
}

var errMachineOutput = errors.New("machine output reported failure")

func clientCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "client", Short: "Inspect and configure supported clients"}
	cmd.AddCommand(claudeClientCmd())
	return cmd
}

func claudeClientCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "claude", Short: "Manage Claude Code integration"}
	cmd.AddCommand(claudeOperationCmd("status"), claudeOperationCmd("apply"), claudeOperationCmd("restore"))
	return cmd
}

func claudeOperationCmd(operation string) *cobra.Command {
	var asJSON bool
	var settingsPath string
	cmd := &cobra.Command{
		Use:   operation,
		Short: map[string]string{"status": "Inspect Claude Code settings", "apply": "Apply managed Claude Code settings", "restore": "Restore the Claude Code settings backup"}[operation],
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return writeClaudeFailure(cmd, operation, claudeconfig.Result{}, "invalid_arguments", "unexpected positional arguments", "")
			}
			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !asJSON {
				return writeClaudeFailure(cmd, operation, claudeconfig.Result{}, "invalid_arguments", operation+" requires --json", "")
			}
			path, err := resolveClaudeSettingsPath(settingsPath)
			if err != nil {
				return writeClaudeFailure(cmd, operation, claudeconfig.Result{}, "unsafe_path", "Claude settings path could not be resolved safely", settingsPath)
			}
			cfgPath, err := filepath.Abs(configPath())
			if err != nil {
				return writeClaudeFailure(cmd, operation, claudeconfig.Result{}, "router_config_error", "Router configuration path could not be resolved", "")
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return writeClaudeFailure(cmd, operation, claudeconfig.Result{}, "router_config_error", "Router configuration could not be loaded", cfgPath)
			}
			if err := cfg.Validate(); err != nil {
				return writeClaudeFailure(cmd, operation, claudeconfig.Result{}, "router_config_error", "Router configuration is invalid", cfgPath)
			}
			values := claudeValues(cfg)
			var result claudeconfig.Result
			var failure *claudeconfig.Failure
			switch operation {
			case "status":
				result, failure = claudeconfig.Status(path, values)
			case "apply":
				result, failure = claudeconfig.Apply(path, values)
			case "restore":
				result, failure = claudeconfig.Restore(path, values)
			}
			if failure != nil {
				return writeClaudeFailure(cmd, operation, result, failure.Code, failure.Message, failure.Path)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(claudeEnvelope{
				SchemaVersion: 1, Client: "claude", Operation: operation, OK: true,
				Errors: []clientError{}, Result: result,
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&settingsPath, "settings", "", "Claude Code settings path")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return writeClaudeFailure(cmd, operation, claudeconfig.Result{}, "invalid_arguments", "command flags are invalid", "")
	})
	return cmd
}

func resolveClaudeSettingsPath(selected string) (string, error) {
	if selected == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		selected = filepath.Join(home, ".claude", "settings.json")
	}
	return filepath.Abs(selected)
}

func claudeValues(cfg *config.Config) claudeconfig.Values {
	return claudeconfig.Values{
		BaseURL: fmt.Sprintf("http://localhost:%d/anthropic", cfg.Server.HTTPPort),
		Default: cfg.ModelSlots.Default, Opus: cfg.ModelSlots.Opus, Sonnet: cfg.ModelSlots.Sonnet,
		Haiku: cfg.ModelSlots.Haiku, Fable: cfg.ModelSlots.Fable,
	}
}

func writeClaudeFailure(cmd *cobra.Command, operation string, result claudeconfig.Result, code, message, path string) error {
	var errorPath *string
	if path != "" {
		errorPath = &path
	}
	_ = json.NewEncoder(cmd.OutOrStdout()).Encode(claudeEnvelope{
		SchemaVersion: 1, Client: "claude", Operation: operation, OK: false,
		Errors: []clientError{{Code: code, Message: message, Path: errorPath}}, Result: result,
	})
	return errMachineOutput
}
