# OneLLMRouter Claude Code 开发指南

## 项目定位

**个人 AI 模型路由网关** — 将 GitHub Copilot Claude 模型 + 任意 Anthropic-compatible API 统一暴露为标准 Anthropic + OpenAI 双接口，供 Claude Code 等工具使用。

- 每人独立部署一套，单机运行
- 不追求高并发（几个工具同时调用）
- **稳定性第一**：不能崩、不能内存泄漏、异常可复现
- **可排查性好**：日志日期滚动、错误栈完整、请求可追踪

## 架构

```
Claude Code CLI              OpenAI 兼容工具
(Anthropic API)              (OpenAI API)
       │                           │
       ▼                           ▼
 /anthropic/v1/messages    /openai/v1/chat/completions
       │                           │
       └───────────┬───────────────┘
                   ▼
    ┌─────────────────────────────┐
    │     onellm-router (Go)       │  ← 单二进制守护进程
    │     · HTTP proxy            │
    │     · Anthropic ↔ OpenAI    │
    │     · 协议翻译 + SSE 流式    │
    └─────────────────────────────┘
```

## 开发环境

| 项目 | 详情 |
|------|------|
| 操作系统 | Windows 11 Pro (10.0.26200) |
| Shell | bash（Git Bash on Windows） |
| SOCKS5 代理 | `127.0.0.1:1082`（用于访问 GitHub Copilot API 等外网） |
| IDE | VS Code（主力）+ Claude Code（AI 辅助） |

### 技术栈

| 层 | 语言 | 框架/库 | 说明 |
|----|------|---------|------|
| 守护进程 | **Go** 1.22+ | cobra, net/http, slog | 代理核心 + API 路由 + 协议翻译 |

### 为什么 Go？

- **稳定性**：GC + 内存安全，没有悬空指针/buffer overflow，守护进程长期运行更可靠
- **可排查性**：panic 自动输出完整 goroutine 栈；`context.Context` 天然携带 trace ID；error wrapping 递归展开错误链
- **网络代理是 Go 的主场**：`net/http` + goroutine + channel 写代理/转发/SSE 流式数据比 C++ 少 3-5 倍代码量，更不容易出错
- **部署简单**：单个 7.8 MB exe，无运行时依赖，复制就能跑

## 项目结构

```
OneLLMRouter/
├── cmd/onellm-router/main.go           # CLI 入口
├── internal/
│   ├── auth/                          # GitHub device OAuth + token 管理
│   ├── config/                        # YAML 配置加载
│   ├── log/                           # slog + 按日滚动日志
│   ├── proxy/                         # HTTP 代理 (Copilot + External)
│   ├── router/                        # Provider 解析 + 模型路由
│   └── translate/                     # Anthropic ↔ OpenAI 协议翻译
├── onellm-router.example.yaml          # 配置模板
├── build.ps1                          # 编译脚本
└── go.mod
```

## Claude Code 项目斜杠命令

| 命令 | 文件 | 用途 |
|------|------|------|
| `/build` | `.claude/commands/build.md` | Go 编译 + 验证 |
| `/commit` | `.claude/commands/commit.md` | Conventional Commit + Co-Authored-By 签名 |
| `/review` | `.claude/commands/review.md` | Go 代码审查 |
| `/plan` | `.claude/commands/plan.md` | 任务分解 + 验证标准定义 |

---

## 核心行为准则

### 1. 编码前思考 — 不假设、不隐藏困惑、呈现权衡

动手前必须：
- **明确陈述假设**。如果不确定，**必须先问**，不要默默选择一种理解。
- 如果存在多种解释，**全部列出**，不要替我选。
- 如果存在更简单、更稳妥的做法，**大胆指出并给出建议**。
- 如果我的需求有问题，**必须及时指出**，不要盲目执行。

### 2. 简洁优先 — 用最少代码解决问题，不做推测性开发

- 用能解决问题的最少代码完成任务，**不添加需求之外的功能**。
- **不为一次性逻辑创建抽象**，不添加未要求的灵活性。
- 不为不可能发生或没有证据表明会发生的场景堆叠防御代码。
- 如果实现明显可以更短、更清晰，**主动收敛复杂度**。

### 3. 精准修改 — 只碰必须碰的，只清理自己造成的混乱

- **只修改完成当前请求必须修改的内容**；不要顺手重构、改格式。
- **匹配项目现有风格**，即使你个人更倾向于另一种写法。
- 对自己没有充分理解的现有代码，**不要做旁路改动**。
- 如果发现无关的死代码或历史问题，**可以汇报，但不要擅自删除**。

### 4. 验证闭环 — 定义成功标准，循环验证直到达成

- 每步必须有可验证的输出（编译通过？代理请求正常？）
- 完成明确步骤后，**汇报结果、风险、验证情况和建议的下一步**。
- 如果验证失败，先基于证据定位原因。

---

## 技术约定

### Go 后台

| 约定 | 说明 |
|------|------|
| 版本 | Go 1.22+ |
| 模块 | `github.com/kkroid/onellm-router` |
| CLI 框架 | `cobra` + `viper` |
| 日志 | `log/slog` + 自实现 daily writer（按天切分，保留 30 天，启动时清理过期日志） |
| 请求 ID | 每个请求生成 UUID v4，通过 `context.Context` 传递 |
| 错误处理 | `fmt.Errorf("...: %w", err)` 包装错误链，绝不吞错误 |
| 代理 | Copilot API 走 SOCKS5 代理（yaml 中 `proxy.socks5` 配置） |
| HTTP | `net/http` 标准库 |
| SSE 流式输出 | Anthropic: `event: <type>\ndata: <json>\n\n`；OpenAI: `data: <json>\n\n` SSE 直通 |
| 测试 | `go test`，表驱动测试 |

#### 日志格式

```json
{"time":"2026-06-12T10:30:00+08:00","level":"INFO","msg":"request","request_id":"a1b2c3d4","method":"POST","path":"/v1/messages","status":200,"duration_ms":1234,"model":"cp/claude-opus-4.8","provider":"cp","stream":true,"ttfb_ms":650}
```

### 构建命令

```bash
# 编译脚本（推荐）
pwsh build.ps1 -Version "1.0.0"

# 手动编译
go build -ldflags="-s -w -X main.version=1.2.0" -o dist/onellm-router-v1.2.0.exe ./cmd/onellm-router/

# 测试
go test ./...
```

### 配置文件

- `onellm-router.yaml`：完整配置（端口、日志、代理、provider、model slots）— gitignore
- `onellm-router.example.yaml`：配置模板（提交到 git）

---

## 关键限制与约束

1. **稳定性 > 功能**：新功能可以慢慢加，但不能引入崩溃或内存问题。
2. **每个请求可追踪**：日志必须带 request_id，出问题时能复现完整请求链路。
3. **协议翻译必须精确**：Anthropic ↔ OpenAI 语义等价，不丢失字段。
4. **SSE 输出必须符合 Anthropic 规范**：`event: <type>\ndata: <json>\n\n` 格式，测试必须验证 event 行存在。
5. **仅本地通信**：HTTP API 只监听 `127.0.0.1`，不暴露到网络。
6. **Git 提交信息末尾必须包含**：
   ```
   Generated with [Claude Code](https://claude.ai/code)
   via [Happy](https://happy.engineering)

   Co-Authored-By: Claude <noreply@anthropic.com>
   Co-Authored-By: Happy <yesreply@happy.engineering>
   ```

---

## 验证方式

```bash
# 1. 启动
./dist/onellm-router-v1.2.0.exe

# 2. 健康检查
curl http://localhost:3456/health

# 3. 模型列表
curl http://localhost:3456/anthropic/v1/models

# 4. Anthropic 非流式
curl -X POST http://localhost:3456/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 5. Anthropic 流式
curl -N -X POST http://localhost:3456/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'

# 6. OpenAI 非流式
curl -X POST http://localhost:3456/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 7. OpenAI 流式
curl -N -X POST http://localhost:3456/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'
```

## 问题排查

### 通过日志定位问题

日志是 JSON 行，每个请求一条。关键字段：`status`、`error`、`model`、`provider`、`path`、`duration_ms`。

```bash
# 查看最近 50 条请求
tail -50 ~/.onellm/logs/onellm-router-$(date +%F).log

# 只看错误
grep '"status":[45]' ~/.onellm/logs/onellm-router-*.log

# 查看 WARN 级别
grep '"level":"WARN"' ~/.onellm/logs/onellm-router-*.log
```

### 常见错误速查

| error 关键字 | 根因 | 排查方向 |
|-------------|------|---------|
| `Insufficient Balance` | DeepSeek 余额不足 | 充值 |
| `context canceled` | 客户端主动断开 | 通常正常（Ctrl+C / 切换模型） |
| `context deadline exceeded` | 上游响应太慢 | 非流式 2min 超时，可能是模型卡住 |
| `parse response: unexpected end of JSON` | 响应体被截断 | 上游中途断连，或 LimitReader 超限 |
| `invalid max_tokens` | 客户端传了非法值 | 客户端 bug，代理不兜底 |
| `model names are ... but you passed` | 模型名不匹配 | OpenAI 端点检查是否含 `[1m]` 后缀 |
| `connection forcibly closed` + `127.0.0.1:1082` | SOCKS5 代理断开 | 检查 provider 是否漏配 `proxy: false` |
| `stream truncated: no` | SSE 流未正常结束 | 上游断连，`[DONE]`/`message_stop` 未收到 |
| `skip malformed stream chunk` | SSE chunk JSON 损坏 | 上游返回了非标准格式 |

### 区分"代理问题"和"上游/客户端问题"

- 错误在 `error` 字段且被 `{}` 包裹 → 上游返回的，代理只是透传（如 `{"error":{...}}`）
- 错误以 `copilot api` / `external api` / `upstream` 开头 → 代理层与上游通信失败
- `status: 200` 但客户端报错 → 检查响应 body 内容，可能是格式不兼容
- `status: 502` → 代理无法连接上游（网络/DNS/代理断开）
- `status: 400` → 大多为上游拒绝请求，代理正确透传

### 测试流程

测试必须：
1. **另起端口**（默认 3465），**后台进程**，不干扰生产 3457
2. **看响应内容**，不只 HTTP 状态码和大小
3. 测试完清理

详见 memory 中的 [[testing-methodology]]。
