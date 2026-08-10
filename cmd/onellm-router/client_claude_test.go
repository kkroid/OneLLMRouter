package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeClaudeCommandConfig(t *testing.T, secret string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "router.yaml")
	data := fmt.Sprintf(`server:
  host: 127.0.0.1
  http_port: 45678
providers:
  - prefix: alpha
    base_url: https://example.invalid/anthropic
    api_key: %s
    models: [default, opus, sonnet, haiku, fable]
model_slots:
  default: alpha/default
  opus: alpha/opus
  sonnet: alpha/sonnet
  haiku: alpha/haiku
  fable: alpha/fable
`, secret)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	old := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = old })
	return path
}

func executeClaudeCommand(t *testing.T, operation, settings, configPath string) (claudeEnvelope, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "client", "claude", operation, "--json", "--settings", settings})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	var envelope claudeEnvelope
	if decodeErr := json.Unmarshal(stdout.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode stdout %q: %v", stdout.String(), decodeErr)
	}
	return envelope, stderr.String(), err
}

func TestClaudeCommandsApplyStatusAndRestore(t *testing.T) {
	secret := "upstream-secret-marker"
	configPath := writeClaudeCommandConfig(t, secret)
	settings := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"theme\": \"light\", \"env\": {\"KEEP\": \"yes\"}\n}\n")
	if err := os.WriteFile(settings, original, 0o600); err != nil {
		t.Fatal(err)
	}

	apply, stderr, err := executeClaudeCommand(t, "apply", settings, configPath)
	if err != nil || stderr != "" || !apply.OK || apply.Operation != "apply" || !apply.Result.Changed || !apply.Result.BackupCreated {
		t.Fatalf("apply = %+v, stderr = %q, err = %v", apply, stderr, err)
	}
	encoded, _ := json.Marshal(apply)
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("apply output leaked upstream secret: %s", encoded)
	}
	status, stderr, err := executeClaudeCommand(t, "status", settings, configPath)
	if err != nil || stderr != "" || !status.OK || status.Result.SyncState != "current" || status.Result.Changed {
		t.Fatalf("status = %+v, stderr = %q, err = %v", status, stderr, err)
	}
	restore, stderr, err := executeClaudeCommand(t, "restore", settings, configPath)
	if err != nil || stderr != "" || !restore.OK || !restore.Result.AtomicReplaced || restore.Result.SyncState != "different" {
		t.Fatalf("restore = %+v, stderr = %q, err = %v", restore, stderr, err)
	}
	after, _ := os.ReadFile(settings)
	if !bytes.Equal(after, original) {
		t.Fatal("restore did not preserve exact original bytes")
	}
}

func TestClaudeCommandFailuresUseSecretSafeEnvelope(t *testing.T) {
	secret := "another-upstream-secret"
	configPath := writeClaudeCommandConfig(t, secret)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"env":"client-secret-marker"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	envelope, stderr, err := executeClaudeCommand(t, "apply", settings, configPath)
	if !errorsIsMachineOutput(err) || stderr != "" || envelope.OK || len(envelope.Errors) != 1 || envelope.Errors[0].Code != "settings_invalid" {
		t.Fatalf("envelope = %+v, stderr = %q, err = %v", envelope, stderr, err)
	}
	encoded, _ := json.Marshal(envelope)
	if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte("client-secret-marker")) {
		t.Fatalf("failure output leaked secret: %s", encoded)
	}
}

func TestClaudeStatusReportsInvalidSettingsWithoutFailure(t *testing.T) {
	configPath := writeClaudeCommandConfig(t, "fake-key")
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"env":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	envelope, stderr, err := executeClaudeCommand(t, "status", settings, configPath)
	if err != nil || stderr != "" || !envelope.OK || envelope.Result.ParseState != "invalid" || envelope.Result.SyncState != "invalid" {
		t.Fatalf("envelope = %+v, stderr = %q, err = %v", envelope, stderr, err)
	}
}

func TestClaudeRestoreMissingBackup(t *testing.T) {
	configPath := writeClaudeCommandConfig(t, "fake-key")
	settings := filepath.Join(t.TempDir(), "settings.json")
	envelope, stderr, err := executeClaudeCommand(t, "restore", settings, configPath)
	if !errorsIsMachineOutput(err) || stderr != "" || envelope.OK || envelope.Errors[0].Code != "backup_missing" || envelope.Errors[0].Path == nil {
		t.Fatalf("envelope = %+v, stderr = %q, err = %v", envelope, stderr, err)
	}
}

func TestClaudeCommandRejectsInvalidRouterConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(configPath, []byte("providers: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	envelope, stderr, err := executeClaudeCommand(t, "status", filepath.Join(t.TempDir(), "settings.json"), configPath)
	if !errorsIsMachineOutput(err) || stderr != "" || envelope.OK || envelope.Errors[0].Code != "router_config_error" {
		t.Fatalf("envelope = %+v, stderr = %q, err = %v", envelope, stderr, err)
	}
}

func TestClaudeCommandRequiresJSONEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := claudeOperationCmd("status")
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	if !errorsIsMachineOutput(err) || stderr.String() != "" || !strings.Contains(stdout.String(), `"invalid_arguments"`) {
		t.Fatalf("stdout = %q, stderr = %q, err = %v", stdout.String(), stderr.String(), err)
	}
}

func TestClaudeCommandRejectsArgumentsWithEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := claudeOperationCmd("status")
	cmd.SetArgs([]string{"--json", "extra"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	if !errorsIsMachineOutput(err) || stderr.String() != "" || !strings.Contains(stdout.String(), `"invalid_arguments"`) {
		t.Fatalf("stdout = %q, stderr = %q, err = %v", stdout.String(), stderr.String(), err)
	}
}

func errorsIsMachineOutput(err error) bool {
	return err == errMachineOutput
}
