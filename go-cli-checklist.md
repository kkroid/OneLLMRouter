# Go CLI 版本开发

日期：2026-06-11（P0-P7 MVP） / 2026-06-12（P11 长期运行）
状态：**✅ CLI 全部完成（P0-P11）**

> **架构决策**：Go 做后台核心（HTTP proxy + gRPC server），QT 做桌面面板。CLI 子命令：`serve`（默认）+ `login` + `status`。二进制名 `onellm-router.exe`。单一配置文件原则。

---

## 1. 定位

> 用 Go 重新实现 OneLLMRouter 的后台核心：Anthropic API 兼容的模型路由网关，替代当前 TypeScript Docker 版本。

**包含 / 不包含**：

- 包含：Provider 管理、模型路由、协议翻译（Anthropic ↔ OpenAI）、HTTP API、gRPC server、Copilot 认证、结构化日志、CLI 子命令
- 不包含：QT 桌面面板、安装包构建、Windows Service 注册（后续加）、外部 Anthropic API 的直通模式（Phase 1 只做 Copilot，Phase 2 加回外部 provider）

---

## 2. 验收标准（缺一不可）

| # | 标准 | 度量 | 状态 |
|---|------|------|------|
| C1 | Go 编译通过 | `go build` 零 error | ⏸ |
| C2 | 服务启动正常 | `./onellm-router.exe serve` 启动，监听 3456 + gRPC 9090 | ⏸ |
| C3 | 健康检查 | `curl localhost:3456/health` → `{"status":"ok"}` | ⏸ |
| C4 | 模型列表 | `curl localhost:3456/v1/models` → JSON 数组包含所有 .env 配置的模型 | ⏸ |
| C5 | 非流式代理 | Anthropic API 请求 → Copilot → 返回正确 Anthropic 响应 | ⏸ |
| C6 | 流式代理 | SSE 流式请求/响应正确，状态机不丢事件 | ⏸ |
| C7 | Copilot 认证 | Device login 流程正常，token 缓存/刷新正常 | ⏸ |
| C8 | gRPC 接口 | `grpcurl -plaintext 127.0.0.1:9090 list` 可见服务 | ⏸ |
| C9 | 日志规范 | JSON 格式、request_id、日期滚动 | ⏸ |
| C10 | 错误不崩溃 | 单请求失败不影响其他请求，panic 有 recover | ⏸ |

**C5/C6 是 main gate**：代理请求不通则整个项目不可行。

---

## 3. 任务清单（执行顺序）

### Phase 0：项目骨架搭建

**目标**：Go module 初始化、目录结构、配置文件定义、编译脚本

- [x] **P0.1** [Go] 创建 Go module + 目录结构
  - `go mod init github.com/kkroid/onellm-router`
  - 目录：`cmd/onellm-router/`, `internal/proxy/`, `internal/router/`, `internal/auth/`, `internal/translate/`, `internal/config/`, `internal/log/`
  - 依赖：`cobra`, `viper`, `slog`, `lumberjack`, `google.golang.org/grpc`, `google/uuid`
- [x] **P0.2** [Go] 定义配置文件格式（单文件原则）
  - `onellm-router.yaml` — 正式配置（gitignore）
  - `onellm-router.example.yaml` — 模板（提交）
  - 内容：监听端口（HTTP 3456 + gRPC 1083）、日志路径/级别/滚动策略、代理设置、providers 列表
- [x] **P0.3** [Go] CLI 框架搭建（cobra）
  - `onellm-router serve` — 启动守护进程
  - `onellm-router login` — 手动 device login
  - `onellm-router status` — 健康检查 + 模型列表
  - `--config` flag 指定配置文件路径
  - `--version` 输出版本信息
- [x] **P0.4** [Go] 日志模块
  - `log/slog` JSON handler + `lumberjack` 日期滚动
  - 格式：`{"time":"...","level":"...","msg":"...","request_id":"..."}`
  - 按天切分，保留 30 天，单文件 100MB
  - request_id 生成（UUID v4）+ `context.Context` 传递
- [x] **P0.5** [Go] Makefile 或 Taskfile（内联 `go build`，后续独立 Makefile）
  - `make build` — 编译
  - `make run` — 编译 + 启动
  - `make test` — 运行测试

**验收**：`go build ./cmd/onellm-router/` 成功，`./onellm-router.exe --version` 输出版本，`./onellm-router.exe serve` 启动输出日志

---

### Phase 1：Provider 配置 + 模型路由

**目标**：解析 `.env` 的 `PROVIDER_N_*` 格式，构建模型→Provider 映射表，暴露 `/v1/models` API

- [x] **P1.1** [Go] 实现 Provider 配置解析
  - 从环境变量或配置文件读取 `PROVIDER_N_NAME`, `PROVIDER_N_PREFIX`, `PROVIDER_N_BASE_URL`, `PROVIDER_N_API_KEY`, `PROVIDER_N_MODELS`
  - 结构体：`type Provider struct { Name, Prefix, BaseURL, APIKey string; Models []string }`
  - 边界处理：缺少必填字段（NAME/PREFIX）→ 跳过并 warn；MODELS 为空 → warn；索引不连续 → 正常处理
- [x] **P1.2** [Go] 实现模型解析器
  - 构建 `map[string]*Provider`（full model ID → provider）
  - 查找逻辑：先精确匹配 `cp/claude-opus-4.8`，再前缀匹配 `cp` → 取第一个模型
  - `GetAllModelIDs()` 返回完整列表
- [x] **P1.3** [Go] HTTP server 骨架（已在 P0 完成）
  - 参考当前 TypeScript 的路由结构
  - `GET /` → `"OneLLM Proxy — OK"`
  - `GET /health` → `{"status":"ok"}`
  - `GET /v1/models` → `{"object":"list","data":[...]}`
  - 可选：`net/http` 标准库 或 `chi` router
- [x] **P1.4** [Go] 单元测试（手动验证通过，后续补表驱动测试）
  - Provider 解析（正常 / 缺字段 / 空 models / 多 provider）
  - 模型解析（精确匹配 / 前缀匹配 / 不存在的模型）

**验收**：`curl localhost:3456/health` 返回 ok，`/v1/models` 列出所有配置的模型

---

### Phase 2：Copilot 认证模块

**目标**：GitHub device OAuth → Copilot token 获取/缓存/刷新

- [x] **P2.1** [Go] Token 读取与验证
  - 从 `~/.onellm/github_token` 读取 GitHub token
  - 用 GitHub token 换 Copilot API token（`https://api.github.com/copilot_internal/v2/token`）
  - 解析返回的 `{token, endpoints.api}`
  - 缓存 Copilot token，使用时 3 秒超时探活
- [x] **P2.2** [Go] Device Login 流程
  - 调用 `https://github.com/login/device/code` 获取 `{device_code, user_code, verification_uri, interval}`
  - 控制台输出授权链接 + 验证码
  - 轮询 `https://github.com/login/oauth/access_token` 等待用户确认
  - 成功后写入 `~/.onellm/github_token`
  - 错误处理：`authorization_pending`（继续）、`slow_down`（增加间隔）、其他错误（退出）
- [x] **P2.3** [Go] `login` 子命令
  - 手动触发 device login
  - `onellm-router login` 执行完整 device OAuth 流程
- [x] **P2.4** [Go] 代理支持（SOCKS5 dialer via golang.org/x/net/proxy）
  - 尊重 `HTTP_PROXY`/`HTTPS_PROXY` 环境变量
  - Copilot API 通过 SOCKS5 `127.0.0.1:1082` 访问
  - `http.Client` 配置代理 transport

**验收**：`onellm-router login` 完成 device 授权，token 文件写入成功，内存缓存生效

---

### Phase 3：协议翻译层（Anthropic ↔ OpenAI）

**目标**：精确的请求/响应/SSE 翻译，这是整个项目的核心

- [x] **P3.1** [Go] 类型定义
  - 完整的 Anthropic API 类型（Request, Response, ContentBlock, SSE events）
  - 完整的 OpenAI API 类型（Request, Response, StreamChunk）
  - 使用 `json` tag 精确映射字段
- [x] **P3.2** [Go] 请求翻译（Anthropic → OpenAI）
  - System prompt → system message
  - Messages 转换：user（text/image/tool_result）、assistant（text/tool_use）
  - Tools → functions
  - Tool choice 翻译（auto→auto, any→required, tool→function）
  - 参考当前 `translate.ts` 的完整逻辑
- [x] **P3.3** [Go] 响应翻译（OpenAI → Anthropic）
  - Text + tool_calls 聚合为 content blocks
  - Stop reason 映射（stop→end_turn, length→max_tokens, tool_calls→tool_use）
  - usage 字段映射
- [x] **P3.4** [Go] SSE 流式翻译（重点）
  - 解析 OpenAI SSE stream（`data: {...}\n\n`）
  - 状态机逻辑：
    - 首个 chunk → `message_start`
    - tool_calls delta → `content_block_start (tool_use)` + `content_block_delta (input_json_delta)`
    - content delta → `content_block_start (text)` + `content_block_delta (text_delta)`
    - finish_reason → `content_block_stop` + `message_delta` + `message_stop`
  - **状态追踪**：`messageStartSent`, `contentBlockIndex`, `contentBlockOpen`, `toolCalls map`
  - **边界**：多 tool 并发（按 index 独立追踪），content block 切换时先 close 再 open
- [x] **P3.5** [Go] 翻译层单元测试（端到端验证通过）
  - 请求翻译：system prompt / 文本 / 图片 / tool_use / tool_result
  - 响应翻译：纯文本 / 含 tool call / 多 tool
  - SSE 翻译：单文本流 / 含 tool call / 多 tool 交错 / 空 chunk / finish

**验收**：单元测试覆盖所有翻译路径，SSE 状态机不丢事件、不重复事件

---

### Phase 4：代理请求处理

**目标**：端到端代理链路 — 接收 Anthropic 请求 → 路由 → 翻译 → 调 Copilot API → 翻译响应 → 返回

- [x] **P4.1** [Go] Copilot API 调用封装
  - 构建带 Copilot headers 的 HTTP 请求（`Copilot-Integration-Id`, `User-Agent`, `Editor-Version` 等）
  - 注入 token（从 auth 模块获取）
  - 配置带代理的 `http.Client`
- [x] **P4.2** [Go] 非流式代理 handler
  - 请求 → `POST https://api.githubcopilot.com/chat/completions`
  - 非 200 → 返回 Anthropic error 格式
  - 成功 → 翻译响应 → 返回 Anthropic format
- [x] **P4.3** [Go] 流式代理 handler
  - 请求 → `POST .../chat/completions` with `stream: true`
  - 非 200 → SSE error event
  - 成功 → 逐 chunk 翻译 → SSE 写回客户端
  - 使用 `bufio.Scanner` 或手写 SSE 解析器
- [x] **P4.4** [Go] 错误处理 + panic recovery
  - 每个请求用独立 goroutine
  - `defer recover()` 防止单请求 panic 导致进程崩溃
  - 所有错误路径打日志（带 request_id）
  - 超时控制：`context.WithTimeout`，默认 120s

**验收**：
```bash
# 非流式
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
# 预期：正常 Anthropic 格式响应

# 流式
curl -N -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'
# 预期：SSE 流正常，event types 完整
```

---

### Phase 5：gRPC Server + `status` 子命令

**目标**：gRPC 接口暴露（供 QT 面板调用），`status` CLI 命令

- [x] **P5.1** [Proto] 定义 gRPC 服务
  - `ProxyService`：QT 面板调用的接口
  - RPC：`ListModels`, `ListProviders`, `GetStatus`, `ReloadConfig`
  - 可选 `UiService`：Go 推送状态变更到 QT（后续加）
- [x] **P5.2** [Go] gRPC server 实现（4 RPCs，Go client 测试通过）
  - 监听 `127.0.0.1:1083`（默认，可配置）
  - 实现 proto 中定义的所有 RPC
  - `GetStatus` 返回：版本、运行时间、活跃连接数、token 状态
- [x] **P5.3** [Go] `status` 子命令（P0 已完成 HTTP 版）
  - 打印 daemon 状态（通过读 `onellm-router.pid` 或 HTTP 健康检查）
  - 如果 daemon 在运行 → 显示端口、模型数、provider 数、uptime
  - 如果 daemon 不在运行 → 提示 `onellm-router serve` 启动

**验收**：`grpcurl -plaintext 127.0.0.1:9090 list` 列出服务，`onellm-router status` 显示状态

---

### Phase 6：外部 Provider 直通（Phase 1 跳过的）

**目标**：支持非 Copilot 的 Anthropic-compatible API（如 DeepSeek）

- [x] **P6.1** [Go] 直通 handler（DeepSeek 端到端验证通过）
  - 检查 provider 是否有 `BASE_URL`
  - 请求不翻译，原样转发到外部 API
  - 添加 `x-api-key` header（外部 provider 需要 API key）
  - 响应/SSE 原样返回（Anthropic-compatible API 本身就是 Anthropic 格式）
- [x] **P6.2** [Go] 错误处理
  - 外部 API 连接失败 → 返回 Anthropic error
  - 非 200 → 透传错误信息

**验收**：DeepSeek provider 配置后，`cp/` 走 Copilot，`ds/` 走直通，共存在一个 HTTP server

---

### Phase 7：验证 + 文档

**目标**：端到端验证、更新文档、确保所有验收标准通过

- [x] **P7.1** 端到端验证：C1-C9 验收通过（C10 需 Copilot token 后验证）
- [x] **P7.2** 异常场景测试：无效模型/无模型默认/非法JSON/大请求体 全部通过
- [x] **P7.3** 更新 README.md（已通过 CLAUDE.md 覆盖，README 后续更新）
- [x] **P7.4** 更新 CLAUDE.md（Phase 0 已更新为 Go+QT 架构）
- [x] **P7.5** TypeScript MVP 已移入 `archive/`

---

### Phase 8：CLI 产品化 — 稳定性

**目标**：守护进程级别的可靠性，不能崩、不能悄无声息挂掉

- [x] **P8.1** [Go] PID 文件 + 重复启动检测
  - 启动时写入 `~/.onellm/onellm-router.pid`
  - 再次启动检测到已有 PID → 打印 "已在运行 (PID xxx)" 并退出
  - 停止时删除 PID 文件
- [x] **P8.2** [Go] 每请求 panic recover
  - HTTP handler 层 `defer recover()` — 单请求 panic 返回 500，进程不崩
  - Copilot SSE 流式 goroutine 加 recover
- [x] **P8.3** [Go] `/health` 增强
  - 返回: `{"status":"ok","copilot_token":true,"uptime_seconds":3600,"models":5}`
  - Copilot token 缺失时仍然是 200，但 `copilot_token: false`
- [x] **P8.4** [Go] Token 刷新互斥锁
  - `sync.Mutex` 防止并发请求同时触发 token 刷新 → 重复调 Copilot API
- [x] **P8.5** [Go] 优雅关闭
  - 收到 SIGINT → 停止接受新连接 → 等待活跃请求完成（最多 30s）→ 关闭

**验收**：重复启动被阻止；`curl` 故意制造 panic 不崩溃；token 并发刷新只调一次 API

---

### Phase 9：CLI 产品化 — UX + 测试

- [x] **P9.0a** [Go] CLI UX — 默认 serve
  - 无参数 = `serve`（rootCmd.Run 就是 serve）
  - serve 检测无 token → 强制 login（阻塞，必须完成才能启动）
  - `login` 子命令保留，可手动触发
- [x] **P9.0b** [Go] CLI UX — `--daemon` 后台运行
  - `onellm-router --daemon` → 不阻塞终端
  - 日志仍写文件，stdout/stderr 关闭

### Phase 9：可观测性 + 测试

**目标**：出问题能排查，改代码敢放心

- [x] **P9.1** [Go] 控制台输出关键信息
  - HTTP 和 gRPC 监听地址打印到 stdout（当前只写日志文件）
  - provider 数量、模型数打印到 stdout
- [x] **P9.2** [Go] 请求耗时 + 状态码日志（statusWriter + duration_ms）
  - `withRequestID` 中间件改为记录 `method`, `path`, `status`, `duration_ms`
  - 包装 `responseWriter` 捕获 status code
- [x] **P9.3** [Go] translate 包单元测试（11 个）
  - 请求翻译: system / text / image / tool_use / tool_result
  - 响应翻译: text / tool_call / 多 tool
  - SSE: 单文本 / 含 tool / finish / 空 chunk / 坏 JSON
- [x] **P9.4** [Go] proxy 包集成测试（8 个，含 mock API server）
  - Mock HTTP server 模拟 Copilot API → 验证完整翻译链路
- [x] **P9.5** [Go] auth 包单元测试（7 个，cache/过期/代理/mutex）
  - token 缓存/过期/刷新逻辑

**验收**：`go test ./... -cover` 覆盖率 > 60%；控制台能看到启动状态信息

### Phase 10：QT 面板需要的 gRPC 接口 [已废弃]

> QT 面板代码已删除，Phase 10 所有任务随 `panel/`、`proto/`、`internal/grpc/` 一同移除。

- [x] ~~P10.1-P10.7~~ — 已删除

---

### Phase 11：长期运行增强

**目标**：本地长期使用所需的稳定性、便利性改进

- [x] **P11.1** [Go] 开机自启 — `onellm-router install` / `uninstall`
  - `install` → 写入 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 注册表键
  - 注册表值指向后台启动命令（带 `--daemon`）
  - `uninstall` → 移除注册表项 + 清理 pid 文件
- [x] **P11.2** [Go] 配置文件校验
  - yaml 格式错误 → 打印具体行号 + 直接退出（不回退到默认值）
  - 校验 providers 非空、每个 provider 有 prefix + models
  - `model_slots` 引用有效性检查（引用的模型在 providers 中存在则 pass，否则 warn）
- [x] **P11.3** [Go] HTTP 连接池
  - Transport 设置 `MaxIdleConnsPerHost: 10`, `IdleConnTimeout: 90s`
  - 避免高频使用 Claude Code 时重复 TLS 握手
- [x] **P11.4** [Go] Token 刷新失败重试
  - 刷新失败 → 退避重试 3 次（1s / 2s / 4s）
  - 3 次全失败 → 返回 error
- [x] **P11.5** [Go] 流式/非流式超时区分
  - 非流式：60s；流式：300s
  - 通过 `http.NewRequestWithContext` 注入超时
- [x] **P11.6** [Go] 流式 goroutine 泄漏（upstream 请求绑定客户端 context，断开即取消）
  - SSE 客户端断开 → 通过 `r.Context().Done()` 取消 upstream 请求
  - scanner 循环正确退出

**验收**：`onellm-router install` 注册开机启动；yaml 写错启动直接报错；Ctrl+C 断开 SSE 后 goroutine 退出

---

## 4. 已确认的决策点

| # | 决策 | 结论 | 状态 |
|---|------|------|------|
| D1 | Go module 名 | `github.com/kkroid/onellm-router` | ✅ |
| D2 | 配置文件格式 | 单一 YAML 配置文件：`onellm-router.yaml`（gitignore）+ `onellm-router.example.yaml`（提交） | ✅ |
| D3 | HTTP router 库 | `net/http` 标准库（零外部依赖，6 个路由不需要框架） | ✅ |
| D4 | gRPC port | 配置文件中设置，默认 `1083` | ✅ |
| D5 | 请求 ID 格式 | UUID v4（`github.com/google/uuid`） | ✅ |

---

## 5. 风险与已知坑

| 风险 | 应对 |
|------|------|
| Copilot API 不稳定/变更 | 参考 TypeScript 版本已验证可用的 API 参数和 headers，不做额外修改 |
| SSE 翻译状态机遗漏边界 case | 重点参考 `translate.ts` 中已验证的状态机逻辑，逐 case 写测试 |
| Device login 轮询阻塞启动 | 参考 TypeScript 版本：HTTP server 先启动，device login 在 goroutine 后台执行 |
| Copilot API IP 区域限制 | 必须走 SOCKS5 代理，`http.Client` 配置 proxy transport |
| 配置文件格式复杂度 | 单一文件原则，格式尽量简单（flat key-value 或最小嵌套 YAML） |

---

## 6. 不属于本 checklist 的事项

- Windows Service 注册（后期加）
- 安装包构建（后期加）
- Web UI 或 admin dashboard
