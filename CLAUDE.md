# OneCCRouter Claude Code 开发指南

## 项目定位

**个人 AI 模型路由网关** — 将 GitHub Copilot Claude 模型 + 任意 Anthropic-compatible API 统一暴露为单一 Anthropic 接口，供 Claude Code 等工具使用。

- 每人独立部署一套，单机运行
- 不追求高并发（几个工具同时调用）
- **稳定性第一**：不能崩、不能内存泄漏、异常可复现
- **可排查性好**：日志日期滚动、错误栈完整、请求可追踪

## 架构

```
Claude Code CLI / VS Code / 其他工具
              │
              ▼  Anthropic Messages API (localhost)
    ┌─────────────────────────┐
    │   OneCC Proxy (Go)      │  ← 后台守护进程（核心）
    │   · API 路由            │
    │   · 协议翻译             │
    │   · gRPC Server         │
    └──────┬──────────────────┘
           │ gRPC (localhost)
           ▼
    ┌─────────────────────────┐
    │   OneCC Panel (QT/C++)  │  ← 桌面管理面板
    │   · 模型管理             │
    │   · Provider 配置        │
    │   · 请求日志             │
    │   · 系统托盘             │
    └─────────────────────────┘
```

**参考架构**：[CloudiaLauncher](E:/code/cloudia540_390/CloudiaLauncher) — 多进程分离、gRPC 通信、QT 面板 + 后台服务的模式。

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
| 后台守护进程 | **Go** 1.22+ | gRPC, protobuf, cobra, lumberjack | 代理核心 + API 路由 + 协议翻译 |
| 桌面管理面板 | **C++17** | **Qt 6.5** (Widgets), gRPC, spdlog | 配置编辑 + 状态监控 + 系统托盘 |
| IPC 通信 | protobuf | gRPC over localhost | 强类型、双向流、CloudiaLauncher 同款方案 |

### 为什么 Go 做后台而不是 C++？

- **稳定性**：GC + 内存安全，没有悬空指针/buffer overflow，守护进程长期运行更可靠
- **可排查性**：panic 自动输出完整 goroutine 栈；`context.Context` 天然携带 trace ID；error wrapping 递归展开错误链
- **网络代理是 Go 的主场**：`net/http` + goroutine + channel 写代理/转发/SSE 流式数据比 C++ 少 3-5 倍代码量，更不容易出错
- **部署简单**：单个 ~10MB exe，无运行时依赖，复制就能跑
- **低并发**：不需要手写 epoll/iocp，goroutine 调度器足够应对几个工具的并发调用

## 项目结构（目标）

```
OneCCRouter/
├── CLAUDE.md                     # 本文件
├── Makefile                      # 顶层构建编排
│
├── cmd/
│   └── oneccd/                   # Go 后台守护进程入口
│       ├── main.go               # 启动入口
│       ├── server.go             # gRPC server + HTTP proxy server
│       ├── proxy/                # Anthropic API 代理核心
│       │   ├── handler.go        # 请求路由 → Copilot / 外部 provider
│       │   ├── translate.go      # Anthropic ↔ OpenAI 协议翻译
│       │   └── stream.go         # SSE 流式转发
│       ├── router/               # Provider 注册 + 模型解析
│       │   ├── provider.go       # Provider 结构 + 多 provider 管理
│       │   └── resolver.go       # 模型名 → provider 解析
│       ├── config/               # 配置管理
│       │   ├── config.go         # 从配置文件/环境变量加载
│       │   └── provider.go       # PROVIDER_N_* 解析（复用 .env 格式）
│       └── log/                  # 日期滚动日志
│           └── log.go            # lumberjack 配置 + slog 结构化输出
│
├── proto/                        # protobuf 定义
│   └── onecc/
│       └── v1/
│           └── service.proto     # PanelService + ProxyService
│
├── panel/                        # QT/C++ 桌面面板
│   ├── CMakeLists.txt
│   ├── src/
│   │   ├── main.cpp              # 入口：gRPC client + QApplication
│   │   ├── main_window.h/cpp     # 主窗口（设置、监控）
│   │   ├── tray_icon.h/cpp       # 系统托盘（状态指示 + 菜单）
│   │   ├── proxy_client.h/cpp    # gRPC client → Go 后台
│   │   ├── model_page.h/cpp      # 模型管理页面
│   │   ├── provider_page.h/cpp   # Provider 配置页面
│   │   └── log_view.h/cpp        # 请求日志查看
│   └── resources/                # 图标、QSS 样式
│
├── third_party/                  # 第三方依赖
│   ├── grpc/                     # gRPC + protobuf（参考 CloudiaLauncher）
│   └── qt/                       # Qt 6.5.3（仅 Core, Gui, Widgets）
│
├── .env.example                  # Provider 配置模板
├── oneccd.yaml                   # 后台服务配置（端口、日志、代理等）
│
└── docs/                         # 设计文档
    └── architecture.md           # 架构设计细节
```

### 当前状态（过渡期）

> ⚠️ 当前 `copilot-anthropic-proxy/` 中的 TypeScript 代码是 **流程验证 MVP**，仅用于跑通端到端链路（Anthropic API 路由 + 协议翻译 + Copilot 认证）。该代码将在 Go 后台搭建完成后废弃。

## Claude Code 项目斜杠命令

| 命令 | 文件 | 用途 |
|------|------|------|
| `/build` | `.claude/commands/build.md` | Go 编译 + CMake QT 编译 + 安装包构建 |
| `/commit` | `.claude/commands/commit.md` | Conventional Commit + Co-Authored-By 签名 |
| `/review` | `.claude/commands/review.md` | Go + C++ 双语言代码审查 |
| `/plan` | `.claude/commands/plan.md` | 任务分解 + 验证标准定义 |

### Claude Code 技能

| 技能 | 调用方式 | 用途 |
|------|---------|------|
| `checklist` | `/checklist` | 大任务分解为结构化 checklist，逐项推进勾选 |
| `audit` | `/audit` | 阶段性自我审核——审查代码/结果/文档，发现问题自动修复 |

---

## 核心行为准则

> 以下原则源自 Andrej Karpathy 对 LLM 编码陷阱的观察，按本项目特点调整。

### 1. 编码前思考 — 不假设、不隐藏困惑、呈现权衡

动手前必须：
- **明确陈述假设**。如果不确定，**必须先问**，不要默默选择一种理解。
- 如果存在多种解释，**全部列出**，不要替我选。
- 如果存在更简单、更稳妥的做法，**大胆指出并给出建议**。
- 如果我的需求有问题，**必须及时指出**，不要盲目执行。
- 如果任务中途发现计划有问题，**及时中断并说明原因**，让我重新决策。

### 2. 简洁优先 — 用最少代码解决问题，不做推测性开发

- 用能解决问题的最少代码完成任务，**不添加需求之外的功能**。
- **不为一次性逻辑创建抽象**，不添加未要求的灵活性、可配置性或复杂框架。
- 不为不可能发生或没有证据表明会发生的场景堆叠防御代码。
- 如果实现明显可以更短、更清晰，**主动收敛复杂度**。

### 3. 精准修改 — 只碰必须碰的，只清理自己造成的混乱

- **只修改完成当前请求必须修改的内容**；不要顺手重构、改格式、改注释或清理无关代码。
- **匹配项目现有风格**，即使你个人更倾向于另一种写法。
- 对自己没有充分理解的现有代码，**不要做旁路改动**。
- 如果发现无关的死代码或历史问题，**可以汇报，但不要擅自删除**。
- 清理**仅限于本次改动造成的**未使用导入、变量、函数或文件。

### 4. 验证闭环 — 定义成功标准，循环验证直到达成

- 每步必须有可验证的输出（编译通过？gRPC 调用返回正确？代理请求正常？）
- 完成明确步骤后，**汇报结果、风险、验证情况和建议的下一步**。
- 如果验证失败，先基于证据定位原因。

---

## 技术约定

### Go 后台（`cmd/oneccd/`）

| 约定 | 说明 |
|------|------|
| 版本 | Go 1.22+ |
| 模块 | `go mod`，模块名待定 |
| CLI 框架 | `cobra`（子命令） + `viper`（配置绑定） |
| 日志 | `log/slog` + `lumberjack`（日期滚动：按天切分，保留 30 天，单文件最大 100MB） |
| 请求 ID | 每个请求生成 UUID，通过 `context.Context` 传递，日志全部带 request_id |
| 错误处理 | `fmt.Errorf("...: %w", err)` 包装错误链，绝不吞错误 |
| 代理 | 尊重 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量；Copilot API 走 SOCKS5 代理 |
| gRPC | `google.golang.org/grpc`，监听 `127.0.0.1` 仅本地 |
| HTTP | `net/http` 标准库 + `httputil.ReverseProxy`（如适用） |
| 测试 | `go test`，表驱动测试 |

#### 日志格式规范

```json
{"time":"2026-06-11T10:30:00.000+08:00","level":"INFO","msg":"proxy request","request_id":"a1b2c3d4","method":"POST","model":"cp/claude-opus-4.8","duration_ms":1234,"status":200}
```

### QT/C++ 面板（`panel/`）

| 约定 | 说明 |
|------|------|
| 标准 | C++17 |
| QT | Qt 6.5.3（仅 Core, Gui, Widgets） |
| 构建 | CMake 3.20+，Visual Studio 2022 generator，x64 |
| gRPC | `third_party/grpc/`，参考 CloudiaLauncher 的 proto 编译和链接方式 |
| 日志 | `spdlog`（`rotating_file_sink`，与 Go 侧日志格式对齐） |
| 托盘 | `QSystemTrayIcon`，窗口关闭 → 隐藏到托盘，托盘菜单退出 → 真正关闭 |
| 样式 | QSS 主题（dark/light，参考 CloudiaLauncher 的 qdarkstyle） |
| 异步 | gRPC 调用在独立 `std::thread`，通过 `QMetaObject::invokeMethod(Qt::QueuedConnection)` 回到主线程 |

> 参考 CloudiaLauncher 但不照搬：不需要 frameless 窗口、不需要 Pimpl 惯用法（除非确实需要编译防火墙）、不需要 Job Object 进程管理（Go 和 QT 是两个对等进程，不是父子关系）。

### protobuf / gRPC（`proto/`）

| 约定 | 说明 |
|------|------|
| 语法 | proto3 |
| 编译 | `protoc` + `protoc-gen-go` + `protoc-gen-grpc-go`（Go 侧）；`protoc` + `grpc_cpp_plugin`（C++ 侧） |
| 通信 | `grpc.WithInsecure()`（localhost only，无 TLS 需求） |
| 服务 | `ProxyService`（QT 调用 Go 的配置/状态接口）+ 可选 `UiService`（Go 推送状态变更到 QT） |

### 构建命令

```bash
# Go 后台
cd cmd/oneccd && go build -o ../../build/oneccd.exe .

# QT 面板
cmake -B build/panel -G "Visual Studio 17 2022" -A x64
cmake --build build/panel --config Release --parallel

# 安装包（后续）
# 将 build/oneccd.exe + build/panel/Release/onecc-panel.exe + Qt DLL + resources 打包
```

### 配置文件

- `oneccd.yaml`：Go 后台配置（监听端口、日志路径、代理设置）
- `.env`：Provider 配置（复用当前格式，`PROVIDER_N_*`）
- `~/.onecc/`：用户数据目录（token 缓存、运行时日志、崩溃 dump）

---

## 关键限制与约束

1. **稳定性 > 功能**：新功能可以慢慢加，但不能引入崩溃或内存问题。
2. **每个请求可追踪**：日志必须带 request_id，出问题时能复现完整请求链路。
3. **协议翻译必须精确**：Anthropic ↔ OpenAI 语义等价，不丢失字段。
4. **仅本地通信**：gRPC 和 HTTP API 都只监听 `127.0.0.1`，不暴露到网络。
5. **Provider 配置向后兼容**：`.env` 格式保留，确保当前用户可以无缝切换。
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
# 1. Go 后台启动
./build/oneccd.exe --config oneccd.yaml
# 预期日志：[INFO] oneccd starting on 127.0.0.1:3456

# 2. 健康检查
curl http://localhost:3456/health
# 预期：{"status":"ok"}

# 3. 模型列表
curl http://localhost:3456/v1/models

# 4. 非流式代理请求
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 5. 流式请求
curl -N -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'

# 6. gRPC 调用（QT 面板对接）
grpcurl -plaintext -d '{"action":"list_models"}' 127.0.0.1:9090 onecc.v1.ProxyService/Call

# 7. QT 面板启动
./build/panel/Release/onecc-panel.exe
# 预期：托盘图标出现，点击打开主窗口，能看到模型列表和 provider 状态
```
