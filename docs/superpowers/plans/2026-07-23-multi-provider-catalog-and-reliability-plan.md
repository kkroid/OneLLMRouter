# Multi-Provider Catalog and Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a protocol-correct three-way model catalog and close the reviewed streaming, resolver, installation, tray, timeout, secret, and version defects in release `1.3.2`.

**Architecture:** Move catalog assembly into a focused `internal/catalog` package whose source selection is endpoint-specific and whose configured lists are authoritative. Keep the other fixes isolated in their existing packages, using small pure helpers where operating-system or timing behavior needs deterministic tests.

**Tech Stack:** Go 1.24, `net/http`, `httptest`, Cobra, Windows registry APIs, PowerShell build tooling.

---

## File Map

- Create `internal/catalog/catalog.go`: protocol-specific provider selection, configured override, upstream discovery, deduplication, and source errors.
- Create `internal/catalog/catalog_test.go`: catalog source and precedence regression tests.
- Create `internal/proxy/timeouts.go`: runtime timeout defaults and `ONELLM_*_TIMEOUT_MS` parsing.
- Create `internal/proxy/timeouts_test.go`: timeout override tests.
- Modify `internal/proxy/handler.go`: delegate model lists, render Codex/OpenAI wrappers, make Responses streaming copy safe, and consume timeout settings.
- Modify `internal/proxy/model_list_test.go`: handler response-contract and failure tests.
- Modify `internal/proxy/proxy_responses_test.go`: large/cancelled/error Responses stream tests.
- Modify `internal/router/provider.go`: endpoint support including built-in Copilot.
- Modify `internal/router/resolver.go`: strict explicit-list resolution.
- Modify `internal/router/resolver_test.go`: resolver bypass regressions.
- Modify `internal/translate/stream.go`: buffer tool calls and emit ordered Anthropic blocks at finish.
- Modify `internal/translate/tool_stream_test.go`: delayed/interleaved/malformed tool regressions.
- Modify `cmd/onellm-router/main.go`: transactional install orchestration.
- Modify `cmd/onellm-router/install_test.go`: validation ordering and rollback tests.
- Modify `internal/ui/tray.go`: testable health polling and reliable status updates.
- Modify `internal/ui/tray_test.go`: malformed/down/degraded/healthy health tests.
- Modify `.gitignore`, `build.ps1`, `README.md`: local secret ignore and version `1.3.2` normalization.
- Delete `dist/onellm-router-v1.3.3.exe`: obsolete artifact explicitly selected by the user.

### Task 1: Protocol-Correct Catalog Core

**Files:**
- Create: `internal/catalog/catalog.go`
- Create: `internal/catalog/catalog_test.go`
- Modify: `internal/router/provider.go`

- [ ] **Step 1: Write failing provider-support and catalog tests**

Add table-driven tests with providers containing unique model-list servers. The essential assertions are:

```go
func TestListConfiguredModelsOverrideUpstream(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()

	service := catalog.New(func(*router.Provider) *http.Client { return server.Client() })
	models, err := service.List(context.Background(), []router.Provider{{
		Prefix: "c78", ResponsesBaseURL: server.URL, Models: []string{"gpt-5.6-sol", "not-real"},
	}}, router.EndpointResponses)
	if err != nil { t.Fatal(err) }
	if called { t.Fatal("configured models must not query upstream") }
	assertIDs(t, models, "c78/gpt-5.6-sol", "c78/not-real")
}

func TestProviderSupportsEndpointIncludesBuiltInCopilot(t *testing.T) {
	p := router.Provider{Prefix: "cp", Models: []string{"claude-opus-4.8"}}
	if !p.SupportsEndpoint(router.EndpointAnthropic) { t.Fatal("cp must support Anthropic") }
	if p.SupportsEndpoint(router.EndpointResponses) { t.Fatal("cp must not support Responses") }
}
```

For `TestListUsesRequestedProtocolURL`, create three `httptest.Server` instances whose handlers increment separate atomic counters and return one uniquely named model. Configure all three URLs on one provider, call `List` once per endpoint type, and assert each call increments only its matching counter and returns only that server's model.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./internal/catalog ./internal/router`

Expected: build/test failure because `catalog.New`, `Service.List`, and `Provider.SupportsEndpoint` do not exist.

- [ ] **Step 3: Implement the minimum catalog API**

Use these production-facing types:

```go
package catalog

type Model struct {
	ID       string
	Created  int64
	OwnedBy  string
	Endpoint router.EndpointType
}

type SourceError struct {
	Provider string
	Err      error
}

type Result struct {
	Models []Model
	Errors []SourceError
}

type Service struct {
	clientFor func(*router.Provider) *http.Client
}

func New(clientFor func(*router.Provider) *http.Client) *Service
func (s *Service) List(ctx context.Context, providers []router.Provider, endpoint router.EndpointType) Result
```

`List` must skip unsupported providers, return configured models without network I/O, otherwise call only `sourceFor(provider, endpoint)`. Decode `{"data":[{"id":"...","created":1}]}` with a 5-second child context, defer cancellation until decoding and body closure finish, and preserve partial successes plus `SourceError` values.

Add:

```go
func (p *Provider) SupportsEndpoint(endpoint EndpointType) bool {
	switch endpoint {
	case EndpointAnthropic:
		return p.BaseURL != "" || p.Prefix == "cp"
	case EndpointOpenAI:
		return p.OpenAIBaseURL != ""
	case EndpointResponses:
		return p.ResponsesBaseURL != ""
	default:
		return false
	}
}
```

- [ ] **Step 4: Run catalog tests and verify GREEN**

Run: `go test -count=1 ./internal/catalog ./internal/router`

Expected: PASS.

### Task 2: Catalog HTTP Rendering

**Files:**
- Modify: `internal/proxy/handler.go`
- Modify: `internal/proxy/model_list_test.go`

- [ ] **Step 1: Write failing handler tests**

Add `TestServeModelListTotalDiscoveryFailureReturnsBadGateway`, which points the sole eligible provider at a server returning `500` and asserts status `502`. Add `TestServeModelListPartialFailureReturnsSuccessfulModels`, which combines that server with a configured successful provider and asserts status `200` plus only the configured slug. Add `TestServeModelListCopilotOnlyAppearsInAnthropic`, which calls both endpoint types and asserts the `cp` slug exists only in the Anthropic response. Add `TestServeModelListResponsesDoesNotQueryAnthropicURL`, which uses separate server counters and asserts the Responses request increments only the Responses counter.

Keep the existing required Codex field assertions and slash-separated slug assertion.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./internal/proxy -run 'TestServeModelList'`

Expected: incorrect source is called, Copilot is absent, or total failure returns `200`.

- [ ] **Step 3: Delegate assembly and retain thin renderers**

Add `Catalog *catalog.Service` to `Handler`; initialize it in `NewHandler` with `h.clientFor`. For directly constructed test handlers, lazily create the service in `ServeModelList`.

Replace `modelListURL` and inline discovery with:

```go
result := h.catalogService().List(r.Context(), h.Resolver.Providers(), endpointType)
if len(result.Models) == 0 && len(result.Errors) > 0 {
	h.writeError(w, http.StatusBadGateway, "model discovery failed")
	return
}
```

Map `catalog.Model` to `router.ModelEntry` for Anthropic/OpenAI output and to the existing `codexModelInfo` for Responses. A record's `supported_endpoint_types` contains only its source endpoint.

- [ ] **Step 4: Run handler tests and verify GREEN**

Run: `go test -count=1 ./internal/proxy -run 'TestServeModelList'`

Expected: PASS.

### Task 3: Strict Resolver

**Files:**
- Modify: `internal/router/resolver.go`
- Modify: `internal/router/resolver_test.go`

- [ ] **Step 1: Write failing bypass tests**

```go
func TestResolverRejectsUnknownConfiguredSlashModel(t *testing.T) {
	r := NewResolver([]Provider{{Prefix: "mars", Models: []string{"gpt-5.6-sol"}}})
	if got := r.Resolve("mars/not-configured"); got != nil { t.Fatalf("unexpected match: %+v", got) }
}

func TestResolverRejectsUnknownConfiguredLegacyModel(t *testing.T) {
	r := NewResolver([]Provider{{Prefix: "mars", Models: []string{"gpt-5.6-sol"}}})
	if got := r.Resolve("mars-not-configured"); got != nil { t.Fatalf("unexpected match: %+v", got) }
}

func TestResolverAllowsNamespacedDynamicModel(t *testing.T) {
	r := NewResolver([]Provider{{Prefix: "mars", ResponsesBaseURL: "http://unused"}})
	got := r.Resolve("mars/anything")
	if got == nil || got.Model != "anything" { t.Fatalf("unexpected result: %+v", got) }
}

func TestResolverRejectsBareDynamicFallback(t *testing.T) {
	r := NewResolver([]Provider{{Prefix: "mars", ResponsesBaseURL: "http://unused"}})
	if got := r.Resolve("anything"); got != nil { t.Fatalf("unexpected match: %+v", got) }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./internal/router -run Resolver`

Expected: explicit-list bypass and bare fallback tests fail.

- [ ] **Step 3: Implement one validated parsing path**

Parse canonical `/` first and legacy `-` second. After finding a provider:

```go
if len(provider.Models) == 0 {
	return &ResolveResult{Provider: provider, Model: model}
}
if configuredModel(provider.Models, model) {
	return &ResolveResult{Provider: provider, Model: model}
}
return nil
```

Remove the last-resort bare-model automatic-provider branch. Preserve prefix-only selection and `[1m]` aliases.

- [ ] **Step 4: Run resolver and proxy tests**

Run: `go test -count=1 ./internal/router ./internal/proxy`

Expected: PASS.

### Task 4: Ordered Tool Stream Translation

**Files:**
- Modify: `internal/translate/stream.go`
- Modify: `internal/translate/tool_stream_test.go`

- [ ] **Step 1: Write failing interleaving tests**

Add an event-sequence assertion for chunks ordered `tool0(name,args1)`, `tool1(name,args1)`, `tool0(args2)`, finish. Assert no tool block is emitted before finish and final events are start/delta/stop for index 0 followed by start/delta/stop for index 1. Add argument-before-name and missing-name-at-finish cases.

`TestCoreStreamInterleavedToolsEmitOrderedBlocksAtFinish` feeds the four chunks described above, asserts the first three calls return no tool block events, then compares the finish call's event types and indices against `start(0), delta(0), stop(0), start(1), delta(1), stop(1), message_delta, message_stop`. `TestCoreStreamBuffersArgumentsBeforeName` sends arguments first and the name second, then asserts the finish delta contains their concatenation. `TestCoreStreamRejectsMissingToolNameAtFinish` leaves a tool unnamed and asserts the finish call returns a non-nil error containing `missing tool name`.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./internal/translate -run 'Tool|Stream'`

Expected: current code emits early blocks, loses early arguments, or sends deltas to the wrong index.

- [ ] **Step 3: Buffer and flush tools deterministically**

Always append `tc.funcArgs()` to `ToolCallState.Args`, regardless of whether the name has arrived. Do not emit tool blocks from delta chunks. On `finish_reason=tool_calls`, close an open text block, sort tool indices, validate ID/name, and emit:

```go
start(index, ToolUseID, ToolName)
delta(index, accumulatedArgs)
stop(index)
```

Then emit `message_delta` and `message_stop`. Remove `ActiveToolIdx` and `BlockSent` if no remaining behavior uses them.

- [ ] **Step 4: Run translation tests and verify GREEN**

Run: `go test -count=1 ./internal/translate`

Expected: PASS.

### Task 5: Responses Stream Transport

**Files:**
- Modify: `internal/proxy/handler.go`
- Modify: `internal/proxy/proxy_responses_test.go`

- [ ] **Step 1: Write failing stream transport tests**

Add an SSE event larger than 256 KiB and assert the entire event survives. Add a reader that returns data followed by a sentinel error and assert request metadata does not end as `ok`. Add a context-controlled delayed stream proving no fixed handler deadline is applied.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./internal/proxy -run Responses`

Expected: large Scanner token is truncated and/or stream error is recorded as success.

- [ ] **Step 3: Split streaming and non-streaming contexts**

For non-streaming Responses, keep `context.WithTimeout(r.Context(), responseRequestTimeout())`. For streaming, create the upstream request with `r.Context()` directly. Copy upstream headers/status, then use:

```go
type flushWriter struct { w io.Writer; flusher http.Flusher }
func (w flushWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if n > 0 && w.flusher != nil { w.flusher.Flush() }
	return n, err
}
```

Call `io.Copy(flushWriter{w, flusher}, resp.Body)`, set `ok` only on nil error, and classify `r.Context().Err()` as `client_cancel`.

- [ ] **Step 4: Run Responses tests and verify GREEN**

Run: `go test -count=1 ./internal/proxy -run Responses`

Expected: PASS.

### Task 6: Runtime Timeout Overrides

**Files:**
- Create: `internal/proxy/timeouts.go`
- Create: `internal/proxy/timeouts_test.go`
- Modify: `internal/proxy/handler.go`

- [ ] **Step 1: Write failing parser tests**

```go
func TestDurationFromEnvUsesMilliseconds(t *testing.T) {
	t.Setenv("ONELLM_TEST_TIMEOUT_MS", "125")
	if got := durationFromEnv("ONELLM_TEST_TIMEOUT_MS", time.Second); got != 125*time.Millisecond { t.Fatal(got) }
}

func TestDurationFromEnvKeepsDefaultForInvalidValues(t *testing.T) {
	for _, value := range []string{"", "bad", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ONELLM_TEST_TIMEOUT_MS", value)
			if got := durationFromEnv("ONELLM_TEST_TIMEOUT_MS", time.Second); got != time.Second { t.Fatal(got) }
		})
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./internal/proxy -run DurationFromEnv`

Expected: helper does not exist.

- [ ] **Step 3: Implement named timeout accessors**

Create `durationFromEnv` using `os.LookupEnv`, `strconv.ParseInt`, and millisecond conversion. Define accessors for all seven variables in `tools/torturetest.ps1`, preserving current 60-second, 300-second, and applicable stream timeout defaults. Use them in external, OpenAI, Copilot, first-event, and idle-stream paths.

- [ ] **Step 4: Run timeout and torture silent cases**

Run: `go test -count=1 ./internal/proxy`

Then, after the development binary is built on a non-production port, run: `pwsh -NoProfile -File tools/torturetest.ps1 -Binary dist/onellm-router-dev.exe -IncludeSilentCases`

Expected: timeout unit tests and silent cases PASS without touching port `3457`.

### Task 7: Transactional Install

**Files:**
- Modify: `cmd/onellm-router/main.go`
- Modify: `cmd/onellm-router/install_test.go`

- [ ] **Step 1: Write failing install-sequence tests**

Extract an `installDeps` struct with functions for config loading, registry get/set/delete, process start, and health wait. Tests record call order and injected failures:

`TestRunInstallValidatesBeforeRegistryWrite` injects a configuration load error, records every dependency call, and asserts the call list contains only `load`. `TestRunInstallRollsBackNewRegistryValueAfterStartFailure` injects no previous value and a start error, then asserts `delete` follows `set`. `TestRunInstallRestoresPreviousValueAfterHealthFailure` injects `old-command`, a successful start, and a health error, then asserts the final `set` restores `old-command`.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./cmd/onellm-router -run Install`

Expected: orchestration helper and rollback behavior do not exist.

- [ ] **Step 3: Implement `runInstall` and thin Cobra wiring**

Move the sequence into `runInstall(ctx, exePath, cfgPath string, deps installDeps) error`. Load config before opening/writing the registry. Capture the previous registry value, write only if changed, and rollback on start/health failure. Return every failure. Keep `installCmd` responsible only for constructing real dependencies and user-facing success output.

- [ ] **Step 4: Run install tests and verify GREEN**

Run: `go test -count=1 ./cmd/onellm-router -run Install`

Expected: PASS.

### Task 8: Testable Tray Health State

**Files:**
- Modify: `internal/ui/tray.go`
- Modify: `internal/ui/tray_test.go`

- [ ] **Step 1: Write failing health classification tests**

Extract `pollHealthWithClient(client *http.Client)` and test `httptest.Server` responses for `500`, malformed JSON, healthy JSON, and healthy JSON plus recorded upstream errors. Assert both `health.statusText` and `GetTrayStatus()`.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -count=1 ./internal/ui -run 'Health|Status'`

Expected: malformed JSON is currently classified healthy and the injected client API does not exist.

- [ ] **Step 3: Implement explicit classification**

Close every non-nil response body. Treat transport, status, and decode errors as down. Only valid JSON may become healthy/degraded. Set the global status before posting `wmUpdateTray`.

- [ ] **Step 4: Run tray tests and verify GREEN**

Run: `go test -count=1 ./internal/ui`

Expected: PASS.

### Task 9: Secret and Version Cleanup

**Files:**
- Modify: `.gitignore`
- Modify: `build.ps1`
- Modify: `README.md`
- Delete: `dist/onellm-router-v1.3.3.exe`

- [ ] **Step 1: Add deterministic text checks**

Before editing, run checks that demonstrate current failures:

```powershell
rg -n '1\.3\.0|1\.3\.3' build.ps1 README.md
git check-ignore onellm-router-v1.3.2.yaml
Test-Path -LiteralPath dist/onellm-router-v1.3.3.exe
```

Expected: old version matches exist, local config is not ignored, and the obsolete executable exists.

- [ ] **Step 2: Apply minimal cleanup**

Add `onellm-router-v*.yaml` to `.gitignore`, update active build/run examples and the release description to `1.3.2`, and remove only `C:\Users\kkroid\OneLLMRouter\dist\onellm-router-v1.3.3.exe` after rechecking its resolved path is inside `dist`.

- [ ] **Step 3: Verify cleanup checks**

Run the same checks. Expected: no active `1.3.0`/`1.3.3` references, the local config is ignored, and the obsolete executable is absent.

### Task 10: Formatting, Full Verification, and Build

**Files:**
- Modify: only Go files touched above through `gofmt`
- Create/replace: `dist/onellm-router-v1.3.2.exe`

- [ ] **Step 1: Format touched Go files**

Run `gofmt -w` with the explicit list of Go files changed by Tasks 1-8.

- [ ] **Step 2: Run targeted and full verification**

Run:

```powershell
go test -count=1 ./internal/catalog ./internal/router ./internal/translate ./internal/proxy ./internal/ui ./cmd/onellm-router
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
git diff --check
```

Expected: every command exits `0` with no test failures or vet diagnostics.

- [ ] **Step 3: Build version 1.3.2**

Run: `pwsh -NoProfile -File build.ps1 -Version 1.3.2`

Expected: `dist/onellm-router-v1.3.2.exe` is created and the build script's tests pass.

- [ ] **Step 4: Verify version and live catalog on an isolated port**

Run the new binary's `version` command and start a temporary instance with a generated config on a port other than `3457`. Request `/anthropic/v1/models`, `/openai/v1/models`, and `/openai/models`; assert their wrappers and protocol-specific slugs. Stop only the temporary process created by this step.

Expected: binary reports `1.3.2`; Codex catalog uses `{"models":...}` and slash-separated slugs; the existing service on `3457` remains untouched.
