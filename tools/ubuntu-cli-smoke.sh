#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
router_pid=""

port_accepts_connections() {
  python3 - "$port" <<'PY'
import socket
import sys

try:
    with socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=1):
        pass
except OSError:
    raise SystemExit(1)
PY
}

cleanup() {
  if [[ -n "$router_pid" ]] && kill -0 "$router_pid" 2>/dev/null; then
    kill -TERM "$router_pid" 2>/dev/null || true
    wait "$router_pid" 2>/dev/null || true
  fi
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }
if [[ -z "${ONELLM_ROUTER_BINARY:-}" ]]; then
  command -v go >/dev/null || { echo "go is required" >&2; exit 1; }
fi

port="$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
    listener.bind(("127.0.0.1", 0))
    print(listener.getsockname()[1])
PY
)"

binary="$temp_dir/onellm-router"
config="$temp_dir/onellm-router.yaml"
health="$temp_dir/health.json"
mkdir -p "$temp_dir/home" "$temp_dir/logs"

if [[ -n "${ONELLM_ROUTER_BINARY:-}" ]]; then
  cp -- "$ONELLM_ROUTER_BINARY" "$binary"
  chmod +x "$binary"
else
  (
    cd "$repo_root"
    CGO_ENABLED=0 go build -trimpath -o "$binary" ./cmd/onellm-router
  )
fi

cat > "$config" <<YAML
server:
  host: "127.0.0.1"
  http_port: $port
log:
  level: "info"
  dir: "$temp_dir/logs"
  max_age_days: 1
proxy:
  socks5: ""
retry:
  enabled: false
  max_attempts: 1
  status_codes: []
  initial_delay: 1ms
  max_delay: 1ms
  max_elapsed: 1ms
  jitter: 0
  honor_retry_after: false
codex:
  overwrite_catalog: false
providers:
  - name: "Smoke Test"
    prefix: "smoke"
    base_url: "http://127.0.0.1:1/anthropic"
    api_key: "not-a-real-key"
    proxy: false
    models:
      - id: "local-health-only"
        endpoints: [anthropic]
model_slots:
  default: "smoke/local-health-only"
  opus: "smoke/local-health-only"
  sonnet: "smoke/local-health-only"
  haiku: "smoke/local-health-only"
  fable: "smoke/local-health-only"
YAML

HOME="$temp_dir/home" "$binary" serve --config "$config" \
  >"$temp_dir/stdout.log" 2>"$temp_dir/stderr.log" &
router_pid=$!

for _ in {1..100}; do
  if curl --silent --show-error --fail \
      --connect-timeout 1 --max-time 1 \
      "http://127.0.0.1:$port/health" >"$health"; then
    break
  fi
  if ! kill -0 "$router_pid" 2>/dev/null; then
    echo "router exited before becoming healthy" >&2
    cat "$temp_dir/stderr.log" >&2
    exit 1
  fi
  sleep 0.1
done

python3 - "$health" "$router_pid" "$port" <<'PY'
import json
import pathlib
import sys

health_path, expected_pid, expected_port = sys.argv[1:]
try:
    payload = json.loads(pathlib.Path(health_path).read_text(encoding="utf-8"))
except (FileNotFoundError, json.JSONDecodeError) as error:
    raise SystemExit(f"health endpoint did not return valid JSON: {error}")

expected = {
    "status": "ok",
    "service": "onellm-router",
    "pid": int(expected_pid),
    "http_port": int(expected_port),
}
for key, value in expected.items():
    if payload.get(key) != value:
        raise SystemExit(
            f"unexpected health field {key}: {payload.get(key)!r}, expected {value!r}"
        )
PY

kill -TERM "$router_pid"
if ! wait "$router_pid"; then
  echo "router did not exit successfully after SIGTERM" >&2
  router_pid=""
  exit 1
fi
router_pid=""

for _ in {1..20}; do
  if ! port_accepts_connections; then
    echo "Ubuntu CLI smoke test passed"
    exit 0
  fi
  sleep 0.1
done

echo "port $port still accepts connections after shutdown" >&2
exit 1
