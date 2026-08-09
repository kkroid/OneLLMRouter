---
description: Build and verify the current Go Core and optional Qt Desktop
allowed-tools: Bash(go:*), Bash(pwsh:*), Bash(cmake:*), Bash(ctest:*), Bash(git:*)
argument-hint: [--go-only|--desktop]
---

## Workflow

Read `AGENTS.md` first. Never start the production Router or use the installer against the active local
installation. Run from the repository root and preserve unrelated working-tree changes.

### Go Core

```powershell
go test ./...
go vet ./...
go build ./cmd/onellm-router/
```

The repository wrapper for portable verification is:

```powershell
pwsh -NoProfile -File .\build.ps1 -TestOnly
```

### Qt Desktop

Only run this when `QT_ROOT` and CMake are available:

```powershell
cmake -S desktop -B desktop/build -DCMAKE_PREFIX_PATH="$env:QT_ROOT" -DBUILD_TESTING=ON
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
```

Report each command and its exit status. Do not claim an installer build unless `-Installer` was explicitly
requested and the build ran in an isolated environment.
