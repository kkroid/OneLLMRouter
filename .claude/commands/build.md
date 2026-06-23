---
description: Go 编译 + CMake QT 编译 + 安装包构建
allowed-tools: Bash(go:*), Bash(cmake:*), Bash(cd *), Bash(mkdir *), Bash(cp *), Bash(curl:*), Bash(protoc:*), Bash(ls *), Bash(cat *)
argument-hint: [--clean] [--debug] [--panel-only] [--daemon-only]
---

## 项目构建

OneLLMRouter 包含两个构建目标：Go 后台守护进程 + QT/C++ 桌面面板。

### 1. 检测构建工具

!`which go 2>/dev/null && echo "Go: $(go version)" || echo "Go: MISSING"`
!`which cmake 2>/dev/null && echo "CMake: $(cmake --version | head -1)" || echo "CMake: MISSING"`
!`which protoc 2>/dev/null && echo "Protoc: $(protoc --version)" || echo "Protoc: MISSING"`

### 2. Go 后台守护进程（onellmd）

```bash
cd cmd/onellmd

# 依赖安装
go mod tidy

# 编译
go build -o ../../build/onellmd.exe -ldflags="-s -w" .

# 运行（开发模式，控制台输出）
./../../build/onellmd.exe --config ../../onellmd.yaml
```

### 3. proto 编译（gRPC 代码生成）

```bash
# Go 侧
protoc --go_out=. --go-grpc_out=. proto/onellm/v1/service.proto

# C++ 侧（参考 CloudiaLauncher 的 proto/CMakeLists.txt）
cmake -B build/proto proto/
cmake --build build/proto
```

### 4. QT 桌面面板

```bash
cmake -B build/panel -G "Visual Studio 17 2022" -A x64
cmake --build build/panel --config Release --parallel
```

### 5. 验证

```bash
# 启动 Go 后台
./build/onellmd.exe &
sleep 2

# 健康检查
curl http://localhost:3456/health

# gRPC 接口检查
grpcurl -plaintext 127.0.0.1:9090 list

# 启动 QT 面板
./build/panel/Release/onellm-panel.exe
```

### 参数说明

- `--clean`：构建前清理 build/ 目录
- `--debug`：Go race detector + C++ Debug 配置
- `--panel-only`：仅构建 QT 面板
- `--daemon-only`：仅构建 Go 后台
