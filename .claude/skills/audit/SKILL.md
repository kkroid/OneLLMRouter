---
name: audit
description: 阶段性自我审核。在每个 Phase 完成后自动触发（或通过 /audit 手动调用），审查所有代码和文档修改，发现问题后自动修复，直到通过才能继续下一步。
---

# 阶段性自我审核

在每个任务阶段完成后，必须执行自我审核。审核通过后才能进入下一阶段。

---

## 审核流程

```
Phase 完成
    │
    ▼
[1] 代码审查 ──→ 发现问题？──→ 修复 ──→ 回到 [1]
    │ (通过)
    ▼
[2] 功能验证 ──→ 发现问题？──→ 修复 ──→ 回到 [2]
    │ (通过)
    ▼
[3] 文档对齐 ──→ 发现问题？──→ 修复 ──→ 回到 [3]
    │ (通过)
    ▼
✅ 审核通过，进入下一阶段
```

**核心原则**：发现问题不准跳过。每一轮修复后必须重新审查该项，直到通过。

---

## [1] 代码审查

### 1.1 改动范围
- 检查 `git diff --stat`，确认改动仅限相关文件
- 是否有调试代码残留（`fmt.Println`、`console.log`、`qDebug()`、`// TODO debug`、注释掉的代码）？
- 是否有临时注释掉的代码块？

### 1.2 Go 专项（如涉及）
- 错误处理：是否有 `_ = err`？error 是否 wrap 保留调用链？
- goroutine 安全：是否有泄漏风险？共享变量是否保护？
- context 传递：超时控制是否到位？
- 日志：是否带 request_id？关键路径是否打日志？

### 1.3 C++ 专项（如涉及）
- 内存安全：use-after-free、double-free、buffer overflow
- RAII：裸 new/delete 是否合理？
- gRPC：ClientContext 生命周期 + deadline
- 线程安全：worker thread → Qt main thread marshaling

---

## [2] 功能验证

### 2.1 Go 后台
```bash
go build -o build/oneccd.exe ./cmd/oneccd/
./build/oneccd.exe &
sleep 2

# 健康检查
curl http://localhost:3456/health

# 代理请求
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# gRPC
grpcurl -plaintext 127.0.0.1:9090 list
```

### 2.2 QT 面板（如涉及）
```bash
# 编译
cmake --build build/panel --config Release

# 启动面板
./build/panel/Release/onecc-panel.exe
# 验证：托盘图标显示正常、主窗口可打开、模型列表显示正确
```

### 2.3 错误路径
- 无效模型名 → 400 + 可用模型列表
- Go 后台未启动时 QT 面板的提示
- 代理连接失败时 Go 的日志和错误响应

---

## [3] 文档对齐

### 3.1 变更同步
- README.md 是否反映最新变更？
- .env.example 是否包含新增配置项？
- CLAUDE.md 是否需要更新技术约定？
- proto 变更是否在 proto 文件中注释说明？

### 3.2 提交信息
- 格式是否符合 Conventional Commits？
- Scope 是否正确（oneccd / panel / proto / build / config / docs）？
- 是否包含 Co-Authored-By 签名？

---

## 审核输出的格式

审核完成后输出简表：

```
## Phase X 审核

| # | 检查项 | 结果 | 备注 |
|---|--------|------|------|
| 1.1 | 改动范围 | ✅ | N 文件，仅限相关模块 |
| 1.2 | Go 代码质量 | ✅ | 无吞错误、goroutine 安全 |
| 1.3 | C++ 代码质量 | ✅ | 无内存问题 |
| 2.1 | Go 后台验证 | ✅ | 健康检查 + 代理请求正常 |
| 2.2 | QT 面板验证 | ✅ | 面板显示正常 |
| 3.1 | 文档同步 | ✅ | CLAUDE.md 已更新 |

审核通过 ✅ / 待修复 ❌（见下）
```

发现问题时，在表格后列出修复清单，修完后重新跑对应检查项。
