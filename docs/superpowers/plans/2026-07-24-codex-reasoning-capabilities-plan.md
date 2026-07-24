# Codex Reasoning Capabilities Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate Codex catalogs with selectable reasoning levels, using configured model mappings first, upstream metadata second, and a four-level fallback last.

**Architecture:** `internal/config` owns the YAML representation and default GPT-5.5+ mappings. `internal/catalog` carries upstream reasoning metadata, applies configured overrides by base model slug, and renders a safe fallback. The proxy endpoint and startup file generator continue sharing the same catalog service and serializer.

**Tech Stack:** Go 1.24, YAML v3, `encoding/json`, existing catalog/proxy tests.

---

### Task 1: Configuration mapping

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `onellm-router.example.yaml`
- Modify: `README.md`

- [ ] Add failing tests for the four GPT-5.5+ defaults and explicit YAML override.
- [ ] Add the YAML model capability mapping and official GPT-5.5+ defaults.
- [ ] Document the mapping without changing unrelated configuration.

### Task 2: Preserve and resolve reasoning metadata

**Files:**
- Modify: `internal/catalog/catalog.go`
- Modify: `internal/catalog/catalog_test.go`
- Modify: `internal/catalog/codex.go`
- Modify: `internal/catalog/codex_test.go`

- [ ] Add failing tests for upstream metadata, configured override, and unknown fallback.
- [ ] Implement only the metadata required by the Codex reasoning picker.

### Task 3: Startup and HTTP wiring

**Files:**
- Modify: `cmd/onellm-router/catalog.go`
- Modify: `cmd/onellm-router/catalog_test.go`
- Modify: `cmd/onellm-router/main.go`
- Modify: `internal/proxy/model_list_test.go`

- [ ] Convert YAML mappings once and configure the shared catalog service.
- [ ] Verify HTTP and generated files contain identical metadata.

### Task 4: Verification and build

- [ ] Run targeted and full tests, vet, and diff checks.
- [ ] Build `dist/onellm-router-v1.3.2.exe` and confirm its version.
- [ ] Verify model selection with isolated Codex state; do not touch port `3457` or the real catalog.
