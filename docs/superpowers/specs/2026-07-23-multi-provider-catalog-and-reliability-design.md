# Multi-Provider Catalog and Reliability Design

## Context

OneLLMRouter now supports Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses providers. The first catalog implementation added all discovery, protocol filtering, Codex metadata generation, and HTTP rendering directly to the proxy handler. That design cannot reliably distinguish models discovered through different protocol endpoints, and it treats provider-wide capabilities as if they were model-level metadata.

The same development batch also introduced correctness gaps in streamed tool translation, Responses streaming, installation, tray health state, model resolution, local secret handling, and release versioning. These are independent problems and will be implemented and verified independently.

## Goals

- Make each of the three model-list endpoints return only models for its protocol.
- Make configured model lists authoritative without validating them against upstream catalogs.
- Make Codex `/model` receive a valid Codex model catalog whose slugs use `provider/model`.
- Preserve valid Anthropic event ordering for streamed tool calls, including delayed names and interleaved deltas.
- Preserve long-running and large Responses SSE streams without false success reporting.
- Make installation failure visible and avoid leaving an invalid startup registration.
- Wire healthy, degraded, and down tray states to actual health data.
- Prevent loose resolver matching from bypassing explicit model lists.
- Keep local credentials out of version control and produce only version `1.3.2` artifacts.

## Non-Goals

- No persistent catalog database, background refresh worker, or new configuration format.
- No model capability validation against upstream services.
- No attempt to infer the protocol of individual configured models from their names.
- No protocol conversion for the Responses API; it remains direct passthrough.
- No unrelated cleanup of existing code or configuration.

## 1. Catalog Architecture

### Package boundary

Create `internal/catalog` as the only component responsible for assembling model catalogs. The package consumes the current `router.Provider` values and an HTTP client. The proxy handler selects a protocol, calls the catalog package, and renders the result; it no longer contains upstream discovery logic.

The catalog package has three responsibilities:

1. Select providers that support the requested endpoint.
2. Obtain model IDs from configuration or the matching upstream endpoint.
3. Return normalized records containing the provider prefix and model ID.

Rendering is split by response contract:

- Anthropic and Chat Completions endpoints return `{"object":"list","data":[...]}`.
- Responses/Codex returns `{"models":[ModelInfo...]}` with the fields expected by the official Codex model client.

### Protocol classification

Provider support is determined per endpoint:

- Anthropic uses `base_url`.
- Chat Completions uses `openai_base_url`.
- Responses/Codex uses `responses_base_url`.
- The built-in `cp` provider is classified as Anthropic even though it has no upstream URL.

Each automatically discovered model inherits the protocol of the source request that returned it. A provider supporting multiple endpoint types is queried independently for each requested catalog. Provider-wide `EndpointTypes()` is not used to assign a model to every supported protocol.

### Configuration precedence

`providers[].models` is an authoritative static override:

- If the list is non-empty, the catalog returns exactly those configured model IDs for every endpoint the provider supports.
- It does not request the upstream model-list endpoint.
- It does not intersect with, validate against, or fall back to the upstream catalog.
- Incorrect configured model IDs are still returned, by explicit product requirement.

If `providers[].models` is empty, the catalog discovers models from only the upstream URL corresponding to the requested protocol.

This means a multi-protocol provider with one configured model list exposes that same configured list on each protocol it supports. Per-protocol static lists would require a new configuration format and are outside this change.

### Upstream requests

Automatic discovery uses the existing upstream URL contracts:

- Anthropic: `base_url + /models`, authenticated with `x-api-key`.
- Chat Completions: `openai_base_url + /models`, authenticated with Bearer auth.
- Responses: `responses_base_url + /v1/models`, authenticated with Bearer auth.

The source layer owns request construction, response validation, bounded decoding, body closure, and cancellation. The response body is decoded before the per-request context is cancelled. Non-2xx responses and malformed payloads are recorded as source failures.

Successful providers are still returned when another automatic source fails. If every eligible source fails and no static records exist, the endpoint returns `502` rather than a misleading empty successful catalog.

Results preserve provider configuration order and upstream/configured model order. Duplicate full slugs are removed without reordering.

### Codex contract

Codex model slugs and display names use `prefix/model`. The renderer emits the full `ModelInfo` shape already introduced in `internal/proxy/handler.go`, with stable defaults for reasoning levels, shell type, visibility, API support, priority, verbosity, patch tool type, truncation policy, parallel tool calls, and experimental tools.

The design follows the official Codex behavior: Codex requests `{model_provider.base_url}/models?client_version=...` and expects a top-level `models` array. No provider selector is added to `/model`; selecting a catalog entry selects the namespaced model itself.

## 2. Streamed Tool Calls

OpenAI may split a tool call's ID, name, and arguments across chunks, and may interleave chunks from multiple tool indices. Anthropic content blocks cannot be resumed after `content_block_stop`, so immediately forwarding interleaved argument fragments can create invalid block references.

`StreamContext` will accumulate tool-call state by OpenAI tool index. Tool output is emitted in index order when the upstream reports `finish_reason=tool_calls`:

1. Emit one `content_block_start` after the complete ID/name state is available.
2. Emit the accumulated arguments as an `input_json_delta`.
3. Emit the matching `content_block_stop`.

This intentionally trades incremental tool-argument delivery for valid deterministic Anthropic event ordering. Text streaming remains incremental. Missing names or malformed final state produce an explicit translation error rather than a delta referencing a block that never started.

## 3. Responses Streaming

Responses requests remain byte-for-byte direct passthrough except for the already required namespaced model rewrite.

- Non-streaming requests retain a finite request timeout.
- Streaming requests use the inbound request context and do not impose the current fixed two-minute deadline.
- SSE copying no longer uses `bufio.Scanner`, avoiding its token-size limit.
- The copy path flushes data as it arrives, propagates upstream status and headers, checks copy errors, and records success only after clean EOF.
- Client cancellation is distinguished from an upstream read failure in logs and request metadata.

## 4. Installation and Tray State

Installation follows a validate-then-mutate sequence:

1. Resolve the executable and absolute configuration paths.
2. Load and validate the configuration.
3. Prepare the daemon command line.
4. Write the startup registry value.
5. Start the daemon and wait for health.

Failures from configuration loading, process startup, or health verification are returned to the caller. If this invocation changed the registry and daemon startup or health verification fails, it restores the previous registry value or removes the newly created value.

Tray polling treats transport errors, non-2xx responses, and malformed health JSON as down. A valid health response with upstream errors is degraded; otherwise it is healthy. Each poll updates both the stored health snapshot and the global tray status before posting the UI refresh message.

## 5. Resolver Rules

Namespaced model identifiers use `prefix/model` as the canonical format. The existing legacy `prefix-model` form may remain for compatibility, but it follows the same validation rules.

- Providers with explicit `models` accept only configured models, including the existing `[1m]` alias normalization.
- Providers without explicit models may accept a namespaced model dynamically.
- A parsed provider prefix never bypasses an explicit model list.
- Bare model names continue to resolve only when they match an explicit configured model; ambiguous automatic-provider fallback is removed.
- Prefix-only input continues to select the first configured model when one exists.

## 6. Secrets, Versioning, and Tooling

- Add versioned local runtime configuration files such as `onellm-router-v*.yaml` to `.gitignore`; keep the example configuration limited to placeholders.
- Do not delete or rewrite the user's local `onellm-router-v1.3.2.yaml`; it remains local and ignored.
- Set build and documentation defaults to `1.3.2`.
- Remove only the obsolete `dist/onellm-router-v1.3.3.exe` artifact previously identified by the user.
- Build the final Windows executable as `dist/onellm-router-v1.3.2.exe`.
- Wire the seven `ONELLM_*_TIMEOUT_MS` variables used by `tools/torturetest.ps1` into their corresponding request, first-event, idle-stream, and Copilot timeout paths while preserving the current durations as defaults. Invalid or non-positive overrides keep the defaults.
- Run `gofmt` only on Go files touched by this work.

## 7. Testing Strategy

Every behavior change follows a red-green cycle. Regression tests are added before production changes and are run once to confirm the expected failure.

Catalog tests cover:

- Static configuration prevents upstream requests.
- Static configuration is returned even when model IDs are invalid.
- Each protocol selects only its matching provider endpoint.
- A multi-protocol provider is queried through the requested protocol URL.
- The built-in `cp` provider appears only in the Anthropic catalog.
- Partial source failure preserves successful providers; total source failure returns `502`.
- Codex output has the official top-level wrapper, required metadata, and slash-separated slugs.

Streaming tests cover delayed tool IDs/names, arguments arriving before names, multiple argument chunks, interleaved tool indices, missing final names, streams longer than two minutes through a controllable context, large SSE events, upstream copy errors, and clean completion metadata.

Installation, tray, and resolver tests cover validation before registry mutation, rollback after daemon failure, non-2xx/malformed health responses, degraded status wiring, explicit-list rejection through both separator forms, dynamic namespaced models, and removal of ambiguous bare-model fallback.

Timeout tests cover valid millisecond overrides, invalid/non-positive fallback behavior, and each runtime path consumed by the torture-test silent cases.

Final verification consists of targeted package tests, `go test -count=1 ./...`, `go vet ./...`, `go test -race -count=1 ./...`, `git diff --check`, a `1.3.2` build, and a local model-catalog request on a non-production port. The running service on port `3457` is not stopped or replaced during tests.

## 8. Implementation Order

1. Catalog package and HTTP handler integration.
2. Resolver strictness required by catalog model slugs.
3. Streamed tool-call state machine.
4. Responses streaming transport.
5. Install transaction and tray health wiring.
6. Secret ignore rules, version normalization, tooling cleanup, formatting, and final build.

Each step must leave its targeted tests green before the next step begins.
