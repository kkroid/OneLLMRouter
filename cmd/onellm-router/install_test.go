package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/kkroid/onellm-router/internal/config"
)

type fakeInstallRegistry struct {
	value    string
	exists   bool
	setCalls []string
	deletes  int
}

func (fake *fakeInstallRegistry) GetStringValue(string) (string, uint32, error) {
	if !fake.exists {
		return "", 0, registry.ErrNotExist
	}
	return fake.value, 0, nil
}

func (fake *fakeInstallRegistry) SetStringValue(_ string, value string) error {
	fake.value = value
	fake.exists = true
	fake.setCalls = append(fake.setCalls, value)
	return nil
}

func (fake *fakeInstallRegistry) DeleteValue(string) error {
	fake.value = ""
	fake.exists = false
	fake.deletes++
	return nil
}

func (fake *fakeInstallRegistry) Close() error { return nil }

type fakeInstallProcess struct {
	killed   bool
	released bool
}

func (fake *fakeInstallProcess) Kill() error {
	fake.killed = true
	return nil
}

func (fake *fakeInstallProcess) Release() error {
	fake.released = true
	return nil
}

func TestInstallArgsIncludeConfigPath(t *testing.T) {
	args := installDaemonArgs(`C:\Users\kkroid\onellm-router.yaml`)

	want := []string{"--daemon", "--config", `C:\Users\kkroid\onellm-router.yaml`}
	if len(args) != len(want) {
		t.Fatalf("arg count mismatch: got %#v want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d mismatch: got %q want %q", i, args[i], want[i])
		}
	}
}

func TestInstallCommandLineQuotesConfigPath(t *testing.T) {
	cmdLine := installCommandLine(`C:\Program Files\OneLLM\onellm-router.exe`, `C:\Users\kkroid\My Configs\onellm-router.yaml`)

	want := `"C:\Program Files\OneLLM\onellm-router.exe" --daemon --config "C:\Users\kkroid\My Configs\onellm-router.yaml"`
	if cmdLine != want {
		t.Fatalf("command line mismatch:\ngot:  %s\nwant: %s", cmdLine, want)
	}
}

func TestInstallConfigPathMakesConfigAbsolute(t *testing.T) {
	oldCfgFile := cfgFile
	cfgFile = `.\onellm-router.yaml`
	defer func() { cfgFile = oldCfgFile }()

	got, err := installConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute config path, got %q", got)
	}
	if filepath.Base(got) != "onellm-router.yaml" {
		t.Fatalf("unexpected config path: %q", got)
	}
}

func TestRunInstallValidatesBeforeRegistryWrite(t *testing.T) {
	openedRegistry := false
	_, err := runInstall(`C:\onellm-router.exe`, `C:\broken.yaml`, installDeps{
		loadConfig: func(string) (*config.Config, error) {
			return nil, errors.New("invalid config")
		},
		openRegistry: func() (installRegistry, error) {
			openedRegistry = true
			return &fakeInstallRegistry{}, nil
		},
	})

	if err == nil || err.Error() != "load config: invalid config" {
		t.Fatalf("error = %v", err)
	}
	if openedRegistry {
		t.Fatal("registry opened before configuration validation")
	}
}

func TestRunInstallRollsBackNewRegistryValueAfterStartFailure(t *testing.T) {
	fakeRegistry := &fakeInstallRegistry{}
	_, err := runInstall(`C:\onellm-router.exe`, `C:\config.yaml`, installDeps{
		loadConfig: func(string) (*config.Config, error) {
			return &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", HTTPPort: 4567}}, nil
		},
		openRegistry:    func() (installRegistry, error) { return fakeRegistry, nil },
		isPortListening: func(string, int) bool { return false },
		startProcess: func(string, []string) (installProcess, error) {
			return nil, errors.New("start failed")
		},
	})

	if err == nil || err.Error() != "start daemon: start failed" {
		t.Fatalf("error = %v", err)
	}
	if fakeRegistry.deletes != 1 || fakeRegistry.exists {
		t.Fatalf("new registry value was not removed: %+v", fakeRegistry)
	}
}

func TestRunInstallRestoresPreviousValueAfterHealthFailure(t *testing.T) {
	fakeRegistry := &fakeInstallRegistry{value: "old-command", exists: true}
	fakeProcess := &fakeInstallProcess{}
	_, err := runInstall(`C:\onellm-router.exe`, `C:\config.yaml`, installDeps{
		loadConfig: func(string) (*config.Config, error) {
			return &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", HTTPPort: 4567}}, nil
		},
		openRegistry:    func() (installRegistry, error) { return fakeRegistry, nil },
		isPortListening: func(string, int) bool { return false },
		startProcess: func(string, []string) (installProcess, error) {
			return fakeProcess, nil
		},
		waitForHealth: func(string, int, time.Duration) error {
			return errors.New("health failed")
		},
	})

	if err == nil || err.Error() != "wait for health: health failed" {
		t.Fatalf("error = %v", err)
	}
	wantCalls := []string{installCommandLine(`C:\onellm-router.exe`, `C:\config.yaml`), "old-command"}
	if len(fakeRegistry.setCalls) != len(wantCalls) {
		t.Fatalf("set calls = %#v, want %#v", fakeRegistry.setCalls, wantCalls)
	}
	for index := range wantCalls {
		if fakeRegistry.setCalls[index] != wantCalls[index] {
			t.Fatalf("set calls = %#v, want %#v", fakeRegistry.setCalls, wantCalls)
		}
	}
	if !fakeProcess.killed || !fakeProcess.released {
		t.Fatalf("failed process was not cleaned up: %+v", fakeProcess)
	}
}
