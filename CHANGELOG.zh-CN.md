# 更新日志

这里记录 OneLLMRouter 面向使用者的重要变更。

## [未发布]

## [1.5.2] - 2026-08-20

### 新增

- 新增托盘重试反馈：上游重试时显示黄色图标和正在重试的客户端模型，并支持并发重试展示。

### 修复

- 为 Anthropic 客户端增加不含方括号的安全别名，例如 `deepseek-v4-flash-1m`，通过 `upstream_model` 映射到包含 `[1m]` 的上游模型名，避免 Claude Code 在请求前剥离后缀。

### 变更

- `/health` 新增当前重试模型字段，不改变原有 Retry 策略。

## [1.5.1] - 2026-08-17

### 新增

- 新增按端点声明 Provider 模型，端点归属必须明确，并支持可选的上游模型名。
- 托盘健康状态提示现在显示正在运行的 Core 版本。
- 新增包含起止日期的 `stats range START END` 统计，以及 Usage 页自动查询的今天、本月和自定义区间筛选。

### 变更

- Usage Token 总量使用紧凑的 K/M/B 格式，tooltip 保留精确值；上游未返回的 Token 字段不再增加表格噪音。

### 修复

- 移除安装完成页的启动动作，升级时只由 Windows Restart Manager 恢复一次正在运行的托盘。
- 历史 Usage 记录缺少请求模型时，月度和自定义区间结果不再被整体清空。

## [1.5.0] - 2026-08-11

### 新增

- 为 Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses 的直通、翻译、流式和非流式路径新增逐 attempt Usage 采集。记录保留未知 token 字段，并通过稳定的 request ID 和从 1 开始的上游尝试序号关联重试。
- 新增按 Provider、请求模型和上游模型分组的 `stats day`、`stats week`、`stats month` 表格/JSON 统计，以及由 Core 配置与统计契约驱动的 Qt Providers、Clients 和 Usage 页面；模型管理位于 Providers 页面内。
- 新增 Qt Clients 页面和 Core 命令，用于 Claude Code 受控 managed-key 合并、单层备份和精确恢复；不修改无关 Claude 偏好，也不会写入上游 Provider API Key。
- 新增 Codex TOML 只读状态、可复制配置预览、确定性 catalog 同步和仅用于显示的 `OneLLMRouter` 来源标识。v1.5.0 不写入 `config.toml`，也不提供原始 TOML/catalog 编辑。
- 新增 Windows、Linux、macOS 的 Go 与 Qt 构建/测试门禁。发行产物仍仅提供 Windows x64；Linux/macOS 安装包、开机自启、daemon 和应用重启集成未交付。

### 变更

- 保持现有同 Provider 重试默认值和流式输出开始后不重放的边界。Usage 现在覆盖成功、重试耗尽、客户端取消和服务关闭 attempt，不改变 Retry 参数。
- MCP/Skill/Prompt 管理、Auto Failover、原始客户端文件编辑和无关偏好编辑仍不属于 v1.5.0 范围。

### 修复

- OpenAI Responses 流在输出开始前遇到模型容量错误时，现在会按配置的上游重试策略处理；重试未恢复时返回最后一次原始 SSE 失败，并且绝不重放已经开始的输出。
- Anthropic、OpenAI Chat Completions 和 OpenAI Responses 原生直通路由现在会原样返回最后一次上游 HTTP 错误的状态码、响应体和端到端响应头，不再包装为 OneLLMRouter 错误；传输失败和协议翻译仍使用路由器生成的错误。

## [1.4.1] - 2026-08-06

### 修复

- OpenAI-compatible Chat Completions 直通转发现在仅改写路由后的模型名，并保留 `response_format`、`thinking`、严格工具定义及未来的 provider 特有字段。

## [1.4.0] - 2026-08-03

### 新增

- 新增 Windows Qt 6 系统托盘，提供中英文状态文案、彩色状态图标、配置与日志快捷入口、代理可达性，以及对自有 core 的启动、停止和重启操作。
- 新增按用户安装的 Inno Setup 安装包，支持可选的登录时启动、配置保留、本地携带 Qt 和 MSVC 运行库，以及基于 Windows Restart Manager 的安全升级。
- 新增稳定的 `/health` 身份字段和不包含敏感信息的 `config-info --json` 桌面发现契约。
- 为 Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses 新增统一且有边界的上游重试策略。需要重试的 HTTP 状态码可显式配置，默认不包含 `403`。
- 新增重试尝试、恢复、取消、跳过和耗尽的结构化日志，并对 credential 做脱敏。
- 新增固定第三方 action 版本的 GitHub Actions 发布流水线，用于构建和验证便携版及 Windows Setup 安装包。

### 变更

- Go 便携版不再内置原生托盘；桌面进程管理统一由 `onellm-router-tray.exe` 负责。
- 模型推理重定向现在作为上游响应处理，确保重试行为明确且严格由配置控制。
- 未知 Codex 模型现在使用合法的回退指令和 reasoning 预设，不再继承不兼容的 model messages。

### 移除

- 完全移除 GitHub Copilot 的认证、token 存储、provider 特殊行为、界面和配置支持。`cp` 等前缀现在只是普通的用户自定义前缀，不再具有内置含义。

### 修复

- 强化托盘进程所有权检查：外部启动的 Router 只读附着，绝不终止无关端口监听进程。
- 修复托盘子进程退出、重启取消、启动失败、端口冲突、旧开机自启迁移和运行中安装升级。
- 修复重试取消和超时边界，客户端断开或服务关闭时会及时停止待处理工作，且不会生成误导性的上游错误。

[1.5.2]: https://github.com/kkroid/OneLLMRouter/compare/v1.5.1...v1.5.2
[1.5.1]: https://github.com/kkroid/OneLLMRouter/compare/v1.5.0...v1.5.1
[1.5.0]: https://github.com/kkroid/OneLLMRouter/compare/v1.4.1...v1.5.0
[1.4.1]: https://github.com/kkroid/OneLLMRouter/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/kkroid/OneLLMRouter/compare/v1.3.2...v1.4.0
