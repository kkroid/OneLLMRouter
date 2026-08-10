package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
)

func TestConfigApplyPreservesCommentsUnknownFieldsAndOldKey(t *testing.T) {
	path := writeCommandConfig(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original = append([]byte("# keep comment\nfuture_setting: keep\n"), original...)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	setCommandConfigPath(t, path)
	existing, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := config.NewSnapshot(existing)
	snapshot.Providers[0].BaseURL = "https://changed.invalid"
	snapshot.ModelSlots.Default = "alpha/model"
	overwriteCatalog := true
	snapshot.Codex.OverwriteCatalog = &overwriteCatalog
	input, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := configApplyCmd()
	cmd.SetArgs([]string{"--stdin-json"})
	cmd.SetIn(bytes.NewReader(input))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := "old-" + "secret"
	for _, expected := range []string{"# keep comment", "future_setting: keep", "http_port: 3456", "overwrite_catalog: true", "https://changed.invalid", oldKey} {
		if !bytes.Contains(updated, []byte(expected)) {
			t.Errorf("updated config omitted %q:\n%s", expected, updated)
		}
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatal("backup is not the exact pre-apply YAML")
	}
	if bytes.Contains(output.Bytes(), []byte(oldKey)) {
		t.Fatalf("config-apply output exposed API key: %s", output.Bytes())
	}
}

func TestConfigApplyRejectsChangedPrefixWithoutKey(t *testing.T) {
	path := writeCommandConfig(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	setCommandConfigPath(t, path)
	existing, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := config.NewSnapshot(existing)
	snapshot.Providers[0].Prefix = "beta"
	input, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := configApplyCmd()
	cmd.SetArgs([]string{"--stdin-json"})
	cmd.SetIn(bytes.NewReader(input))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("invalid apply changed original config")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("invalid apply created backup: %v", err)
	}
}

func TestConfigApplyAcceptsExplicitReplacementWithoutEchoingIt(t *testing.T) {
	path := writeCommandConfig(t)
	setCommandConfigPath(t, path)
	existing, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := config.NewSnapshot(existing)
	newKey := "new-" + "secret"
	snapshot.Providers[0].APIKey = &newKey
	input, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := configApplyCmd()
	cmd.SetArgs([]string{"--stdin-json"})
	cmd.SetIn(bytes.NewReader(input))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte(newKey)) {
		t.Fatal("updated config omitted explicit replacement key")
	}
	if bytes.Contains(output.Bytes(), []byte(newKey)) || bytes.Contains(backup, []byte(newKey)) {
		t.Fatal("explicit replacement key leaked outside updated config")
	}
}
