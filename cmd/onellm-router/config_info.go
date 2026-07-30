package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kkroid/onellm-router/internal/config"
	"github.com/spf13/cobra"
)

type configInfo struct {
	Service           string `json:"service"`
	ConfigPath        string `json:"config_path"`
	Host              string `json:"host"`
	HTTPPort          int    `json:"http_port"`
	LogDir            string `json:"log_dir"`
	ProxySOCKS5       string `json:"proxy_socks5"`
	Bell              bool   `json:"bell"`
	OneLLMCatalogPath string `json:"onellm_catalog_path"`
	CodexCatalogPath  string `json:"codex_catalog_path"`
}

func buildConfigInfo(cfg *config.Config, path, home string) configInfo {
	bell := cfg.Server.Bell == nil || *cfg.Server.Bell
	options := codexCatalogOptions(home, cfg.Codex.OverwriteCatalog)
	return configInfo{
		Service:           "onellm-router",
		ConfigPath:        path,
		Host:              cfg.Server.Host,
		HTTPPort:          cfg.Server.HTTPPort,
		LogDir:            cfg.Log.Dir,
		ProxySOCKS5:       cfg.Proxy.Socks5,
		Bell:              bell,
		OneLLMCatalogPath: options.OneLLMPath,
		CodexCatalogPath:  options.CodexPath,
	}
}

func configInfoCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "config-info",
		Short: "Validate config and print non-secret runtime settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !asJSON {
				return fmt.Errorf("config-info currently requires --json")
			}
			path, err := filepath.Abs(configPath())
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("validate config: %w", err)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve user home: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(buildConfigInfo(cfg, path, home))
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
