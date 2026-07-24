# OneLLMRouter Build Script
param(
    [switch]$Clean,
    [switch]$TestOnly,
    [switch]$Install,
    [string]$Version = "1.3.2"
)

$ErrorActionPreference = "Stop"
$OutDir = "dist"
$Binary = "$OutDir\onellm-router-v$Version.exe"

if ($TestOnly) {
    Write-Host "=== 测试 ===" -ForegroundColor Cyan
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "测试失败" }
    Write-Host "测试全部通过" -ForegroundColor Green
    exit 0
}

if ($Clean) {
    Write-Host "=== 清理 ===" -ForegroundColor Yellow
    Remove-Item -Recurse -Force $OutDir -ErrorAction SilentlyContinue
}

Write-Host "=== 编译 v$Version ===" -ForegroundColor Cyan
New-Item -ItemType Directory -Force $OutDir | Out-Null
$ldflags = "-s -w -X main.version=$Version"
go build -ldflags="$ldflags" -o $Binary ./cmd/onellm-router/
if ($LASTEXITCODE -ne 0) { throw "编译失败" }

$size = (Get-Item $Binary).Length
Write-Host "  $Binary ($('{0:N0}' -f $size) bytes)" -ForegroundColor Green

Write-Host "=== 测试 ===" -ForegroundColor Cyan
go test ./...
if ($LASTEXITCODE -ne 0) { throw "测试失败" }
Write-Host "  测试全部通过" -ForegroundColor Green

if ($Install) {
    Write-Host "=== 安装开机自启 ===" -ForegroundColor Cyan
    & $Binary install
}

Write-Host ""
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Green
Write-Host "  onellm-router v$Version" -ForegroundColor Green
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Green
Write-Host ""
Write-Host "  .\$Binary              # 启动"
Write-Host "  .\$Binary --daemon     # 后台"
Write-Host "  .\$Binary status       # 状态"
Write-Host "  .\$Binary install      # 开机自启"
