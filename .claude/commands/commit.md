---
description: 生成符合项目规范 + Conventional Commits 格式的 Git 提交信息
allowed-tools: Bash(git:*), Read(*)
---

## 提交信息生成

先生成 Conventional Commits 格式的提交信息，然后提供 `git commit` 命令。

### 1. 检查当前状态

!`git status --short`
!`git diff --stat HEAD`
!`git branch --show-current`

### 2. 生成提交信息

根据上述变更，生成符合以下规范的提交信息。

**格式要求：**
```
<type>(<scope>): <简短描述>

<详细说明（可选）>

Generated with [Claude Code](https://claude.ai/code)
via [Happy](https://happy.engineering)

Co-Authored-By: Claude <noreply@anthropic.com>
Co-Authored-By: Happy <yesreply@happy.engineering>
```

**Type 类型：**
- `feat`: 新功能
- `fix`: 修复 bug
- `docs`: 文档变更
- `style`: 代码风格（不影响功能）
- `refactor`: 重构（不改变功能）
- `perf`: 性能优化
- `test`: 测试相关
- `build`: 构建系统或外部依赖
- `ci`: CI 配置
- `chore`: 其他杂项

**Scope 范围（本项目）：**
- `onellmd`: Go 后台守护进程（代理核心、路由、协议翻译）
- `panel`: QT/C++ 桌面管理面板
- `proto`: gRPC/protobuf 接口定义
- `build`: 构建系统（Makefile, CMake）
- `config`: 配置文件（onellmd.yaml, .env）
- `docs`: 文档
- `ts-proxy`: TypeScript MVP（过渡期，后续废弃）

### 3. 提供提交命令

生成提交信息后，提供下一步的 `git commit -m "..."` 命令供用户执行。

> **注意**：不要自动执行 `git commit`，先展示提交信息让用户确认。
