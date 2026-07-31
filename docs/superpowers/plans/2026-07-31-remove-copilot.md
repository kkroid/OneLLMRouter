# Remove Copilot Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove every product and runtime dependency on GitHub Copilot while preserving configurable Anthropic, OpenAI Chat, and OpenAI Responses providers.

**Architecture:** Remove the dedicated authentication and proxy branch instead of replacing it with a compatibility layer. Keep protocol conversion independent because configured providers continue to use it. Simplify the health and desktop contracts by removing token state.

**Tech Stack:** Go 1.25, Qt 6.8.3/C++17, PowerShell, Markdown/YAML

---

### Task 1: Remove Go Copilot Runtime

**Files:**
- Delete: `internal/auth/auth.go`
- Delete: `internal/auth/auth_test.go`
- Modify: `cmd/onellm-router/main.go`
- Modify: `cmd/onellm-router/health.go`
- Modify: `cmd/onellm-router/health_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/proxy/handler.go`
- Modify: `internal/proxy/proxy_test.go`
- Modify: `internal/proxy/timeouts.go`
- Modify: `internal/proxy/timeouts_test.go`
- Modify: `internal/router/provider.go`
- Modify: `internal/router/resolver.go`
- Modify: `internal/router/resolver_test.go`
- Modify: `tools/torturetest.ps1`

- [ ] Change tests so `cp` has no built-in endpoint behavior and health omits token state.
- [ ] Run focused tests and verify they fail against the Copilot implementation.
- [ ] Delete authentication, token, Copilot proxy, timeout, and resolver branches.
- [ ] Run `gofmt`, `go test -count=1 ./...`, and `go vet ./...`.

### Task 2: Remove Desktop Copilot Contract

**Files:**
- Modify: `desktop/src/router_types.h`
- Modify: `desktop/src/router_discovery.cpp`
- Modify: `desktop/src/i18n.h`
- Modify: `desktop/src/tray_application.cpp`
- Modify: `desktop/tests/router_discovery_test.cpp`
- Modify: `desktop/tests/tray_application_test.cpp`

- [ ] Change tests so health has no `copilot_token` and the menu has no token row.
- [ ] Run focused tests and verify the old contract fails.
- [ ] Remove token parsing, state, strings, and menu presentation.
- [ ] Build and run the Qt test suite.

### Task 3: Remove Product Configuration and Documentation

**Files:**
- Modify: `onellm-router.example.yaml`
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Delete: `go-cli-checklist.md`
- Modify: current desktop design documents that define health token state

- [ ] Delete the Copilot provider and `cp` model-slot examples.
- [ ] Remove login, token, Copilot API, and built-in-prefix documentation.
- [ ] Run a case-insensitive repository search and classify any historical-only references.

### Task 4: Integrated Verification and Review

**Files:** All changed files.

- [ ] Run Go tests, race tests, vet, module verification, Qt tests, and diff checks.
- [ ] Confirm current source/config/docs have no executable Copilot path.
- [ ] Request independent specification and code-quality reviews.
- [ ] Commit the removal as a focused change.
- [ ] Re-run the v1.4.0 audit and begin fixing the remaining findings.
