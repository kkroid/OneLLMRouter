$ErrorActionPreference = "Stop"

$installerPath = Join-Path $PSScriptRoot "..\installer\onellm-router.iss"
$installer = Get-Content -LiteralPath $installerPath -Raw
if ($installer -notmatch '(?m)^CloseApplications=yes\s*$') {
    throw "Installer does not close applications during upgrade"
}
if ($installer -notmatch '(?m)^RestartApplications=yes\s*$') {
    throw "Installer does not restart applications after upgrade"
}

Write-Host "Installer running-upgrade contract passed" -ForegroundColor Green
