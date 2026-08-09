---
description: Break a OneLLMRouter change into scoped, verifiable tasks
allowed-tools: Read(*), Grep(*), Glob(*), Bash(git:*)
argument-hint: <task description>
---

## Planning Rules

Read `AGENTS.md`, the relevant README sections, current tests, and any linked design document. Do not assume
the old `onellmd`, gRPC, proto, or panel architecture.

For the requested change, produce:

1. Current behavior and the concrete success criterion.
2. Design gate: whether the change needs a repository-backed design decision.
3. Task steps with exact repository-relative write scopes.
4. Interfaces, persisted data, compatibility, and security implications.
5. Focused verification commands and expected evidence.
6. Non-goals and unresolved choices.

Keep the smallest plan that fully covers the requested behavior. Do not edit files or implement the plan
unless the user asks for that separately.
