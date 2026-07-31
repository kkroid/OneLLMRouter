package main

import (
	"encoding/json"
	"testing"
)

func TestHealthPayloadIdentifiesRouter(t *testing.T) {
	payload := buildHealthPayload("1.4.0", 1234, 3456, 7, `C:\config\router.yaml`, "127.0.0.1:1082")
	if payload.Service != "onellm-router" || payload.Status != "ok" {
		t.Fatalf("identity = %+v", payload)
	}
	if payload.PID != 1234 || payload.Models != 7 || payload.ProxySOCKS5 != "127.0.0.1:1082" {
		t.Fatalf("runtime fields = %+v", payload)
	}
	if payload.ConfigPath != `C:\config\router.yaml` {
		t.Fatalf("config path = %q", payload.ConfigPath)
	}
	if payload.Version != "1.4.0" || payload.HTTPPort != 3456 {
		t.Fatalf("compatibility fields = %+v", payload)
	}
}

func TestHealthPayloadAlwaysIncludesProxyField(t *testing.T) {
	data, err := json.Marshal(buildHealthPayload("dev", 1, 2, 3, `C:\config\router.yaml`, ""))
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"status", "service", "pid", "version", "http_port", "models", "config_path", "proxy_socks5",
	} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("missing %q in %s", field, data)
		}
	}
	if _, ok := fields["copilot_token"]; ok {
		t.Fatalf("unexpected copilot_token in %s", data)
	}
}
