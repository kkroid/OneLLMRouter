# Ubuntu 24.04 命令行版

OneLLMRouter 可在 Ubuntu 24.04 上以前台命令行服务运行。Linux 命令行版不包含 Windows
桌面端、安装器或后台 daemon 命令。

## 构建

安装 Git、curl 和 `go.mod` 声明的 Go 版本。以下命令为 x86-64 用户安装该 Go 工具链，
不会替换系统级安装：

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

请在仓库根目录执行。Arm64 系统需将下载地址中的 `linux-amd64` 改为 `linux-arm64`。

## 配置

创建显式 YAML 配置。在发起推理请求前，请替换示例端点、密钥和模型名。

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

实际配置中会包含 provider 凭据，请限制文件权限：

```bash
chmod 600 "$HOME/.config/onellm-router/onellm-router.yaml"
```

## 运行和停止

通过显式配置路径以前台模式启动。保存 PID 后，脚本或另一个终端可以只停止该进程：

```bash
bin/onellm-router serve \
  --config "$HOME/.config/onellm-router/onellm-router.yaml" &
ROUTER_PID=$!
```

健康检查只访问本地服务，不会请求已配置的 provider：

```bash
curl --fail --show-error http://127.0.0.1:3456/health
```

健康响应包含 `"status":"ok"` 和 `"service":"onellm-router"`。向准确的前台进程发送
SIGTERM，并等待它完成优雅退出：

```bash
kill -TERM "$ROUTER_PID"
wait "$ROUTER_PID"
```

以下命令可持续验证同一条隔离的构建、配置、健康检查和停止路径，且不会访问 provider：

```bash
bash tools/ubuntu-cli-smoke.sh
```
