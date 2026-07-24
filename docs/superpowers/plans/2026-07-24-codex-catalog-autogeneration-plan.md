# Codex Catalog Auto-Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate a Codex-compatible Responses model catalog after startup and optionally mirror it into the Codex catalog path, enabled by default.

**Architecture:** Extend `internal/catalog` with one shared Codex renderer and an atomic file generator so HTTP and disk output cannot drift. Keep startup orchestration thin in `cmd/onellm-router`, and make configuration defaulting explicit in `internal/config`.

**Tech Stack:** Go 1.24, `encoding/json`, `os`, YAML v3, existing catalog discovery service.

---

### Task 1: Configuration Default

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `onellm-router.example.yaml`
- Modify: `README.md`

- [ ] Write tests proving omitted `codex.overwrite_catalog` is `true` and explicit `false` is preserved.
- [ ] Run `go test -count=1 ./internal/config` and verify RED.
- [ ] Add `CodexConfig` and initialize `OverwriteCatalog: true` in `DefaultConfig`.
- [ ] Document `codex.overwrite_catalog` in the example and README.
- [ ] Run `go test -count=1 ./internal/config` and verify GREEN.

### Task 2: Shared Codex Rendering

**Files:**
- Create: `internal/catalog/codex.go`
- Create: `internal/catalog/codex_test.go`
- Modify: `internal/proxy/handler.go`
- Modify: `internal/proxy/model_list_test.go`

- [ ] Write a failing renderer test for the existing Codex wrapper, ordering, slash slugs, and required fields.
- [ ] Run `go test -count=1 ./internal/catalog -run Codex` and verify RED.
- [ ] Move the Codex model shape into `internal/catalog` and expose `MarshalCodex([]Model)`.
- [ ] Make `ServeModelList` write the shared rendered bytes for Responses catalogs.
- [ ] Run catalog and model-list tests and verify GREEN.

### Task 3: Atomic Catalog Generation

**Files:**
- Create: `internal/catalog/generate.go`
- Create: `internal/catalog/generate_test.go`

- [ ] Write failing tests proving overwrite enabled writes both files, disabled writes only OneLLM, existing files are replaced, and empty discovery preserves both files.
- [ ] Run `go test -count=1 ./internal/catalog -run Generate` and verify RED.
- [ ] Add `GenerateCodex` using Responses-only discovery, shared rendering, temporary files, sync, close, and rename.
- [ ] Return source errors without writing partial models; reject incomplete or empty results before any write.
- [ ] Run `go test -count=1 ./internal/catalog -run Generate` and verify GREEN.

### Task 4: Startup Wiring

**Files:**
- Modify: `cmd/onellm-router/main.go`
- Create: `cmd/onellm-router/catalog_test.go`

- [ ] Write a failing orchestration test proving the default paths are `~/.onellm/model-catalog.json` and `~/.codex/model-catalog.json` and generation receives the configured overwrite flag.
- [ ] Run `go test -count=1 ./cmd/onellm-router -run Catalog` and verify RED.
- [ ] Start one asynchronous catalog generation after the HTTP listener goroutine starts; log partial source failures, write failures, model count, and paths.
- [ ] Run `go test -count=1 ./cmd/onellm-router -run Catalog` and verify GREEN.

### Task 5: Verification and Release Build

**Files:**
- Replace: `dist/onellm-router-v1.3.2.exe`

- [ ] Run `gofmt` on touched Go files.
- [ ] Run `go test -count=1 ./...`, `go vet ./...`, `go test -race -count=1 ./...`, and `git diff --check`.
- [ ] Build with `pwsh -NoProfile -File build.ps1 -Version 1.3.2`.
- [ ] Start only an isolated-port instance with temporary user paths and verify both catalog outputs and `/openai/models` match byte-for-byte.
- [ ] Verify `dist/onellm-router-v1.3.2.exe version` prints `1.3.2` and do not touch port `3457`.
