$ErrorActionPreference = "Stop"

$installerPath = Join-Path $PSScriptRoot "..\installer\onellm-router.iss"
$installer = Get-Content -LiteralPath $installerPath -Raw
$workflowPath = Join-Path $PSScriptRoot "..\.github\workflows\release.yml"
$workflow = Get-Content -LiteralPath $workflowPath -Raw
if ($installer -notmatch '(?m)^CloseApplications=yes\s*$') {
    throw "Installer does not close applications during upgrade"
}
if ($installer -notmatch '(?m)^CloseApplicationsFilter=\{#AppExeName\}\s*$') {
    throw "Installer does not limit Restart Manager shutdown to the tray"
}
if ($installer -notmatch '(?m)^RestartApplications=yes\s*$') {
    throw "Installer does not restart applications after upgrade"
}
if ($workflow -notmatch 'tools[\\/]installer-running-upgrade-test\.ps1') {
    throw "Release workflow does not run the live installer upgrade test"
}

$integrationPath = Join-Path $PSScriptRoot "installer-running-upgrade-test.ps1"
if (-not (Test-Path -LiteralPath $integrationPath -PathType Leaf)) {
    throw "Live installer upgrade test script is missing"
}
$integration = Get-Content -LiteralPath $integrationPath -Raw
if ($integration -notmatch '\$env:GITHUB_ACTIONS\s+-ne\s+"true"' -or
    $integration -notmatch '\$env:RUNNER_ENVIRONMENT\s+-ne\s+"github-hosted"') {
    throw "Live installer upgrade test is not restricted to GitHub-hosted runners"
}
if ($workflow -notmatch 'installer-running-upgrade-test\.ps1\s+`[\r\n]+\s*-Setup\s+\S+\s+`[\r\n]+\s*-ProtectedPorts\s+3456,3457') {
    throw "Release workflow does not protect the production ports during the live upgrade test"
}
if ($integration -notmatch '\[Diagnostics\.Process\]::new\(\)' -or
    $integration -notmatch '\$originalTray\.WaitForExit\(' -or
    $integration -notmatch '\$originalTray\.ExitCode') {
    throw "Live installer upgrade test does not track the exact tray process"
}
if ($integration -notmatch '\$process\.WaitForExit\(\)' -or
    $integration -notmatch '\$process\.ExitCode') {
    throw "Live installer upgrade test does not wait for installer exit codes"
}
if ($integration -notmatch "@\('/TASKS=autostart'\)") {
    throw "Live installer upgrade test does not explicitly enable autostart"
}
if ($integration -notmatch '\$beforeCorePID' -or
    $integration -notmatch '-DifferentFromPID\s+\$beforeCorePID' -or
    $integration -notmatch '\$stable\.pid\s+-ne\s+\[int\]\$after\.pid') {
    throw "Live installer upgrade test does not verify core replacement"
}
if ($integration -notmatch 'running upgrade marker' -or
    $integration -notmatch 'Wait-DynamicPortClosed') {
    throw "Live installer upgrade test does not verify configuration preservation and shutdown"
}
if ($integration -notmatch 'Get-FileHash' -or
    ([regex]::Matches($integration, 'Assert-ConfigUnchanged').Count -lt 3)) {
    throw "Live installer upgrade test does not compare the complete configuration"
}
if ($integration -notmatch 'Running upgrade changed the desktop autostart value') {
    throw "Live installer upgrade test does not verify autostart after upgrade"
}
if ($integration -notmatch '/NORESTARTAPPLICATIONS' -or
    $integration -notmatch 'Non-restarting upgrade changed the desktop autostart value') {
    throw "Live installer upgrade test does not quiesce the installed tray before uninstall"
}
if ($integration -match '(?i)\bGet-Process\b|\bStop-Process\b|\btaskkill(?:\.exe)?\b') {
    throw "Live installer upgrade test must not enumerate or terminate processes by name"
}

Write-Host "Installer running-upgrade contract passed" -ForegroundColor Green
