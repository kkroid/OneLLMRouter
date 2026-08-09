# OneLLMRouter Claude Code Guide

The repository-wide rules are in [AGENTS.md](AGENTS.md). Read that file first; it applies to every
agent. This file only adds Claude Code-specific entry points and quick context.

## Product Context

OneLLMRouter is the local multi-provider entry point for Claude Code and Codex: configure providers once,
keep them all available in the model catalog, and switch models as `provider/model` inside the client.
The Go Core exposes Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses endpoints. The Qt
Desktop shell owns presentation and the lifecycle of a Core child; an externally started Core is read-only.

## Repository Map

```text
cmd/onellm-router/  Cobra CLI and server lifecycle
internal/config/    YAML configuration and validation
internal/catalog/   Provider discovery and Codex model catalog
internal/router/    Provider/model resolution
internal/proxy/     HTTP forwarding, translation entry points, retry errors
internal/translate/ Protocol-neutral request/response/stream translation
internal/upstream/  Bounded retry, timeout, cancellation, sanitization
desktop/            Qt tray, process ownership, discovery, tests
installer/          Windows installer and upgrade contracts
```

For v1.5.0 work, start with [the version plan](docs/superpowers/specs/2026-08-09-v1.5.0-plan.md), then
read the package tests before choosing an implementation boundary.

## Claude Code Commands

| Command | Purpose |
| --- | --- |
| `/build` | Run the current Go/Qt build and test workflow |
| `/plan` | Turn a requested change into scoped, verifiable tasks |
| `/review` | Review the current diff for correctness, safety, protocol, and test gaps |
| `/commit` | Draft a Conventional Commit message without committing automatically |

These commands are helpers, not a substitute for the repository rules or the user's scope.

## Claude-Specific Notes

- Use the repository's actual `build.ps1`, `CMakeLists.txt`, Go packages, and tests; do not assume a previous
  `onellmd`, gRPC, proto, or panel architecture.
- Do not add Claude Code or Happy attribution trailers unless the user or repository policy explicitly requires them.
- Do not start the production Router while investigating. Prefer isolated fixtures and dynamic ports.
- Keep design decisions in the linked design documents and keep implementation changes in the declared task scope.
