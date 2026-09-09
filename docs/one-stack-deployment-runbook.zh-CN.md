# OneProxy + OneLLMRouter Docker Compose 部署 Runbook

适用范围：Ubuntu 24.04、Docker Compose v2、`amd64`。Router 仅监听服务器本机
`127.0.0.1:3456`，OneProxy 仅供 Compose 内部使用。

## 0. 上线前确认

- [ ] 服务器架构为 `amd64`；ARM64 需重新制作镜像。
- [ ] 已安装并启动 Docker Engine、Compose v2。
- [ ] 服务器可访问镜像源、Go 模块源和 OneProxy 节点。
- [ ] 两份配置已通过安全通道传输，未提交到 Git 或普通制品库。
- [ ] 端口 `3456` 仅供本机使用；远程访问使用 SSH 隧道。
- [ ] 已保留上一版本部署目录和镜像，用于回滚。

当前生产缺口：目标机联网构建、仅支持 `amd64`、凭据为明文只读挂载、尚未接入集中监控。
正式规模化部署前，应改为 CI 构建并推送按 digest 固定的镜像，同时接入密钥管理和告警。

## 1. 准备服务器

```bash
sudo apt update
sudo apt install --yes ca-certificates curl docker.io docker-compose-v2
sudo systemctl enable --now docker
docker compose version
docker version
```

如当前用户需要直接运行 Docker：

```bash
sudo usermod -aG docker "$USER"
```

重新登录后继续，不要放宽 Docker socket 权限。

## 2. 上传部署包

将整个 `one-stack` 目录通过 SSH/SCP 上传到服务器，例如：

```bash
scp -r one-stack user@server:/tmp/one-stack
ssh user@server
sudo mv /tmp/one-stack /opt/one-stack
sudo chown -R "$USER":"$USER" /opt/one-stack
cd /opt/one-stack
```

收紧配置权限：

```bash
sudo chown -R 1000:1000 config
sudo chmod 700 config config/oneproxy config/onellm-router
sudo chmod 600 config/oneproxy/config.json
sudo chmod 600 config/onellm-router/onellm-router.yaml
```

镜像内服务固定使用 UID/GID `1000:1000`，因此配置属主不能随部署账号变化。

配置必须保持以下容器内参数：

- OneProxy：监听 `0.0.0.0:1082`，`dns.flush_on_failure=false`。
- Router：监听 `0.0.0.0:3456`，`proxy.socks5=oneproxy:1082`。
- Compose：宿主机仅发布 `127.0.0.1:3456:3456`。

## 3. 构建并启动

```bash
cd /opt/one-stack
docker compose config -q
docker compose build --pull
docker compose up -d --wait --wait-timeout 120
docker compose ps
```

预期：`oneproxy` 和 `onellm-router` 均为 `healthy`，OneProxy 无宿主机端口。

## 4. 验证

```bash
curl --fail --show-error http://127.0.0.1:3456/health
ss -ltn | grep '127.0.0.1:3456'
docker compose logs --since=5m --no-color
```

健康响应应包含：

```text
"status":"ok"
"service":"onellm-router"
"proxy_socks5":"oneproxy:1082"
```

需要验证代理出口时执行一次免费 204 请求：

```bash
docker run --rm --network one-stack_default \
  docker.m.daocloud.io/curlimages/curl:8.16.0 \
  --fail --max-time 20 --socks5-hostname oneproxy:1082 \
  --output /dev/null --write-out 'HTTP %{http_code}\n' \
  https://www.gstatic.com/generate_204
```

预期为 `HTTP 204`。这不会调用模型 Provider。

## 5. 远程使用

不要把 `3456` 直接发布到公网。在客户端建立 SSH 隧道：

```bash
ssh -N -L 3456:127.0.0.1:3456 user@server
```

客户端继续访问 `http://127.0.0.1:3456`。

## 6. 修改配置

```bash
cd /opt/one-stack
sudo cp -a config "config.backup.$(date +%Y%m%d-%H%M%S)"
# 使用 sudoedit 编辑两份挂载配置
sudo chown -R 1000:1000 config
sudo chmod 600 config/oneproxy/config.json config/onellm-router/onellm-router.yaml
docker compose up -d --force-recreate --wait --wait-timeout 120
curl --fail --show-error http://127.0.0.1:3456/health
```

备份中含真实凭据，必须使用同等权限保护。

## 7. 升级与回滚

升级前保留旧部署目录和旧镜像；新版本必须使用新的镜像标签：

```bash
cd /opt/one-stack
docker compose build --pull
docker compose up -d --wait --wait-timeout 120
docker compose ps
```

验证失败时，切回上一版本的 Compose 文件和镜像标签，然后执行：

```bash
docker compose up -d --no-build --wait --wait-timeout 120
```

不要执行 `docker compose down -v`，否则会删除两个运行状态卷。

## 8. 日常操作

```bash
cd /opt/one-stack
docker compose ps
docker compose logs -f --tail=100
docker compose restart
docker compose stop
docker compose start
```

## 9. 故障检查

```bash
cd /opt/one-stack
docker compose config -q
docker compose ps
docker compose logs --tail=200 --no-color oneproxy
docker compose logs --tail=200 --no-color onellm-router
docker inspect one-stack-oneproxy-1 --format '{{json .State.Health}}'
docker inspect one-stack-onellm-router-1 --format '{{json .State.Health}}'
```

排查顺序：Docker 是否启动 -> OneProxy 是否健康 -> Router 是否健康 -> `3456` 是否仅监听
`127.0.0.1` -> Provider 配置与网络是否可达。
