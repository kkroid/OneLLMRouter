$ErrorActionPreference = "Stop"

$installerPath = Join-Path $PSScriptRoot "..\installer\onellm-router.iss"
$installer = Get-Content -LiteralPath $installerPath -Raw
$legacyDeletePattern =
    'ValueType:\s*none;\s*ValueName:\s*"OneLLMRouter";\s*Flags:\s*deletevalue'

if ($installer -notmatch $legacyDeletePattern) {
    throw "Installer does not remove the legacy OneLLMRouter Run value"
}

Write-Host "Installer autostart migration contract passed" -ForegroundColor Green
