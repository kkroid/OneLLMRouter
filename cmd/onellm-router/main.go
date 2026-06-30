package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net"
	"os"
	"os/signal"
	"sync"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/kkroid/onellm-router/internal/auth"
	"github.com/kkroid/onellm-router/internal/config"
	onellmLog "github.com/kkroid/onellm-router/internal/log"
	netproxy "golang.org/x/net/proxy"

	"github.com/kkroid/onellm-router/internal/proxy"
	"github.com/kkroid/onellm-router/internal/router"
	"github.com/kkroid/onellm-router/internal/ui"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	daemon   bool
	noPidLock bool
	version string // set via ldflags: -X main.version=1.0.0
)

func init() {
	if version == "" {
		version = "dev"
	}
}

func configPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	exe, err := os.Executable()
	if err != nil {
		return "onellm-router.yaml"
	}
	return filepath.Join(filepath.Dir(exe), "onellm-router.yaml")
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "onellm-router",
		Short: "OneLLMRouter — AI model proxy gateway",
		Long: `OneLLMRouter unifies GitHub Copilot Claude models and arbitrary
Anthropic-compatible APIs behind standard Anthropic + OpenAI API endpoints.`,
		Version: version,
		RunE:    serveCmd().RunE,
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file path (default: onellm-router.yaml next to exe)")
	rootCmd.PersistentFlags().BoolVarP(&daemon, "daemon", "d", false, "run in background")
	rootCmd.PersistentFlags().BoolVar(&noPidLock, "no-pid", false, "allow multiple instances (skip PID lock)")

	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(&cobra.Command{
		Use: "version", Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) { fmt.Println(version) },
	})
	rootCmd.AddCommand(installCmd())
	rootCmd.AddCommand(uninstallCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the proxy daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("配置校验失败: %w", err)
			}

			httpAddr := net.JoinHostPort(cfg.Server.Host, fmt.Sprintf("%d", cfg.Server.HTTPPort))
			if !noPidLock {
				if conn, err := net.DialTimeout("tcp", httpAddr, 500*time.Millisecond); err == nil {
					conn.Close()
					return fmt.Errorf("port %s is in use", httpAddr)
				}
			}

						logCfg := onellmLog.FromConfig(cfg.Log.Level, cfg.Log.Dir, cfg.Log.MaxAgeDays)
			logger, cleanup, err := onellmLog.Setup(logCfg)
			if err != nil {
				return fmt.Errorf("setup logger: %w", err)
			}
			defer cleanup()

			providers := router.FromConfig(cfg.Providers)
			resolver := router.NewResolver(providers)

			proxyAddr := cfg.Proxy.Socks5
			httpClient, err := makeHTTPClient(proxyAddr)
			if err != nil {
				return fmt.Errorf("create http client: %w", err)
			}

			tokenFile := config.DefaultTokenFile()
			tokenMgr, err := auth.NewTokenManager(tokenFile, proxyAddr)
			if err != nil {
				return fmt.Errorf("create token manager: %w", err)
			}

			// Force login if cp configured but no token
			if resolver.CopilotProvider() != nil && !tokenMgr.CheckTokenAvailable() {
				fmt.Fprint(os.Stderr, "\n🔑 Copilot 未授权，请先登录:\n")
				if err := tokenMgr.DeviceLogin(); err != nil {
					return fmt.Errorf("登录失败: %w", err)
				}
				fmt.Fprint(os.Stderr, "✅ 登录成功，启动服务...\n")
			}

			directClient, err := makeHTTPClient("")
			if err != nil {
				return fmt.Errorf("create direct client: %w", err)
			}
			// Bell: beep on error, default on
			bell := cfg.Server.Bell == nil || *cfg.Server.Bell
			ui.SetBell(bell)

			proxyHandler := proxy.NewHandler(resolver, tokenMgr, httpClient, directClient, logger)

			logger.Info("onellm-router starting",
				"version", version,
				"http_port", cfg.Server.HTTPPort,
				"providers", len(providers),
				"models", len(resolver.AllModelIDs()),
			)

			fmt.Fprintf(os.Stderr, "HTTP: http://%s:%d  |  %d providers, %d models\n",
				cfg.Server.Host, cfg.Server.HTTPPort, len(providers), len(resolver.AllModelIDs()))

			for _, p := range providers {
				logger.Info("provider",
					"name", p.Name,
					"prefix", p.Prefix,
					"use_proxy", p.ShouldUseProxy(),
					"has_openai_url", p.OpenAIBaseURL != "",
				)
			}

			printClaudeCodeSettings(cfg)

			if daemon {
				detachFromTerminal()
			}

			mux := http.NewServeMux()
			registerRoutes(mux, resolver, proxyHandler, tokenMgr, cfg, logger)

			httpServer := &http.Server{Addr: httpAddr, Handler: withRequestID(mux, logger)}

			go func() {
				logger.Info("HTTP server listening", "addr", httpAddr)
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("HTTP server error", "error", err)
				}
			}()

			// Shutdown coordination: tray or signal
			doneCh := make(chan struct{})
			once := new(sync.Once)
			stop := func() { once.Do(func() { close(doneCh) }) }

			go ui.NewTray(cfg.Server.HTTPPort, nil, stop).Run()

			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh
				stop()
			}()

			<-doneCh
			logger.Info("shutting down")

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				logger.Error("HTTP shutdown error", "error", err)
			}

			logger.Info("onellm-router stopped")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			reqClient := &http.Client{Timeout: 3 * time.Second}
			url := fmt.Sprintf("http://%s:%d/health", cfg.Server.Host, cfg.Server.HTTPPort)
			resp, err := reqClient.Get(url)
			if err != nil {
				fmt.Println("未运行")
				fmt.Println("启动: onellm-router")
				return nil
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				fmt.Println("运行中:", url)
				modelsURL := fmt.Sprintf("http://%s:%d/v1/models", cfg.Server.Host, cfg.Server.HTTPPort)
				if r2, err := http.Get(modelsURL); err == nil {
					defer r2.Body.Close()
					body := make([]byte, 4096)
					n, _ := r2.Body.Read(body)
					fmt.Printf("Models: %s\n", string(body[:n]))
				}
			}
			return nil
		},
	}
}

func writeModelList(w http.ResponseWriter, resolver *router.Resolver) {
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	var models []modelEntry
	for _, id := range resolver.AllModelIDs() {
		models = append(models, modelEntry{ID: id, Object: "model", Created: 1, OwnedBy: "router"})
	}
	if models == nil {
		models = []modelEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": models})
}

func registerRoutes(mux *http.ServeMux, resolver *router.Resolver, proxyHandler *proxy.Handler, tokenMgr *auth.TokenManager, cfg *config.Config, logger *slog.Logger) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("OneLLM Proxy — OK"))
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		health := map[string]interface{}{
			"status":        "ok",
			"models":        len(resolver.AllModelIDs()),
			"copilot_token": tokenMgr.CheckTokenAvailable(),
			"http_port":     cfg.Server.HTTPPort,
			"version":       version,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(health)
	})

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeModelList(w, resolver)
	})

	mux.HandleFunc("/anthropic/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeModelList(w, resolver)
	})

	mux.HandleFunc("/openai/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeModelList(w, resolver)
	})

	// Anthropic endpoints
	anthropicH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHandler.ServeHTTP(w, r)
	})
	mux.Handle("/anthropic/v1/messages", withPanicRecover(anthropicH, logger))
	mux.Handle("/v1/messages", withPanicRecover(anthropicH, logger))
	mux.Handle("/messages", withPanicRecover(anthropicH, logger))

	// OpenAI endpoints
	openaiH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHandler.ServeOpenAI(w, r)
	})
	mux.Handle("/openai/v1/chat/completions", withPanicRecover(openaiH, logger))
	mux.Handle("/openai/chat/completions", withPanicRecover(openaiH, logger)) // some tools omit /v1
	}

func printClaudeCodeSettings(cfg *config.Config) {
	slots := cfg.ModelSlots
	settings := map[string]interface{}{
		"env": map[string]string{
			"ANTHROPIC_BASE_URL":             fmt.Sprintf("http://localhost:%d/anthropic", cfg.Server.HTTPPort),
			"ANTHROPIC_AUTH_TOKEN":           "x",
			"ANTHROPIC_MODEL":                slots.Default,
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   slots.Opus,
			"ANTHROPIC_DEFAULT_SONNET_MODEL": slots.Sonnet,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  slots.Haiku,
			"ANTHROPIC_DEFAULT_FABLE_MODEL":  slots.Fable,
		},
		"theme":                    "dark",
		"skipWorkflowUsageWarning": true,
	}

	out, _ := json.MarshalIndent(settings, "", "  ")
	fmt.Println()
	fmt.Println(string(out))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func withRequestID(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := onellmLog.WithRequestID(r.Context())
		meta := &onellmLog.RequestMeta{}
		meta.MarkStart()
		ctx = onellmLog.WithRequestMeta(ctx, meta)
		requestID := onellmLog.RequestIDFromContext(ctx)

		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r.WithContext(ctx))

		meta = onellmLog.RequestMetaFromContext(ctx)
		attrs := []any{
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if meta.Model != "" {
			attrs = append(attrs, "model", meta.Model, "provider", meta.Provider, "stream", meta.Stream)
		}
		if meta.MaxTokens > 0 {
			attrs = append(attrs, "max_tokens", meta.MaxTokens)
		}
		if meta.TTFBMs > 0 {
			attrs = append(attrs, "ttfb_ms", meta.TTFBMs)
		}
		if meta.Error != "" {
			attrs = append(attrs, "error", meta.Error)
		}
		logger.Info("request", attrs...)
	})
}

func withPanicRecover(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error("panic in handler",
					"request_id", onellmLog.RequestIDFromContext(r.Context()),
					"error", fmt.Sprintf("%v", err),
				)
				http.Error(w, `{"error":{"type":"internal","message":"internal server error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func detachFromTerminal() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	freeConsole := kernel32.NewProc("FreeConsole")
	freeConsole.Call()
	os.Stdin.Close()
}

func makeHTTPClient(proxyAddr string) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	if proxyAddr != "" {
		dialer, err := netproxy.SOCKS5("tcp", proxyAddr, nil, netproxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", err)
		}
		transport.DialContext = dialer.(netproxy.ContextDialer).DialContext
	}
	return &http.Client{Transport: transport}, nil
}

func installCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Register auto-start with Windows",
		RunE: func(cmd *cobra.Command, args []string) error {
			exePath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("get exe path: %w", err)
			}
			k, err := registry.OpenKey(registry.CURRENT_USER,
				`Software\Microsoft\Windows\CurrentVersion\Run`,
				registry.QUERY_VALUE|registry.SET_VALUE)
			if err != nil {
				return fmt.Errorf("open registry: %w", err)
			}
			defer k.Close()
			cmdLine := fmt.Sprintf(`"%s" --daemon`, exePath)

			// Check if already registered
			existing, _, _ := k.GetStringValue("OneLLMRouter")
			if existing == cmdLine {
				fmt.Println("✅ 已注册开机启动 (无需重复注册)")
			} else {
				if err := k.SetStringValue("OneLLMRouter", cmdLine); err != nil {
					return fmt.Errorf("set registry: %w", err)
				}
				fmt.Println("✅ 已注册开机启动")
			}
			fmt.Printf("   命令: %s\n", cmdLine)

			// Auto-start immediately
			fmt.Print("正在启动... ")
			cfgPath := configPath()
			if err := exec.Command(exePath, "--daemon", "--config", cfgPath).Start(); err != nil {
				fmt.Printf("失败: %v\n", err)
			} else {
				fmt.Println("完成")
			}
			return nil
		},
	}
}

func uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove auto-start registration",
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := registry.OpenKey(registry.CURRENT_USER,
				`Software\Microsoft\Windows\CurrentVersion\Run`,
				registry.SET_VALUE)
			if err != nil {
				return fmt.Errorf("open registry: %w", err)
			}
			defer k.Close()
			if err := k.DeleteValue("OneLLMRouter"); err != nil {
				return fmt.Errorf("not registered (no registry key found)")
			}
			fmt.Println("✅ 已取消开机启动")
			return nil
		},
	}
}
