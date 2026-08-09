---
description: Review Go, Qt, protocol, retry, and configuration changes
allowed-tools: Read(*), Grep(*), Glob(*), Bash(git:*), Bash(go:*), Bash(cmake:*), Bash(ctest:*)
argument-hint: [file1.go file2.cpp ...] [--full]
---

## Review Scope

Read `AGENTS.md` and review the current diff unless explicit files are provided. Findings come first,
ordered by severity, with file and line references. Do not modify files or silently fix findings.

Check:

- Go errors, context cancellation, goroutine/resource lifetimes, request IDs, and secret redaction.
- Provider/model resolution and preservation of Anthropic, Chat Completions, and Responses fields.
- SSE event ordering, stream termination, first-output boundaries, and no replay after client-visible output.
- Retry status/configuration, `Retry-After`, elapsed budget, cancellation, and same-Provider semantics.
- Qt process ownership, external-instance read-only behavior, platform boundaries, and test-port isolation.
- YAML validation and atomic configuration writes when configuration code changes.
- Tests proportional to the changed behavior; run only isolated verification commands.

If there are no findings, state that clearly and list remaining test gaps or residual risk.
