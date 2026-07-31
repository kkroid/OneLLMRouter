$ErrorActionPreference = "Stop"

$smokeScript = Join-Path $PSScriptRoot "desktop-smoke.ps1"
$source = Get-Content -LiteralPath $smokeScript -Raw

if ($source -match 'GetProcessById' -or $source -match '\$coreProcess\.Kill\s*\(') {
    throw "desktop-smoke.ps1 must not terminate a process recovered only from a health PID"
}
if ($source -notmatch '\$trayProcess\.Kill\s*\(') {
    throw "desktop-smoke.ps1 no longer cleans up the tray process it started"
}

Write-Host "Desktop smoke cleanup safety test passed" -ForegroundColor Green
