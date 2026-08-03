# 更新日志

这里记录 OneLLMRouter 面向使用者的重要变更。

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

[1.4.0]: https://github.com/kkroid/OneLLMRouter/compare/v1.3.2...v1.4.0
