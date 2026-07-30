# OneLLMRouter build and release script.
param(
    [switch]$Clean,
    [switch]$TestOnly,
    [switch]$Desktop,
    [switch]$Installer,
    [string]$Version = "1.4.0",
    [string]$QtRoot = $env:QT_ROOT,
    [string]$StageDirectory = "",
    [string]$CMake = "cmake",
    [string]$InnoSetup = "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe"
)

$ErrorActionPreference = "Stop"
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "Version must use major.minor.patch format: $Version" }
if ($Installer) { $Desktop = $true }

$outDir = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "dist"))
$portable = Join-Path $outDir "onellm-router-v$Version.exe"
$defaultStage = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "desktop\stage"))
$desktopRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "desktop"))
$stage = if ([string]::IsNullOrWhiteSpace($StageDirectory)) {
    $defaultStage
} else {
    [IO.Path]::GetFullPath($StageDirectory)
}
if (-not $stage.StartsWith($desktopRoot + [IO.Path]::DirectorySeparatorChar,
        [StringComparison]::OrdinalIgnoreCase)) {
    throw "StageDirectory must be inside $desktopRoot"
}
$core = Join-Path $stage "onellm-router-core.exe"
$desktopBuild = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "desktop\build"))

function Remove-GeneratedDirectory {
    param([string]$Path, [string]$ExpectedPath)
    $fullPath = [IO.Path]::GetFullPath($Path)
    $fullExpected = [IO.Path]::GetFullPath($ExpectedPath)
    if (-not [string]::Equals($fullPath, $fullExpected, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to clean unexpected generated path: $fullPath"
    }
    if (-not (Test-Path -LiteralPath $fullPath)) { return }
    $item = Get-Item -LiteralPath $fullPath -Force
    if (-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw "Refusing to clean non-directory or reparse point: $fullPath"
    }
    $resolved = (Resolve-Path -LiteralPath $fullPath).Path
    if (-not [string]::Equals($resolved, $fullExpected, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to clean unexpected resolved path: $resolved"
    }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}

Push-Location $PSScriptRoot
try {
    if ($TestOnly) {
        Write-Host "=== Go tests ===" -ForegroundColor Cyan
        & go test ./...
        if ($LASTEXITCODE -ne 0) { throw "Go tests failed" }
        Write-Host "Go tests passed" -ForegroundColor Green
        return
    }
    if ($Clean) {
        Remove-GeneratedDirectory -Path $outDir -ExpectedPath (Join-Path $PSScriptRoot "dist")
    }
    Remove-GeneratedDirectory -Path $stage -ExpectedPath $stage
    New-Item -ItemType Directory -Path $outDir, $stage -Force | Out-Null

    Write-Host "=== Build portable core v$Version ===" -ForegroundColor Cyan
    $ldflags = "-s -w -X main.version=$Version"
    & go build -trimpath -ldflags $ldflags -o $portable ./cmd/onellm-router/
    if ($LASTEXITCODE -ne 0) { throw "Portable core build failed" }
    Copy-Item -LiteralPath $portable -Destination $core -Force

    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw "Go tests failed" }

    if ($Desktop) {
        if ([string]::IsNullOrWhiteSpace($QtRoot)) { throw "QtRoot is required for -Desktop (or set QT_ROOT)" }
        $qtRootFull = [IO.Path]::GetFullPath($QtRoot)
        if (-not (Test-Path -LiteralPath $qtRootFull -PathType Container)) { throw "QtRoot does not exist: $qtRootFull" }
        $cmakeCommand = Get-Command $CMake -CommandType Application -ErrorAction Stop
        $ctestPath = Join-Path (Split-Path -Parent $cmakeCommand.Source) "ctest.exe"
        if (-not (Test-Path -LiteralPath $ctestPath -PathType Leaf)) {
            $ctestPath = (Get-Command "ctest" -CommandType Application -ErrorAction Stop).Source
        }

        & $cmakeCommand.Source -S (Join-Path $PSScriptRoot "desktop") -B $desktopBuild `
            "-DCMAKE_PREFIX_PATH=$qtRootFull" "-DCMAKE_BUILD_TYPE=Release" `
            "-DBUILD_TESTING=ON" "-DONELLM_VERSION=$Version"
        if ($LASTEXITCODE -ne 0) { throw "Qt configure failed" }
        & $cmakeCommand.Source --build $desktopBuild --config Release
        if ($LASTEXITCODE -ne 0) { throw "Qt build failed" }
        & $ctestPath --test-dir $desktopBuild -C Release --output-on-failure
        if ($LASTEXITCODE -ne 0) { throw "Qt tests failed" }

        $trayCandidates = @(
            (Join-Path $desktopBuild "Release\onellm-router-tray.exe"),
            (Join-Path $desktopBuild "onellm-router-tray.exe")
        )
        $traySource = $trayCandidates | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
        if (-not $traySource) { throw "Qt tray executable not found in multi-config or single-config output" }
        $trayDestination = Join-Path $stage "onellm-router-tray.exe"
        Copy-Item -LiteralPath $traySource -Destination $trayDestination -Force

        $windeployQt = Join-Path $qtRootFull "bin\windeployqt.exe"
        if (-not (Test-Path -LiteralPath $windeployQt -PathType Leaf)) { throw "windeployqt not found: $windeployQt" }
        & $windeployQt --release --no-translations $trayDestination
        if ($LASTEXITCODE -ne 0) { throw "Qt deployment failed" }

        foreach ($relativePath in @(
            "onellm-router-core.exe", "onellm-router-tray.exe", "Qt6Core.dll",
            "Qt6Gui.dll", "Qt6Widgets.dll", "Qt6Network.dll", "platforms\qwindows.dll"
        )) {
            if (-not (Test-Path -LiteralPath (Join-Path $stage $relativePath) -PathType Leaf)) {
                throw "Desktop stage is incomplete; missing $relativePath"
            }
        }
    }

    if ($Installer) {
        if ([string]::IsNullOrWhiteSpace($InnoSetup) -or -not (Test-Path -LiteralPath $InnoSetup -PathType Leaf)) {
            throw "Inno Setup compiler not found: $InnoSetup"
        }
        & $InnoSetup "/DAppVersion=$Version" (Join-Path $PSScriptRoot "installer\onellm-router.iss")
        if ($LASTEXITCODE -ne 0) { throw "Installer build failed" }
        $setup = Join-Path $outDir "OneLLMRouter-$Version-setup.exe"
        if (-not (Test-Path -LiteralPath $setup -PathType Leaf) -or (Get-Item -LiteralPath $setup).Length -le 0) {
            throw "Installer output is missing or empty: $setup"
        }
    }

    Write-Host "Built $portable" -ForegroundColor Green
    if ($Desktop) { Write-Host "Staged desktop at $stage" -ForegroundColor Green }
    if ($Installer) { Write-Host "Built $(Join-Path $outDir "OneLLMRouter-$Version-setup.exe")" -ForegroundColor Green }
}
finally {
    Pop-Location
}
