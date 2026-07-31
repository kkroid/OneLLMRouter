$ErrorActionPreference = "Stop"

$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$buildScript = Join-Path $repoRoot "build.ps1"
$sourceDirectory = Join-Path $repoRoot "desktop\src"
$env:GOCACHE = Join-Path $repoRoot "desktop\build\build-safety-go-cache"

$output = & pwsh -NoProfile -File $buildScript -TestOnly -StageDirectory $sourceDirectory 2>&1
$exitCode = $LASTEXITCODE

if ($exitCode -eq 0) {
    throw "build.ps1 accepted a source directory as StageDirectory"
}
if (($output | Out-String) -notmatch "StageDirectory") {
    throw "build.ps1 rejected the path for an unexpected reason: $output"
}

Write-Host "Build stage safety test passed" -ForegroundColor Green
