//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kkroid/onellm-router/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/registry"
)

var errInstallValueNotExist = registry.ErrNotExist

func validatePlatformLifecycle(daemon, trayChild bool) error {
	return nil
}

func detachFromTerminal() error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	freeConsole := kernel32.NewProc("FreeConsole")
	freeConsole.Call()
	os.Stdin.Close()
	return nil
}

type installRegistry interface {
	GetStringValue(string) (string, uint32, error)
	SetStringValue(string, string) error
	DeleteValue(string) error
	Close() error
}

type installProcess interface {
	Kill() error
	Release() error
}

type installDeps struct {
	loadConfig      func(string) (*config.Config, error)
	openRegistry    func() (installRegistry, error)
	isPortListening func(string, int) bool
	startProcess    func(string, []string) (installProcess, error)
	waitForHealth   func(string, int, time.Duration) error
}

type installResult struct {
	CommandLine     string
	RegistryChanged bool
	AlreadyRunning  bool
}

func runInstall(exePath, cfgPath string, deps installDeps) (installResult, error) {
	result := installResult{CommandLine: installCommandLine(exePath, cfgPath)}
	cfg, err := deps.loadConfig(cfgPath)
	if err != nil {
		return result, fmt.Errorf("load config: %w", err)
	}

	registryKey, err := deps.openRegistry()
	if err != nil {
		return result, fmt.Errorf("open registry: %w", err)
	}
	defer registryKey.Close()

	previousValue, _, err := registryKey.GetStringValue("OneLLMRouter")
	previousExists := err == nil
	if err != nil && !errors.Is(err, errInstallValueNotExist) {
		return result, fmt.Errorf("read registry: %w", err)
	}
	if !previousExists || previousValue != result.CommandLine {
		if err := registryKey.SetStringValue("OneLLMRouter", result.CommandLine); err != nil {
			return result, fmt.Errorf("set registry: %w", err)
		}
		result.RegistryChanged = true
	}

	rollback := func() error {
		if !result.RegistryChanged {
			return nil
		}
		if previousExists {
			return registryKey.SetStringValue("OneLLMRouter", previousValue)
		}
		return registryKey.DeleteValue("OneLLMRouter")
	}
	fail := func(prefix string, cause error) (installResult, error) {
		if rollbackErr := rollback(); rollbackErr != nil {
			return result, fmt.Errorf("%s: %w (rollback: %v)", prefix, cause, rollbackErr)
		}
		return result, fmt.Errorf("%s: %w", prefix, cause)
	}

	if deps.isPortListening(cfg.Server.Host, cfg.Server.HTTPPort) {
		if err := deps.waitForHealth(cfg.Server.Host, cfg.Server.HTTPPort, 5*time.Second); err != nil {
			return fail("wait for existing health", err)
		}
		result.AlreadyRunning = true
		return result, nil
	}

	process, err := deps.startProcess(exePath, installDaemonArgs(cfgPath))
	if err != nil {
		return fail("start daemon", err)
	}
	if err := deps.waitForHealth(cfg.Server.Host, cfg.Server.HTTPPort, 5*time.Second); err != nil {
		_ = process.Kill()
		_ = process.Release()
		return fail("wait for health", err)
	}
	if err := process.Release(); err != nil {
		return fail("release daemon", err)
	}
	return result, nil
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
			cfgPath, err := installConfigPath()
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}

			result, err := runInstall(exePath, cfgPath, installDeps{
				loadConfig: config.Load,
				openRegistry: func() (installRegistry, error) {
					return registry.OpenKey(registry.CURRENT_USER,
						`Software\Microsoft\Windows\CurrentVersion\Run`,
						registry.QUERY_VALUE|registry.SET_VALUE)
				},
				isPortListening: isPortListening,
				startProcess: func(executable string, arguments []string) (installProcess, error) {
					process := exec.Command(executable, arguments...)
					if err := process.Start(); err != nil {
						return nil, err
					}
					return process.Process, nil
				},
				waitForHealth: waitForHealth,
			})
			if err != nil {
				return err
			}

			if result.RegistryChanged {
				fmt.Println("✅ 已注册开机启动")
			} else {
				fmt.Println("✅ 已注册开机启动 (无需重复注册)")
			}
			fmt.Printf("   命令: %s\n", result.CommandLine)
			if result.AlreadyRunning {
				fmt.Println("端口已被占用，未重复启动")
				return nil
			}
			fmt.Println("启动完成")
			return nil
		},
	}
}

func installConfigPath() (string, error) {
	return filepath.Abs(configPath())
}

func installDaemonArgs(cfgPath string) []string {
	return []string{"--daemon", "--config", cfgPath}
}

func installCommandLine(exePath, cfgPath string) string {
	return fmt.Sprintf(`"%s" --daemon --config "%s"`, exePath, cfgPath)
}

func isPortListening(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func waitForHealth(host string, port int, timeout time.Duration) error {
	url := "http://" + net.JoinHostPort(host, fmt.Sprintf("%d", port)) + "/health"
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

func uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove auto-start registration",
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := registry.OpenKey(registry.CURRENT_USER,
				`Software\Microsoft\Windows\CurrentVersion\Run`,
				registry.SET_VALUE)
			if err != nil {
				return fmt.Errorf("open registry: %w", err)
			}
			defer key.Close()
			if err := key.DeleteValue("OneLLMRouter"); err != nil {
				return fmt.Errorf("not registered (no registry key found)")
			}
			fmt.Println("✅ 已取消开机启动")
			return nil
		},
	}
}
