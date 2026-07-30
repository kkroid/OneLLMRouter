# OneLLMRouter Qt Tray Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the native Windows tray with a Qt 6 tray that safely supervises only its own Go child, attaches read-only to an existing instance, and reports the configured SOCKS5 boundary without controlling OneProxy.

**Architecture:** Build a small Qt application around three focused units: configuration/health discovery, owned-child lifecycle, and tray presentation. Use `QProcess` for the Go core, `QNetworkAccessManager` for `/health`, and `QTcpSocket` for local proxy reachability; never terminate by process name or PID discovered from the network.

**Tech Stack:** C++17, Qt 6.8.3 Widgets/Network/Test, CMake 3.16+, MSVC 2022, existing Go core from the desktop-core plan.

---

## Safety Gate

The tray treats any already-running OneLLMRouter as `External`. External instances have no stop or restart operation. `RouterProcess` must not expose a generic PID-kill method, and no test may call `taskkill` or `Stop-Process`.

### Task 1: Qt Project and State Contract

**Files:**
- Create: `desktop/CMakeLists.txt`
- Create: `desktop/src/main.cpp`
- Create: `desktop/src/router_types.h`
- Create: `desktop/tests/CMakeLists.txt`
- Create: `desktop/tests/router_types_test.cpp`
- Modify: `.gitignore`

- [ ] **Step 1: Write a failing state-policy test**

Define the expected safety rule before process code exists:

```cpp
void RouterTypesTest::onlyOwnedProcessCanBeControlled()
{
    QVERIFY(!canControlRouter(ProcessOwnership::None));
    QVERIFY(!canControlRouter(ProcessOwnership::External));
    QVERIFY(canControlRouter(ProcessOwnership::Owned));
}
```

- [ ] **Step 2: Add the smallest shared types**

```cpp
enum class ProcessOwnership { None, Owned, External };
enum class RouterState { Stopped, Starting, Healthy, Degraded, Conflict, Error };

struct RouterHealth {
    bool valid = false;
    QString service;
    QString version;
    QString proxySocks5;
    int pid = 0;
    int port = 0;
    int models = 0;
    bool copilotToken = false;
};

constexpr bool canControlRouter(ProcessOwnership ownership)
{
    return ownership == ProcessOwnership::Owned;
}
```

- [ ] **Step 3: Create a source-buildable CMake project**

Use environment/configuration input instead of a hardcoded Qt installation:

```cmake
cmake_minimum_required(VERSION 3.16)
project(OneLLMRouterDesktop VERSION 1.4.0 LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(ONELLM_VERSION "1.4.0" CACHE STRING "OneLLMRouter release version")
include(CTest)
find_package(Qt6 REQUIRED COMPONENTS Widgets Network Test)

add_executable(onellm-router-tray WIN32
    src/main.cpp
)
target_link_libraries(onellm-router-tray PRIVATE Qt6::Widgets Qt6::Network)
target_compile_definitions(onellm-router-tray PRIVATE ONELLM_VERSION="${ONELLM_VERSION}")

if(BUILD_TESTING)
    add_subdirectory(tests)
endif()
```

Add `desktop/build/` to `.gitignore`. Do not commit Qt DLLs, executables, generated MOC files, or CMake output.

- [ ] **Step 4: Configure, build, test, and commit**

```powershell
cmake -S desktop -B desktop/build -DCMAKE_PREFIX_PATH="$env:QT_ROOT" -DBUILD_TESTING=ON
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
git add .gitignore desktop/CMakeLists.txt desktop/src/main.cpp desktop/src/router_types.h desktop/tests
git commit -m "build: scaffold Qt desktop tray"
```

Expected: the state-policy test passes and no process or port is touched.

### Task 2: Configuration and Health Discovery

**Files:**
- Create: `desktop/src/router_discovery.h`
- Create: `desktop/src/router_discovery.cpp`
- Create: `desktop/tests/router_discovery_test.cpp`
- Modify: `desktop/CMakeLists.txt`
- Modify: `desktop/tests/CMakeLists.txt`

- [ ] **Step 1: Test secret-free config and health parsing**

```cpp
void RouterDiscoveryTest::parsesExpectedRouterHealth()
{
    const auto health = parseRouterHealth(R"({
        "status":"ok","service":"onellm-router","pid":42,"version":"1.4.0",
        "http_port":45678,"models":2,"copilot_token":true,
        "proxy_socks5":"127.0.0.1:1082"
    })");
    QVERIFY(health.valid);
    QCOMPARE(health.service, QString("onellm-router"));
    QCOMPARE(health.pid, 42);
    QCOMPARE(health.proxySocks5, QString("127.0.0.1:1082"));
}

void RouterDiscoveryTest::rejectsAnotherServiceOnThePort()
{
    const auto health = parseRouterHealth(R"({"status":"ok","service":"other"})");
    QVERIFY(!health.valid);
}
```

- [ ] **Step 2: Run the test and verify RED**

Build and run `ctest --test-dir desktop/build -C Release --output-on-failure`.

Expected: compilation fails because `parseRouterHealth` is undefined.

- [ ] **Step 3: Implement strict JSON parsing**

```cpp
RouterHealth parseRouterHealth(const QByteArray &payload)
{
    const auto document = QJsonDocument::fromJson(payload);
    if (!document.isObject()) return {};
    const auto object = document.object();
    if (object.value("status").toString() != "ok" ||
        object.value("service").toString() != "onellm-router") return {};

    RouterHealth result;
    result.valid = true;
    result.service = object.value("service").toString();
    result.version = object.value("version").toString();
    result.proxySocks5 = object.value("proxy_socks5").toString();
    result.pid = object.value("pid").toInt();
    result.port = object.value("http_port").toInt();
    result.models = object.value("models").toInt();
    result.copilotToken = object.value("copilot_token").toBool();
    return result;
}
```

- [ ] **Step 4: Add discovery orchestration**

`RouterDiscovery` must:

1. Run `onellm-router-core.exe --config <path> config-info --json` and parse only the documented non-secret fields.
2. GET `http://<host>:<port>/health` with a two-second timeout.
3. Emit `externalRouterFound` only for a valid OneLLMRouter payload.
4. Emit `portConflict` when TCP/HTTP responds but identity is invalid.
5. Emit `routerAbsent` only for connection-refused/unreachable results.

Use `QProcess::setProgram`, `setArguments`, and `QNetworkRequest`; never build a shell command string.

- [ ] **Step 5: Verify and commit**

```powershell
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
git add desktop/src/router_discovery.* desktop/tests/router_discovery_test.cpp desktop/CMakeLists.txt desktop/tests/CMakeLists.txt
git commit -m "feat: discover router state from Qt"
```

Expected: malformed JSON and another service are rejected, with no live network tests.

### Task 3: Owned Child Lifecycle

**Files:**
- Create: `desktop/src/router_process.h`
- Create: `desktop/src/router_process.cpp`
- Create: `desktop/tests/router_process_test.cpp`
- Modify: `desktop/CMakeLists.txt`
- Modify: `desktop/tests/CMakeLists.txt`

- [ ] **Step 1: Test argument construction and ownership guard**

```cpp
void RouterProcessTest::usesExplicitTrayChildArguments()
{
    const auto args = routerChildArguments(R"(C:\Users\test\.onellm\onellm-router.yaml)");
    QCOMPARE(args, QStringList({"serve", "--tray-child", "--config",
                                R"(C:\Users\test\.onellm\onellm-router.yaml)"}));
}

void RouterProcessTest::externalInstanceCannotStop()
{
    RouterProcess process;
    process.attachExternalForTest();
    QVERIFY(!process.requestGracefulStop());
}
```

- [ ] **Step 2: Implement process start without a shell**

```cpp
QStringList routerChildArguments(const QString &configPath)
{
    return {"serve", "--tray-child", "--config", configPath};
}
```

`RouterProcess::startOwned` must set the program to the core executable next to the tray, pass the argument list above, set the working directory to the application directory, and use `setCreateProcessArgumentsModifier` with `CREATE_NO_WINDOW` on Windows. Record `Owned` only after `QProcess::started` fires.

- [ ] **Step 3: Implement graceful stop with no kill fallback**

```cpp
bool RouterProcess::requestGracefulStop()
{
    if (m_ownership != ProcessOwnership::Owned ||
        m_process.state() == QProcess::NotRunning) return false;
    if (m_process.write("shutdown\n") < 0) return false;
    m_stopTimer.start(30000);
    return true;
}
```

When the 30-second timer expires, emit `gracefulStopTimedOut` and leave both process and tray running. Do not call `kill()`, `terminate()`, `taskkill`, or `Stop-Process`. Restart is implemented as graceful stop followed by a new start only after `QProcess::finished`.

- [ ] **Step 4: Ensure attached mode is read-only**

`attachExternal` stores health data and sets ownership to `External` without creating a `QProcess`. Start, stop, and restart return false in this state.

- [ ] **Step 5: Verify and commit**

```powershell
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
git add desktop/src/router_process.* desktop/tests/router_process_test.cpp desktop/CMakeLists.txt desktop/tests/CMakeLists.txt
git commit -m "feat: supervise only tray-owned router child"
```

Expected: process-policy tests pass; no test launches or stops the production binary.

### Task 4: Proxy Reachability and Tray Presentation

**Files:**
- Create: `desktop/src/proxy_probe.h`
- Create: `desktop/src/proxy_probe.cpp`
- Create: `desktop/src/tray_application.h`
- Create: `desktop/src/tray_application.cpp`
- Create: `desktop/src/i18n.h`
- Create: `desktop/tests/proxy_probe_test.cpp`
- Create: `desktop/tests/autostart_test.cpp`
- Modify: `desktop/src/main.cpp`
- Modify: `desktop/CMakeLists.txt`
- Modify: `desktop/tests/CMakeLists.txt`

- [ ] **Step 1: Test SOCKS5 address parsing**

```cpp
void ProxyProbeTest::parsesIPv4HostAndPort()
{
    const auto endpoint = parseProxyEndpoint("127.0.0.1:1082");
    QVERIFY(endpoint.has_value());
    QCOMPARE(endpoint->host, QString("127.0.0.1"));
    QCOMPARE(endpoint->port, quint16(1082));
}

void ProxyProbeTest::rejectsInvalidPort()
{
    QVERIFY(!parseProxyEndpoint("127.0.0.1:70000").has_value());
}
```

- [ ] **Step 2: Implement a local-only proxy probe**

Use `QTcpSocket::connectToHost` with a one-second timer. A successful TCP connection means `reachable`; timeout/refusal means `unreachable`. Do not request Google or any provider endpoint in the tray poller.

- [ ] **Step 3: Build the menu from current state**

`TrayApplication` rebuilds its menu only from `QMenu::aboutToShow`. It shows:

```text
OneLLMRouter 1.4.0 - Healthy
Models: 2 | Port: 3456
Proxy: 127.0.0.1:1082 - Reachable
Copilot token: Available
----------------------------
Restart Router
Stop Router
Open Configuration
Open Logs
Start on login [x]
Quit
```

For an external instance, replace the controls with a disabled `Managed by another process` item. For a conflict, show the conflicting port and no destructive action.

- [ ] **Step 4: Implement autostart as a pure quoted command plus registry adapter**

Test the command independently:

```cpp
QString autoStartCommand(const QString &exe, const QString &config)
{
    return QString("\"%1\" --config \"%2\"").arg(exe, config);
}
```

Use `QSettings` with `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run` and the value name `OneLLMRouter Desktop`. Toggling it must not modify the legacy `OneLLMRouter` CLI auto-start value.

- [ ] **Step 5: Add state-change notifications and localization**

Notify only on transitions into conflict, down, or degraded states and on recovery. Rate-limit identical notifications for five minutes. Keep strings in a small `Strings` struct selected by `QLocale`, following OneProxy's proven approach without copying OneProxy-specific actions.

- [ ] **Step 6: Verify and commit**

```powershell
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
git add desktop/src desktop/tests desktop/CMakeLists.txt
git commit -m "feat: add Qt router tray experience"
```

Expected: all Qt tests pass and no production port is contacted by unit tests.

- [ ] **Step 7: Add a non-interactive tray smoke mode**

Accept `--smoke-test <absolute-result-path>` in `main.cpp`. In this mode, create `RouterDiscovery` and `RouterProcess` without displaying `QSystemTrayIcon`, require the state to become `Owned` and healthy, then write this JSON atomically:

```json
{"service":"onellm-router","ownership":"owned","healthy":true,"pid":1234,"port":45678}
```

After writing the result, call `requestGracefulStop()` and exit zero only after `QProcess::finished`. Exit non-zero on config failure, conflict, external attachment, startup timeout, or graceful-stop timeout. This mode must use the same discovery and process classes as the interactive tray; it must not call `kill()` or introduce a separate startup path.

### Task 5: Reuse the Existing Brand Icons

**Files:**
- Modify: `internal/ui/gen_icons.py`
- Create: `desktop/assets/green.ico`
- Create: `desktop/assets/yellow.ico`
- Create: `desktop/assets/red.ico`
- Create: `desktop/assets/icons.qrc`
- Modify: `desktop/CMakeLists.txt`

- [ ] **Step 1: Extend the existing deterministic generator**

Keep the rounded-square background and white hexagon drawing, and add ICO output at 16, 24, 32, 48, and 256 pixels:

```python
from pathlib import Path

DESKTOP_ASSET_DIR = Path(__file__).resolve().parents[2] / "desktop" / "assets"
DESKTOP_ASSET_DIR.mkdir(parents=True, exist_ok=True)

master = draw_master_icon(bg_rgb)
master.save(
    DESKTOP_ASSET_DIR / f"{name}.ico",
    format="ICO",
    sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (256, 256)],
)
```

Refactor `draw_icon` into `draw_master_icon` plus the existing 16-pixel BMP resource conversion so the old and new assets come from one drawing definition.

- [ ] **Step 2: Generate and inspect all states**

```powershell
Push-Location internal/ui
python gen_icons.py
Pop-Location
```

Expected: the three existing `.bin` files and the three Qt `.ico` files are generated. Inspect them at original resolution before committing.

- [ ] **Step 3: Embed icons through Qt resources and commit**

Reference `:/icons/green.ico`, `:/icons/yellow.ico`, and `:/icons/red.ico` from `TrayApplication`, then:

```powershell
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
git add internal/ui/gen_icons.py internal/ui/*.bin desktop/assets desktop/CMakeLists.txt desktop/src/tray_application.cpp
git commit -m "feat: add branded Qt status icons"
```

### Task 6: Retire the Native Tray After Qt Acceptance

**Files:**
- Delete: `internal/ui/icon.go`
- Delete: `internal/ui/notify.go`
- Delete: `internal/ui/status.go`
- Delete: `internal/ui/tray.go`
- Delete: `internal/ui/tray_test.go`
- Delete: `internal/ui/green.bin`
- Delete: `internal/ui/yellow.bin`
- Delete: `internal/ui/red.bin`
- Move: `internal/ui/gen_icons.py` to `desktop/assets/gen_icons.py`
- Modify: `cmd/onellm-router/main.go`
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Verify Qt acceptance before deletion**

Run all Qt tests and manually inspect the Qt menu using a temporary config and a non-production port. Do not continue if the tray cannot attach read-only to the current instance without exposing stop/restart actions.

- [ ] **Step 2: Remove native tray startup and package**

Delete the `internal/ui` import and all `ui.NewTray`, `ui.SetBell`, and native notification calls. The Go core waits only on signals or tray-child standard input. Preserve `server.bell` in configuration because the Qt tray consumes it through `config-info`.

- [ ] **Step 3: Move the icon generator and verify no orphan imports**

```powershell
rg -n 'internal/ui|ui\.' -g '*.go'
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
git diff --check
```

Expected: `rg` returns no matches and every verification command exits zero.

- [ ] **Step 4: Commit the replacement**

```powershell
git add -A internal/ui desktop/assets cmd/onellm-router/main.go README.md CLAUDE.md
git commit -m "refactor: replace native tray with Qt desktop"
```
