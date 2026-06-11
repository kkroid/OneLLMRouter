---
description: Go + C++ 双语言代码审查，聚焦 Go 错误处理/goroutine 安全 + C++ 内存安全/gRPC 正确性
allowed-tools: Read(*), Grep(*), Glob(*), Bash(git:*)
argument-hint: [file1.go file2.cpp ...] [--full]
---

## 代码审查

对指定的文件或当前变更进行审查。支持 Go 和 C++ 双语言。

### 审查维度

按以下优先级逐项检查：

**1. 正确性（最高优先级）**

Go 专项：
- 错误处理：是否有 `_ = err` 吞错误？error 是否通过 `%w` wrap 保留调用链？
- goroutine 安全：是否有 goroutine 泄漏（没加退出条件）？共享变量是否加锁或 channel 同步？
- context 使用：是否正确传递 `context.Context`？超时控制是否到位？
- 资源释放：`defer resp.Body.Close()` 是否遗漏？HTTP client 是否复用连接？

C++ 专项：
- 内存安全：是否有 use-after-free、double-free、buffer overflow？
- RAII：资源是否通过构造/析构管理？有没有裸 `new`/`delete`？
- gRPC 调用：`grpc::ClientContext` 生命周期是否正确？deadline 是否设置？
- 线程安全：gRPC worker thread → Qt main thread 的 callback 是否通过 `QMetaObject::invokeMethod` marshaling？

协议翻译专项：
- Anthropic ↔ OpenAI 字段映射是否完整？有没有漏掉的字段？
- SSE chunk 转换的状态机是否正确？`content_block_start/stop` 是否配对？
- 流式响应结束时是否正确发送 `message_stop`？

**2. 稳定性**

- 是否有 panic 风险（Go: nil pointer dereference / slice out of bounds；C++: 空指针/未初始化变量）？
- 是否有潜在的 goroutine/thread 泄漏（长时间运行后内存/线程数持续增长）？
- 日志是否带 request_id？异常路径是否有日志？
- 单次请求失败是否会影响其他请求？

**3. 项目风格一致性**

Go：
- 是否遵循 Effective Go 惯例（camelCase、Tab 缩进、导出名首字母大写）？
- 包结构是否合理（按功能分包，避免 util/common 大杂烩）？

C++：
- 命名：类名 PascalCase、函数 camelCase、常量 UPPER_SNAKE？
- 是否匹配项目现有风格？
- Qt 部分：是否避免 `Q_OBJECT` 宏开销（轻量场景用 lambda + `QObject::connect`）？

**4. 简洁性**

- 是否有过度抽象？Go: 不要为 3 个 provider 写工厂模式；C++: 不要为一次性逻辑建抽象基类
- 是否有可以简化的冗余逻辑？
- 是否有死代码或未使用的变量/导入？

### 输出格式

```
## 审查结果：<文件名>

### 🔴 必须修复（正确性/稳定性问题）
- <问题描述> @ <行号或范围>

### 🟡 建议改进（风格/简洁性）
- <问题描述> @ <行号或范围>

### 🟢 通过项
- <通过的检查项>

### 📝 备注
- <其他观察>
```

### 参数说明

- 无参数：审查当前 `git diff` 变更
- `file1.go file2.cpp`：审查指定文件
- `--full`：审查整个文件，不限于 diff
