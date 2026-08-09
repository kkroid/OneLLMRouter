---
description: Draft a Conventional Commit message for reviewed changes
allowed-tools: Bash(git:*), Read(*)
argument-hint: [scope]
---

## Workflow

1. Read `AGENTS.md` and inspect `git status --short`, `git diff --stat`, and the relevant diff.
2. Confirm the proposed commit contains only the user's requested change; do not stage unrelated files.
3. Draft a Conventional Commit subject using a current scope such as `core`, `proxy`, `translate`, `retry`,
   `catalog`, `config`, `desktop`, `build`, `ci`, or `docs`.
4. Show the proposed message and the exact files it covers.

Do not run `git commit` unless the user explicitly asks for the commit. Do not add Claude Code, Happy, or
other attribution trailers unless the user or repository policy explicitly requires them.
