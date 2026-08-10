package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
)

func TestConfigValidateReturnsStableFieldErrorsWithoutWriting(t *testing.T) {
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
	snapshot.Providers[0].BaseURL = ""
	input, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := configValidateCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SetIn(bytes.NewReader(input))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result configValidationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Errors) == 0 || result.Errors[0].Field != "providers[0]" {
		t.Fatalf("validation result = %+v", result)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("config-validate modified the configuration")
	}
}

func TestConfigValidateRejectsNonEditableFields(t *testing.T) {
	path := writeCommandConfig(t)
	setCommandConfigPath(t, path)
	var output bytes.Buffer
	cmd := configValidateCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SetIn(bytes.NewBufferString(`{"providers":[],"codex":{"models":{}},"model_slots":{},"server":{"http_port":9999}}`))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result configValidationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Errors) == 0 || result.Errors[0].Field != "$" {
		t.Fatalf("validation result = %+v", result)
	}
}

func TestConfigValidateAcceptsCodexOverwriteCatalog(t *testing.T) {
	path := writeCommandConfig(t)
	setCommandConfigPath(t, path)
	var output bytes.Buffer
	cmd := configValidateCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SetIn(bytes.NewBufferString(`{"providers":[{"name":"Alpha","prefix":"alpha","base_url":"https://example.invalid","openai_base_url":"","responses_base_url":"","api_key_set":true,"models":["model"],"proxy":null}],"codex":{"overwrite_catalog":true,"models":{}},"model_slots":{"default":"alpha/model"}}`))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result configValidationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("validation result = %+v", result)
	}
}

func TestConfigValidateDoesNotEchoMalformedSecretInput(t *testing.T) {
	path := writeCommandConfig(t)
	setCommandConfigPath(t, path)
	secret := "proposed-" + "secret"
	var output bytes.Buffer
	cmd := configValidateCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SetIn(bytes.NewBufferString(`{"api_key":"` + secret))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte(secret)) {
		t.Fatalf("validation output echoed secret: %s", output.Bytes())
	}
}
