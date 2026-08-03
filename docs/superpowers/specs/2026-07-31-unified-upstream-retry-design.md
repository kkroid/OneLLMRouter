# OneLLMRouter 统一上游错误重试需求

> 实施状态：功能已在面向 v1.4.0 的 `master` 中完成；第 16 节保留为历史实施清单，最终发布验证、tag 和 GitHub release 尚未完成。

## 1. 背景

OneLLMRouter 当前将 Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses 请求转发给不同上游供应商。模型请求收到上游 HTTP 错误或网络错误后会立即返回客户端。Codex 自身虽然会重试部分错误，但重试次数有限；超过其内部上限后，Paseo 中的整个 agent turn 会失败并中断。

第三方转发供应商的 `429`、`502` 等错误通常是短暂故障，稍后使用相同请求再次调用即可恢复。因此，OneLLMRouter 需要在模型请求离开本机后、错误返回客户端前，统一执行有限时间的自动重试。

本需求优先保证改动小、协议安全和行为可预测。重试期间不向模型流注入自定义文本；只有最终无法恢复时，客户端才收到包含重试摘要和最后一次上游错误原文的增强错误。

## 2. 目标

- 为所有模型推理上游建立一套统一的重试机制。
- 对配置中明确列出的上游 HTTP 状态执行统一重试，未列出的状态立即返回。
- 对发送请求期间发生的网络和传输错误执行相同的重试策略。
- 单次重试等待最长约 30 秒，整个重试阶段最长约 5 分钟。
- 重试成功后保持原有协议和响应内容，客户端无需感知中间失败。
- 最终失败时，让 Paseo、Codex、Claude 等客户端直接看到重试摘要和最后一次上游错误。
- 保持流式响应实时透传，不建设跨协议的虚拟响应流或消息编排器。

## 3. 非目标

- 不修改 Paseo、Codex CLI、Claude Code 或任何上游供应商。
- 不在重试过程中向前端实时显示“正在重试”消息。
- 不向 assistant 输出或模型上下文注入 OneLLMRouter 状态文本。
- 不保证对已经开始向客户端输出的流进行续传、回滚、去重或重新生成。
- 不为不同供应商、模型或状态码提供独立重试策略。
- 不将本次机制用于模型目录发现、Codex catalog 生成、健康检查、配置发现、托盘轮询或代理可达性探测。
- 不引入外部重试库。

## 4. 适用范围

统一重试适用于以下模型推理路径：

- Anthropic Messages 请求。
- OpenAI Chat Completions 请求。
- OpenAI Responses 请求。
- 外部供应商的直连请求。
- Anthropic 与 OpenAI 之间经过协议翻译的请求。
- 流式和非流式请求，但遵守第 7 节的流式边界。

重试对象仅限 OneLLMRouter 对上游模型供应商发起的 HTTP 请求。客户端请求进入 Router 之前或 Router 本地处理阶段产生的错误不属于上游错误。

OneLLMRouter 已完全移除 GitHub Copilot 支持。本设计不得重新引入 Copilot 模型、token、登录、特殊 provider prefix 或兼容分支。

### 4.1 推理请求重定向策略

模型推理请求必须禁用 `http.Client` 的自动重定向。推理专用 client view 的 `CheckRedirect` 返回 `http.ErrUseLastResponse`，使 `3xx` 作为真实的非 `2xx` 响应进入统一重试。该设置不得改变模型目录发现或其他非推理 HTTP 客户端的重定向行为。

## 5. 统一重试条件

### 5.1 必须重试

在尚未向客户端写出响应的前提下，出现以下任一情况必须进入统一重试：

1. 上游返回 `retry.status_codes` 中明确配置的 HTTP 状态。
2. DNS 解析、TCP 连接、TLS 握手、SOCKS5 代理或连接重置等传输错误。
3. 等待上游响应头期间发生超时。
4. 非流式请求在读取完整响应体时发生错误，且尚未向客户端写出任何内容。

HTTP 状态判断必须严格服从配置，不解析或猜测第三方响应正文的业务含义。默认列表不包含 `403`；配置者显式加入后，Router 必须重试该状态并由配置者承担这一行为。

### 5.2 不得重试

以下情况不得进入统一重试：

1. 客户端请求体无法解析或缺少必填字段。
2. 模型或供应商无法在本地解析。
3. Router 本地协议翻译失败。
4. 客户端取消请求、关闭连接或请求 context 已结束。
5. OneLLMRouter 正在关闭。
6. 流式请求已经接受上游 `2xx` 响应头，或非流式请求已经完整读取成功响应。
7. 重试次数或总时间已经达到上限。
8. 上游 HTTP 状态未出现在 `retry.status_codes` 中。

本地错误必须立即返回，不应因为统一重试而延迟五分钟。

## 6. 默认重试策略

全局默认配置如下：

```yaml
retry:
  enabled: true
  max_attempts: 15
  status_codes: [408, 409, 425, 429, 500, 502, 503, 504]
  initial_delay: 1s
  max_delay: 30s
  max_elapsed: 5m
  jitter: 0.2
  honor_retry_after: true
```

字段语义：

- `enabled`：是否启用模型上游统一重试。关闭时统一组件只执行一次请求且不等待，但仍负责资源关闭和协议兼容错误封装。
- `max_attempts`：最大上游调用次数，包含第一次调用；默认最多调用 15 次。
- `status_codes`：允许重试的 HTTP 状态码列表，严格按配置匹配。默认不包含 `403`；显式空列表表示不重试任何 HTTP 状态。该字段不影响传输、超时和响应体读取错误。
- `initial_delay`：第一次失败后的基础等待时间。
- `max_delay`：任意两次尝试之间的最大等待时间，默认 30 秒。
- `max_elapsed`：从第一次上游调用开始计算的最大重试阶段时长，默认 5 分钟。
- `jitter`：在计算出的等待时间上加入正负 20% 的随机抖动，降低并发请求同时重试造成的冲击。
- `honor_retry_after`：上游提供合法 `Retry-After` 时优先采用，但最终等待时间仍不得超过 `max_delay`，也不得突破 `max_elapsed`。

缺少字段时使用以上默认值。显式配置的零值或负值不能被静默替换成默认值；即使 `enabled: false`，非法配置仍应在启动阶段失败。

配置层需要一个只接受字符串的 YAML duration 类型，支持 `250ms`、`30s`、`5m` 等 Go duration 语法，并拒绝裸数字，避免误把数字解释为纳秒。验证规则为：

- `max_attempts >= 1`；
- `status_codes` 只能包含 `100..599` 范围内的非 `2xx` 状态，且不得重复；
- `initial_delay > 0`；
- `max_delay >= initial_delay`；
- `max_elapsed > 0`；
- `0 <= jitter <= 1`。

不含抖动时，默认退避序列约为：

```text
1s → 2s → 4s → 8s → 16s → 30s → 30s → ...
```

停止条件采用“最先达到者”：

- 请求成功；
- 达到 `max_attempts`；
- 达到 `max_elapsed`；
- 客户端取消；
- OneLLMRouter 关闭。

等待过程必须使用可被 context 取消的 timer，不得使用不可取消的裸 `time.Sleep`。

指数退避先计算并封顶基础值，再乘以 `[1-jitter, 1+jitter]` 内的均匀随机因子，最终再次限制在 `[0, max_delay]`。测试必须可注入时钟、等待函数和随机源。

合法的 `Retry-After` 同时支持非负整数秒和未来的 HTTP-date；过去、负数、溢出或格式错误的值回退到指数退避。`Retry-After` 不再叠加 jitter。最后一次允许的尝试之后不等待；如果本次等待将耗尽剩余 `max_elapsed`，直接使用最后错误结束，不做没有机会发起下一次请求的空等待。

`max_elapsed` 只限制错误恢复阶段。流式请求一旦获得可正常转发的成功响应，后续正常生成时间仍沿用现有流式超时规则，不受五分钟重试预算限制。

## 7. 流式请求边界

### 7.1 可以重试的流式错误

流式请求在以下阶段仍可重试：

- 建立上游连接失败。
- 等待上游响应头失败。
- 上游返回 `retry.status_codes` 中配置的非 `2xx` 状态。

此时 OneLLMRouter 尚未向客户端提交成功响应，可以关闭本次上游响应体、等待退避时间并重新建立请求。

### 7.2 不可以重试的流式错误

一旦 OneLLMRouter 接受上游 `2xx` 响应头，本次流式响应即视为已开始，不再等待第一个 SSE 事件，也不以是否已经写出下游字节作为重试条件。之后出现连接中断、首事件超时、空闲超时、畸形事件或缺少结束事件时，不得自动重新调用模型。

原因是客户端已收到的内容无法撤回，而重新调用模型可能产生不同文本或重复工具调用。此类错误继续使用现有断流处理和日志行为。

为了保持改动小，本需求不要求为了捕获首个 SSE 事件而缓存或重组上游流。流式请求收到成功响应头后即可沿用现有透传路径。

## 8. 请求重建与资源处理

每一次重试都必须创建新的上游 `http.Request` 和新的请求体 reader，不能复用已被消费的 request body。

每次尝试必须：

- 复用同一个客户端请求 context 或其派生 context。
- 重新设置认证头、内容类型、Accept 和供应商专用请求头。
- 在重试前关闭非空的上游响应体。
- 对错误响应正文设置读取上限，防止异常供应商返回过大内容。
- 保留最后一次失败的状态码、错误类型和响应正文，用于最终返回。

原有各端点的请求超时仍然生效。单次尝试不得超过剩余的 `max_elapsed` 时间。

### 8.1 Context 生命周期与双层超时

HTTP server 必须使用服务生命周期 base context。客户端断开会取消请求 context；OneLLMRouter 收到停止信号时必须先取消服务 context，再调用 `http.Server.Shutdown` 等待活动请求退出。因此，无论重试正在请求上游还是等待退避，都能被客户端取消或服务关机立即终止，且不得再向已取消的客户端写入新错误。

服务 base context 必须由 `context.WithCancelCause` 创建。关闭路径使用统一组件导出的 `upstream.ErrServiceShutdown` 作为 cause；客户端断开仍产生普通的 `context.Canceled`。Executor 通过 `context.Cause(requestContext)` 区分 `service_shutdown` 和 `client_cancel`；若两者竞争，以最先使请求 context 结束的 cause 为准。不得仅根据 `ctx.Err()` 推断取消来源。

`max_elapsed` 只约束失败恢复阶段，现有端点 timeout 改为每次尝试的上限：

- Anthropic 非流式：现有 external request timeout，默认 60 秒；
- Anthropic 流式响应头：现有 external stream timeout，默认 300 秒；
- Chat Completions 直连和翻译路径：现有 OpenAI request timeout，默认 120 秒；
- Responses 流式响应头和非流式：OpenAI request timeout，默认 120 秒。

每次失败尝试的实际限制取“端点 timeout”和“剩余 `max_elapsed`”的较小值。非流式限制包含完整响应体读取。流式请求获得 `2xx` 响应头后必须停止失败恢复 timer，使健康的长流不受五分钟预算影响；此后仅保留客户端/服务取消以及现有首事件和空闲超时。

实现不得把不可撤销的 `max_elapsed` deadline 绑定到成功流的 response body。可以使用可取消的子 context 加 timer，并在成功响应头边界停止 timer；response body 关闭时必须释放该子 context。

## 9. 成功行为

任意一次尝试获得成功后：

- 立即停止重试和退避计时。
- 流式请求继续使用当前 SSE 透传或翻译逻辑。
- 非流式请求继续使用当前响应解析、翻译和返回逻辑。
- 不向响应中添加“重试成功”“供应商恢复”等文字。
- 不修改模型返回的正文、工具调用、usage、finish reason 或事件顺序。
- 不把重试诊断信息写入模型上下文。

客户端只会看到一次正常响应。

## 10. 最终失败行为

达到停止条件且仍未成功时，OneLLMRouter 返回增强后的最终错误。

### 10.1 HTTP 状态

- 最后一次失败为非 `2xx` HTTP 响应时，使用最后一次上游状态码，包括 `3xx`。
- 最后一次失败为网络、传输或非超时的 body 读取错误时，返回 `502 Bad Gateway`。
- 最后一次失败为等待响应头或读取非流式 body 超时时，返回 `504 Gateway Timeout`。
- 客户端主动取消或服务关机时不生成新的错误响应。

尝试次数或 `max_elapsed` 耗尽时，必须保留最后一个真实失败的分类。`2xx` body 读取失败绝不能使用成功状态码返回错误正文。

### 10.2 错误文本

错误正文应包含：

- OneLLMRouter 标识。
- 上游供应商前缀或名称。
- 总尝试次数。
- 重试阶段总耗时。
- 最后一次错误的 HTTP 状态或传输错误。
- 最后一次上游错误响应原文的安全摘要。

示例：

```text
OneLLMRouter 上游请求最终失败。
供应商：c78
共尝试：15 次
总耗时：4m58s
最后错误：HTTP 429 Too Many Requests
上游响应：upstream capacity temporarily exhausted
```

网络错误示例：

```text
OneLLMRouter 上游请求最终失败。
供应商：ds
共尝试：9 次
总耗时：5m0s
最后错误：连接上游失败：connection reset by peer
```

Anthropic Messages 最终错误必须使用：

```json
{
  "type": "error",
  "error": {
    "type": "api_error",
    "message": "OneLLMRouter upstream request failed ..."
  }
}
```

Chat Completions 和 Responses 最终错误必须使用：

```json
{
  "error": {
    "message": "OneLLMRouter upstream request failed ...",
    "type": "upstream_error",
    "param": null,
    "code": "upstream_retry_exhausted"
  }
}
```

`code` 与最终失败在当前配置下的重试资格一致：具备资格但达到次数或时间上限时为 `upstream_retry_exhausted`；未启用重试或 HTTP 状态不在 `status_codes` 中时为 `upstream_retry_skipped`。`max_attempts: 1` 不改变失败的重试资格。

最终错误统一使用 `application/json`，不透传任意上游错误 header 或 Content-Type。成功响应仍保持现有字节、语义、usage、tool call、finish reason、SSE 顺序以及 Responses 流当前透传的成功 header。

### 10.3 原始错误正文处理

- 默认最多保留最后一次上游错误正文的 4 KiB。
- 超过上限时截断并明确标记。
- 无效 UTF-8 使用替换字符解码，移除不可显示的控制字符但保留正常空白。
- 大小写不敏感地屏蔽 Bearer credential，以及 JSON/header 文本中的 `api_key`、`api-key`、`x-api-key` 和 `authorization` 值。
- 完全相同的错误无需在最终响应中重复列出。
- 日志同样不得记录明文凭据。

这里的“原文”是经过长度限制和敏感信息清理后的最后一次上游响应，不承诺逐字节无损返回。

清理逻辑必须集中实现，并覆盖 JSON、纯文本、header 文本、大小写混合 secret、无效 UTF-8、超长正文和截断边界测试。即使上游反射了当前 provider 的配置 API key，也不得进入客户端错误或日志。

## 11. 统一实现边界

模型请求的重试判断、退避计算、`Retry-After` 解析、context 取消、尝试计数和最后错误保存必须集中在一个内部组件中。各协议 handler 只负责：

1. 提供可重复构建上游请求的函数。
2. 调用统一重试组件。
3. 在成功后执行现有响应处理。
4. 在最终失败后使用现有协议格式返回增强错误。

不得在每个 handler 中分别复制重试循环。

统一组件应保持小而明确，不负责协议翻译、SSE 解析、供应商路由或前端通知。

### 11.1 请求工厂

handler 必须先完成模型解析、模型 ID 改写和协议转换，再把不可变的上游请求字节交给 request factory。factory 每次调用都使用新的 body reader 构建新的 `http.Request`，并重新设置认证、Content-Type、Accept 和 provider 专用 header。不得复用已消费的 request 或在多次尝试之间共享可变的翻译对象。

### 11.2 两种执行模式

统一组件使用同一个重试循环，但提供两种明确模式：

1. **Headers 模式**用于流式请求。非 `2xx` 时读取受限错误正文并关闭 body；状态存在于 `retry.status_codes` 时重试，否则立即返回。`2xx` 时立即返回保持打开的 response body，不缓存 SSE 数据，由 handler 延续现有透传或翻译。
2. **Buffered 模式**用于非流式请求。只有在该路径声明的成功 body 策略内完整读完 `2xx` body 才算成功；读取超时或 I/O 错误属于可重试失败。组件返回响应元数据和完整 body，handler 再执行现有解析、翻译或透传。

Buffered 调用必须显式传入 `SuccessBodyLimit`：`0` 表示不限长，正数表示最多接受的成功正文长度。各路径固定为：

- Anthropic Messages 直连：`1 MiB`，保持当前内存上限；
- Chat Completions 直连：`0`，保持当前不限长；
- Chat Completions 到 Anthropic 翻译：`0`，保持当前不限长；
- OpenAI Responses 直连：`0`，保持当前不限长。

Anthropic 路径读取 `limit + 1` 字节以识别超限。超过 1 MiB 时关闭 body，返回不可重试的上游协议错误和 `502 Bad Gateway`，不得把截断 JSON 作为 `200` 成功返回。该行为修正现有的静默截断缺陷，但不提高内存上限。完整但格式错误的 `2xx` JSON 属于成功响应后的上游协议错误，不进入统一重试。

### 11.3 资源所有权

每个失败 HTTP body 最多读取 4097 字节：前 4096 字节用于安全摘要，额外一字节用于判断是否截断，然后必须在等待前关闭。超过限制的 body 不假定连接可复用。如果 `http.Client.Do` 同时返回非空 response 和 error，也必须读取受限正文并关闭 response body。

Headers 模式的成功 body 由 handler 关闭；Buffered 模式的成功 body 由统一组件读完并关闭。最终响应只保留最后一次失败，早期失败仅进入结构化日志。

### 11.4 Handler 迁移清单

以下推理路径必须全部迁移到统一组件，且不能遗漏流式或非流式分支：

- Anthropic Messages 直连；
- Chat Completions 直连；
- Chat Completions 到 Anthropic 的翻译路径；
- OpenAI Responses 直连。

## 12. 日志与可观测性

每一次失败和下一次等待都应记录结构化日志，至少包含：

- `request_id`
- `provider`
- `model`
- `endpoint`
- `attempt`
- `max_attempts`
- `status`（存在时）
- `failure_kind`
- `error`（必须是清理后的摘要）
- `delay_ms`
- `elapsed_ms`

成功前发生过重试时，记录一条恢复日志，包含尝试次数和总耗时。最终失败具备重试资格但达到次数或时间上限时记录 `upstream retry exhausted`；不具备重试资格时记录 `upstream retry skipped`。客户端取消和服务关机分别记录取消原因，不伪装成新的上游错误。同一个客户端请求的全部尝试沿用同一个 `request_id`。

重试日志不得改变客户端协议，也不新增托盘弹窗或系统通知要求。

## 13. 兼容性与取舍

- HTTP 重试行为完全由全局 `status_codes` 决定。默认排除 `403` 等通常不可恢复的状态；配置者可根据第三方供应商的实际行为调整并承担相应取舍。
- 上游可能对失败尝试计费，OneLLMRouter 无法保证供应商侧幂等或零费用。
- 重试可能增加上游负载，因此必须具有最大次数、最大总时长和随机抖动。
- 客户端自身可能仍有小于五分钟的超时；Router 无法阻止客户端提前取消。一旦客户端取消，Router 必须立即结束重试。
- `Retry-After` 超过 30 秒时会被截断到 30 秒，这是为了满足单次等待上限。
- 当前配置采用全局策略，避免新增供应商级覆盖造成额外复杂度。
- Anthropic 非流式成功正文超过 1 MiB 时，从现有的静默截断 `200` 改为明确的不可重试 `502`；这是为保证 Buffered“完整成功”语义所需的兼容性修正。

## 14. 验收标准

### 14.1 配置

- 缺少 `retry` 配置时使用文档中的默认值。
- 配置可关闭统一重试。
- 非法时长、负数、零上限和非法 jitter 在启动校验阶段返回清晰错误。
- 非法、成功态或重复的 `status_codes` 在启动校验阶段返回清晰错误；显式空列表合法。
- 示例配置和 README 描述新增字段及默认行为。

### 14.2 HTTP 状态重试

- 上游第一次返回 `429`、第二次返回成功时，客户端只收到成功响应。
- 上游依次返回配置中列出的 `400`、`401`、`502` 后成功时，所有失败均使用同一策略重试。
- 上游 `3xx` 不被自动跟随；配置该状态时重试，未配置时立即保留并返回。
- 上游持续返回错误时，在次数或五分钟时间上限到达后停止。
- 最终错误包含尝试次数、耗时、最后状态和清理后的最后响应正文。

### 14.3 网络错误重试

- 连接失败后恢复时，客户端收到正常成功响应。
- context 取消后，等待中的 timer 和上游请求立即退出。
- 网络错误最终失败时返回 `502`，响应包含最后一次错误摘要。
- 上游响应超时最终失败时返回 `504`。

### 14.4 退避

- 退避按指数增长并封顶于 30 秒。
- 抖动范围符合配置。
- 合法 `Retry-After` 被采用但不超过 30 秒。
- 等待不会突破剩余五分钟预算。
- 测试通过注入时钟或等待函数完成，不使用真实五分钟等待。

### 14.5 请求与响应完整性

- 每次尝试都收到完整且相同的模型请求体。
- 认证头和供应商专用请求头在每次尝试中保持正确。
- 每次失败的响应体均被关闭。
- 成功响应与未发生重试时的现有响应保持一致。
- 重试状态不会出现在 assistant 内容或后续模型上下文中。

### 14.6 流式边界

- 流式上游返回配置列出的非 `2xx` 后可以重试并在成功后正常透传。
- 流式上游的 `2xx` 响应头被接受后断开时不创建第二个上游请求。
- 不产生重复的 SSE 开始、结束或工具调用事件。

### 14.7 配置与退避边界

- 缺少 retry block 和缺少单个字段都能得到完整默认值。
- `enabled: false` 只执行一次请求且不等待。
- duration 字符串、裸数字、零值、负值和字段关系均有测试。
- jitter 使用可控随机源验证上下界，测试不依赖真实随机性。
- `Retry-After` 的整数秒和 HTTP-date 均覆盖；过去、负数、溢出和错误格式回退到指数退避。
- 最后一次尝试后不等待，剩余预算不足以等待时不做空等待。

### 14.8 Context 与资源生命周期

- 请求中和退避中的客户端取消都能立即退出。
- 服务关闭会先取消活动重试，再等待 HTTP server 退出。
- context cause 测试分别得到 client_cancel 和 service_shutdown，竞争时保留最先发生的 cause。
- 取消后不向客户端写新错误，也不启动下一次尝试。
- `Do` 同时返回 response 和 error 时仍关闭非空 body。
- Headers 成功 body、Buffered 成功 body 和每个失败 body 的所有权都有测试。
- Anthropic 成功 body 在 1 MiB 边界内通过，超过边界返回不可重试 `502`；其他非流式路径保持不限长。
- race 测试验证没有遗留 timer、goroutine 或共享随机源竞争。

### 14.9 四条协议路径

Anthropic Messages、Chat Completions 直连、Chat 到 Anthropic 翻译、Responses 都必须分别覆盖：

- 非流式先失败后成功，成功响应与不重试时一致；
- 流式非 `2xx` 后成功，不产生重复 SSE 事件；
- 接受流式 `2xx` 响应头后断流，不产生第二次上游请求；
- 持续失败时返回准确的下游 JSON schema 和 HTTP status；
- 模型 ID、请求 body、认证 header、provider header 和 request ID 在每次尝试中正确。

### 14.10 集成验证

- torture test 使用短的测试策略覆盖自动重试，不使用真实五分钟等待。
- 运行 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 和 `go mod verify`。
- 继续运行现有 Qt、构建、安装器、smoke 和 release contract tests。
- `git diff --check` 通过，仓库搜索确认没有重新引入 Copilot 路径。

## 15. 完成定义

本需求完成需同时满足：

1. 配置默认值、字符串 duration 解码、校验、README 和示例配置全部完成。
2. 统一组件集中实现请求重建、重试判断、两种执行模式、退避、`Retry-After`、取消、资源关闭、错误保存和安全清理。
3. 推理请求禁用自动重定向，但目录发现等非推理请求保持原行为。
4. HTTP server 具有可在 Shutdown 前取消的服务生命周期 context。
5. 四条模型推理路径的流式和非流式分支全部使用同一重试组件。
6. 默认策略为最多 15 次调用、首次等待 1 秒、单次等待最多 30 秒、失败恢复最多 5 分钟和 20% jitter。
7. 配置列出的 HTTP 状态、规定的传输错误和 Buffered body 读取错误进入重试；未列出的 HTTP 状态立即返回。
8. 客户端取消和服务关机及时终止请求与等待，且不写入伪造错误。
9. 最终失败响应包含协议兼容的安全摘要，且 `2xx` body 失败不会返回成功状态。
10. 流式 `2xx` 响应头被接受后不会自动重试，也不会受剩余 `max_elapsed` 终止。
11. 结构化日志覆盖每次失败、恢复、取消和最终失败，并且不泄露 credential。
12. 全部单元、race、协议集成、torture、桌面和 release 质量门通过。

## 16. 实施工作分解

本节是本需求的完整实施清单。后续实现按以下顺序推进；每项先写失败测试，再做满足测试的最小修改。不得先迁移 handler 再补统一组件，也不得在迁移过程中保留新旧两套重试循环。

### 16.1 配置模型与文档

涉及文件：

- internal/config/config.go
- internal/config/config_test.go
- onellm-router.example.yaml
- README.md

工作：

- [ ] 新增 RetryConfig 和严格的字符串 Duration YAML 类型，并挂到 Config.Retry。
- [ ] 在 DefaultConfig 中设置第 6 节的八个默认值。
- [ ] 验证 max_attempts、status_codes、initial_delay、max_delay、max_elapsed 和 jitter 的边界及字段关系。
- [ ] 证明缺少 retry block、缺少单个字段、enabled: false、裸数字 duration、零值和负值的加载行为。
- [ ] 在示例配置和 README 中记录默认启用、全局作用域、最长五分钟和状态码严格按配置匹配的行为。

预期接口形状：

    type RetryConfig struct {
        Enabled         bool     `yaml:"enabled"`
        MaxAttempts     int      `yaml:"max_attempts"`
        StatusCodes     []int    `yaml:"status_codes"`
        InitialDelay    Duration `yaml:"initial_delay"`
        MaxDelay        Duration `yaml:"max_delay"`
        MaxElapsed      Duration `yaml:"max_elapsed"`
        Jitter          float64  `yaml:"jitter"`
        HonorRetryAfter bool     `yaml:"honor_retry_after"`
    }

验证：

    go test -count=1 ./internal/config

### 16.2 统一重试内核

涉及文件：

- 新建 internal/upstream/retry.go
- 新建 internal/upstream/retry_test.go
- internal/catalog/catalog_test.go

工作：

- [ ] 定义 Headers 和 Buffered 两种模式、RequestFactory、Result、Failure、FailureKind 和只含非敏感字段的 Metadata。
- [ ] 实现统一尝试循环、最大次数、最大耗时、指数退避、jitter、Retry-After 和可取消等待。
- [ ] 证明 enabled: false 只调用一次且不等待；honor_retry_after: false 时忽略响应头并使用指数退避。
- [ ] 每次尝试调用 RequestFactory 生成新 request 和 body；factory 失败视为本地错误并立即结束。
- [ ] 为推理调用浅拷贝 http.Client 并设置 CheckRedirect 返回 http.ErrUseLastResponse，保持共享 Transport，不影响 catalog 客户端。
- [ ] 在 catalog 测试中证明模型发现仍可跟随重定向，防止推理策略污染目录请求。
- [ ] 通过可注入 now、wait 和 jitter 函数覆盖所有时间边界，测试不得真实等待五分钟。
- [ ] 明确最后一次尝试后不等待，剩余预算不足以发起下一次请求时直接返回最后错误。

核心调用边界：

    result, failure := executor.Do(
        requestContext,
        client,
        metadata,
        upstream.Options{
            Mode:              mode,
            PerAttemptTimeout: perAttemptTimeout,
            SuccessBodyLimit:  successBodyLimit,
            Sanitizer:         sanitizer,
        },
        requestFactory,
    )

验证：

    go test -count=1 ./internal/upstream -run "Retry|Backoff|RetryAfter|Cancel|Redirect"
    go test -race -count=1 ./internal/upstream

### 16.3 超时与响应体所有权

涉及文件：

- internal/upstream/retry.go
- internal/upstream/retry_test.go

工作：

- [ ] 每次尝试使用客户端 context 派生的可取消 context 和独立 timer，实际 timeout 取端点上限与剩余预算较小值。
- [ ] Headers 模式在收到 2xx 响应头后停止失败恢复 timer，但保留客户端和服务取消；返回的 body Close 同时释放尝试 context。
- [ ] Buffered 模式完整读取并关闭 2xx body 后才返回成功；读取超时分类为 504，其他读取错误分类为 502。
- [ ] Buffered 接受 SuccessBodyLimit；测试 Anthropic 的 1 MiB 边界与不可重试超限 502，以及其他路径的 0 不限长语义。
- [ ] 非 2xx 以及 Do 同时返回 response 和 error 时，最多读取 4097 字节并关闭 body。
- [ ] 测试所有成功、失败、取消和异常返回组合的 body 关闭次数，确保无 timer 和 goroutine 泄漏。

验证：

    go test -count=1 ./internal/upstream -run "Headers|Buffered|Body|Timeout"
    go test -race -count=1 ./internal/upstream

### 16.4 安全错误摘要与协议错误

涉及文件：

- 新建 internal/upstream/sanitize.go
- 新建 internal/upstream/sanitize_test.go
- 新建 internal/proxy/upstream_error.go
- 新建 internal/proxy/upstream_error_test.go

工作：

- [ ] 实现 4096 字节截断、无效 UTF-8 替换、不可显示控制字符清理和明确截断标记。
- [ ] 大小写不敏感地屏蔽 Bearer、authorization、api_key、api-key 和 x-api-key 的值。
- [ ] 由 handler 为每次已解析请求创建包含当前 provider API key 的 Sanitizer，并作为独立参数传给 Executor；Metadata 和日志字段不得保存该 secret。
- [ ] Failure 保留最后 HTTP 状态、失败种类、安全摘要、尝试次数和耗时，不保留完整 credential 或完整超长 body。
- [ ] Anthropic 和 OpenAI 两种最终错误 writer 生成第 10.2 节的精确 JSON schema，并统一设置 application/json。
- [ ] 覆盖 JSON、纯文本、header 风格、混合大小写、无效 UTF-8、超长内容和 provider API key 被反射的测试。

验证：

    go test -count=1 ./internal/upstream -run "Sanitize|Failure"
    go test -count=1 ./internal/proxy -run "UpstreamError"

### 16.5 服务生命周期取消

涉及文件：

- cmd/onellm-router/main.go
- cmd/onellm-router/server_lifecycle_test.go

工作：

- [ ] 使用 context.WithCancelCause 创建服务 base context，并通过 http.Server.BaseContext 传给所有请求。
- [ ] stop 信号到达后以 upstream.ErrServiceShutdown 取消服务 context，再调用 Shutdown。
- [ ] 客户端取消、tray shutdown、系统信号和 Serve 异常都只能触发一次关闭序列。
- [ ] 测试活动 handler 在服务取消后立即退出、Shutdown 不必等待完整重试预算，并证明 context.Cause 能区分服务关闭和客户端取消。

验证：

    go test -count=1 ./cmd/onellm-router -run "Server|Shutdown|Lifecycle"

### 16.6 Handler 接线与 Anthropic Messages 迁移

涉及文件：

- cmd/onellm-router/main.go
- internal/proxy/handler.go
- 新建 internal/proxy/retry_helpers.go
- internal/proxy/proxy_test.go

工作：

- [ ] 在启动时由 cfg.Retry 构建唯一 Executor，并注入 Handler；测试 helper 显式使用短策略。
- [ ] 把已完成模型改写的不可变 JSON 字节交给 Anthropic request factory，每次尝试重建 x-api-key 和 Content-Type。
- [ ] 非流式使用 Buffered 模式，流式使用 Headers 模式；删除原有直接 client.Do 和状态判断分支。
- [ ] 证明 429 后成功、400/401/502 后成功、持续失败、transport 恢复、Retry-After、请求体一致和每次 body 关闭。
- [ ] 证明流式非 2xx 后成功只产生一组 SSE，接受 2xx 响应头后断流不再请求上游。

验证：

    go test -count=1 ./internal/proxy -run "Anthropic|External|Retry"

### 16.7 Chat Completions 两条路径迁移

涉及文件：

- internal/proxy/handler.go
- internal/proxy/retry_helpers.go
- internal/proxy/proxy_openai_test.go

工作：

- [ ] OpenAI 直连 request factory 每次设置 Authorization、Content-Type 和流式 Accept，并保持去除 [1m] 后的模型名。
- [ ] Chat 到 Anthropic 翻译只在第一次尝试前执行一次，重试复用不可变翻译结果；每次尝试设置 x-api-key。
- [ ] 两条路径的非流式使用 Buffered，流式使用 Headers，并删除各自重复的 client.Do 错误分支。
- [ ] 完整 2xx JSON 解析或协议翻译失败不进入重试。
- [ ] 分别覆盖两条路径的先失败后成功、持续失败、请求完整性、SSE 边界和协议错误 JSON。

验证：

    go test -count=1 ./internal/proxy -run "OpenAI|Translate|Retry"

### 16.8 Responses 迁移与成功 header

涉及文件：

- internal/proxy/handler.go
- internal/proxy/retry_helpers.go
- internal/proxy/proxy_responses_test.go

工作：

- [ ] 保持现有 provider/model 前缀移除逻辑在重试前只执行一次。
- [ ] request factory 每次设置 Authorization、Content-Type 和流式 Accept。
- [ ] 非流式迁移到 Buffered；流式迁移到 Headers，并仅在最终 2xx 后复制现有成功 header。
- [ ] 证明 3xx 不自动跳转、配置列出的非 2xx 可重试、未列出的状态立即返回、非流式 body 读取错误可重试。
- [ ] 保留当前 Responses 字节透传和流式 io.Copy 行为；接受 2xx 后的断流只记录错误，不重试。

验证：

    go test -count=1 ./internal/proxy -run "Responses|Retry|Redirect"

### 16.9 日志与请求汇总

涉及文件：

- internal/upstream/retry.go
- internal/log/log.go
- cmd/onellm-router/main.go
- internal/upstream/retry_test.go
- 新建 cmd/onellm-router/request_logging_test.go

工作：

- [ ] 每次失败和等待写出第 12 节要求的结构化字段，错误字段只能使用安全摘要。
- [ ] 重试后成功记录 recovered；具备重试资格的最终失败记录 exhausted，不具备资格的失败记录 skipped；客户端取消和服务关闭使用不同 failure_kind。
- [ ] 扩展 RequestMeta 和请求结束日志，记录总尝试次数、重试耗时、最后状态及最后失败种类。
- [ ] 同一请求的所有日志沿用 middleware 生成的 request_id。
- [ ] 测试日志输出不包含配置 API key、Bearer token 或被清理前的上游正文。

验证：

    go test -count=1 ./internal/upstream ./internal/log ./cmd/onellm-router -run "Log|RequestID|Retry"

### 16.10 集成、回归与发布门

涉及文件：

- tools/torturetest.ps1
- README.md
- onellm-router.example.yaml

工作：

- [ ] torture test 增加短重试策略和可恢复 mock 上游，覆盖至少一个非流式协议及一个流式协议。
- [ ] 搜索四条推理路径，确认没有绕过 Executor 的直接 client.Do；catalog discovery 保持原实现。
- [ ] 搜索 Copilot 标识，确认本需求没有重新引入已删除功能。
- [ ] 运行完整 Go、race、vet、module、Qt、构建、安装器、smoke 和 release 契约。
- [ ] 仅在 GitHub 托管 Windows runner 执行真实安装器集成；本机不得运行安装器或占用 3456/3457。

最终验证：

    go test -count=1 ./...
    go test -race -count=1 ./...
    go vet ./...
    go mod verify
    pwsh -NoProfile -File .\tools\torturetest.ps1
    ctest --test-dir desktop\build -C Release --output-on-failure
    pwsh -NoProfile -File .\tools\build-safety-test.ps1
    pwsh -NoProfile -File .\tools\desktop-smoke-safety-test.ps1
    pwsh -NoProfile -File .\tools\installer-autostart-contract-test.ps1
    pwsh -NoProfile -File .\tools\installer-upgrade-contract-test.ps1
    pwsh -NoProfile -File .\tools\release-workflow-contract-test.ps1
    git diff --check

每完成 16.1 至 16.9 的一个阶段，都应单独提交其代码与测试；16.10 只修改 torture test、README 和示例配置并执行现有质量门，不再引入新的产品行为，也不得放宽端口和进程所有权保护。
