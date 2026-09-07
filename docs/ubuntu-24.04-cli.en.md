# Ubuntu 24.04 CLI

OneLLMRouter runs as a foreground CLI service on Ubuntu 24.04. The Linux CLI does not provide the
Windows desktop, installer, or background daemon commands.

## Build

Install Git, curl, and the Go version declared by `go.mod`. The commands below install that Go
toolchain for an x86-64 user account without replacing a system installation:

```bash
sudo apt update
sudo apt install --yes ca-certificates curl git

GO_VERSION="$(awk '$1 == "go" { print $2 }' go.mod)"
mkdir -p "$HOME/.local"
curl --fail --location --output /tmp/go.tar.gz \
  "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
tar --extract --gzip --file /tmp/go.tar.gz --directory "$HOME/.local"
export PATH="$HOME/.local/go/bin:$PATH"

go version
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/onellm-router ./cmd/onellm-router
```

Run these commands from the repository root. For Arm64, replace `linux-amd64` with `linux-arm64` in
the download URL.

## Configure

Create an explicit YAML configuration. Replace the example endpoint, key, and model before making
inference requests.

```bash
mkdir -p "$HOME/.config/onellm-router"
cat > "$HOME/.config/onellm-router/onellm-router.yaml" <<'YAML'
server:
  host: "127.0.0.1"
  http_port: 3456

log:
  level: "info"
  dir: "~/.onellm/logs"
  max_age_days: 30

proxy:
  socks5: ""

retry:
  enabled: true
  max_attempts: 3
  status_codes: [429, 500, 502, 503, 504]
  initial_delay: 1s
  max_delay: 10s
  max_elapsed: 30s
  jitter: 0.2
  honor_retry_after: true

codex:
  overwrite_catalog: true

providers:
  - name: "Dummy Provider"
    prefix: "dummy"
    base_url: "https://provider.example/anthropic"
    api_key: "not-a-real-key"
    proxy: false
    models:
      - id: "example-model"
        endpoints: [anthropic]

model_slots:
  default: "dummy/example-model"
  opus: "dummy/example-model"
  sonnet: "dummy/example-model"
  haiku: "dummy/example-model"
  fable: "dummy/example-model"
YAML
```

Protect the file because a real configuration contains the provider credential:

```bash
chmod 600 "$HOME/.config/onellm-router/onellm-router.yaml"
```

## Run and stop

Start the service in the foreground with the explicit configuration path. Keeping the PID lets a
script or another terminal stop only this process:

```bash
bin/onellm-router serve \
  --config "$HOME/.config/onellm-router/onellm-router.yaml" &
ROUTER_PID=$!
```

The health endpoint is local and does not call the configured provider:

```bash
curl --fail --show-error http://127.0.0.1:3456/health
```

A healthy response contains `"status":"ok"` and `"service":"onellm-router"`. Stop the exact
foreground process gracefully with SIGTERM and wait for shutdown to finish:

```bash
kill -TERM "$ROUTER_PID"
wait "$ROUTER_PID"
```

To continuously exercise the same isolated build, configuration, health, and shutdown path without
contacting a provider, run:

```bash
bash tools/ubuntu-cli-smoke.sh
```
