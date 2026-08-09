# OneLLMRouter Agent Guide

## Product Context

OneLLMRouter is a local multi-provider gateway for Claude Code, Codex, and compatible clients.
Configure providers once, keep all configured providers in the model catalog, and select a namespaced
`provider/model` from the client. Do not introduce a separate runtime Provider-switching state.

The current product work is tracked in [the v1.5.0 plan](docs/superpowers/specs/2026-08-09-v1.5.0-plan.md).
That plan is a design target, not permission to implement every item in one change.

## Repository Map

- `cmd/onellm-router`: Cobra CLI, server lifecycle, health and desktop contracts.
- `internal/config`: YAML configuration and validation.
- `internal/catalog`: Provider model discovery and Codex catalog generation.
- `internal/router`: Provider and namespaced model resolution.
- `internal/proxy`: HTTP endpoints, protocol forwarding/translation, and upstream failures.
- `internal/translate`: Anthropic/OpenAI request, response, and stream translation.
- `internal/upstream`: bounded retry, timeout, cancellation, and error sanitization.
- `internal/log`: structured request logging and daily rotation.
- `desktop`: Qt 6 tray, process ownership, discovery, and tests.
- `installer`: Windows per-user installer; platform packaging is intentionally separate.

## Engineering Rules

- Read the relevant code, tests, and design document before changing behavior.
- State assumptions and unresolved choices before implementation. Ask when a choice changes product behavior.
- Keep changes surgical. Do not add speculative abstractions, unrelated cleanup, or new features.
- Preserve existing user changes in a dirty worktree. Never reset, checkout, or overwrite unrelated work.
- Keep secrets out of logs, test fixtures, generated files, and responses. Do not print API keys.
- Preserve the existing public protocol contracts and namespaced model selection semantics.
- Retry the explicitly selected Provider/model only. Do not add Auto Failover, MCP, Skill, or Prompt management
  unless the user explicitly changes the product scope.

## Safety Boundaries

- Never kill, terminate, or restart a production Router by image name, PID, `taskkill`, or `Stop-Process`.
- Do not run installer `install`/`uninstall` workflows against the active local installation.
- Tests must use `httptest` or an isolated loopback port and must target only the process they created.
- Do not bind the configured production port in unit or integration tests. Clean up exact test process objects.
- A Qt process attached to an external Core is read-only; it must not expose stop, restart, or config-write actions.
- A stream that has already produced client-visible output must not be replayed automatically.

## Verification

For Go changes, use focused package tests first, then as risk requires:

```text
go test ./...
go test -race ./...
go vet ./...
```

For Qt changes, use a configured build directory and an offscreen test environment where needed:

```text
cmake -S desktop -B desktop/build -DCMAKE_PREFIX_PATH="$env:QT_ROOT" -DBUILD_TESTING=ON
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
```

Use `pwsh -NoProfile -File ./build.ps1 -TestOnly` for the repository's portable Go verification.
Do not claim a test passed without reporting the command and result. For documentation-only changes,
run `git diff --check` and validate all referenced paths.

## Source Of Truth

- User-facing behavior: `README.md`, `README.en.md`, and the current configuration example.
- Version design: `docs/superpowers/specs/` and its linked implementation plan.
- Runtime behavior: the code and tests in the relevant package.
- Do not copy a version number, local machine detail, or generated artifact name into this guide.
