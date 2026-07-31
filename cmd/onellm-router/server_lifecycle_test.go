package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeListenHelper(t *testing.T) {
	if os.Getenv("ONELLM_SERVE_LISTEN_HELPER") != "1" {
		return
	}
	cfgFile = os.Getenv("ONELLM_SERVE_LISTEN_CONFIG")
	noPidLock = true
	command := serveCmd()
	err := command.RunE(command, nil)
	if err == nil {
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func TestServeReturnsWhenListenFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	configFile := filepath.Join(dir, "onellm-router.yaml")
	configData := fmt.Sprintf(`server:
  host: "127.0.0.1"
  http_port: %d
log:
  level: "error"
  dir: "%s"
proxy:
  socks5: ""
codex:
  overwrite_catalog: false
providers:
  - name: "test"
    prefix: "test"
    base_url: "http://127.0.0.1:9"
    api_key: "test"
    proxy: false
    models: ["model"]
`, port, filepath.ToSlash(filepath.Join(dir, "logs")))
	if err := os.WriteFile(configFile, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServeListenHelper$")
	command.Env = append(os.Environ(),
		"ONELLM_SERVE_LISTEN_HELPER=1",
		"ONELLM_SERVE_LISTEN_CONFIG="+configFile,
		"HOME="+dir,
		"USERPROFILE="+dir,
	)
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("serve remained alive after HTTP listen failed")
	}
	if err == nil || !strings.Contains(string(output), "listen") {
		t.Fatalf("error = %v, output = %s; want listen failure", err, output)
	}
}
