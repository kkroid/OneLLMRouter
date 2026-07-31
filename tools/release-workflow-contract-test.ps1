$ErrorActionPreference = "Stop"

$workflowPath = Join-Path $PSScriptRoot "..\.github\workflows\release.yml"
$workflow = Get-Content -LiteralPath $workflowPath -Raw

if ($workflow -match '(?m)^\s*uses:\s*[^\s]+@v\d+(?:\.\d+)*\s*$') {
    throw "Release workflow contains an action pinned only to a movable version tag"
}
foreach ($usesLine in [regex]::Matches(
    $workflow, '(?m)^\s*uses:\s*([^\s#]+)(?:\s+#.*)?$')) {
    if ($usesLine.Groups[1].Value -notmatch '@[0-9a-f]{40}$') {
        throw "Release workflow action is not pinned to a full commit SHA: $($usesLine.Groups[1].Value)"
    }
}
if ($workflow -notmatch '(?ms)^permissions:\s*\r?\n\s+contents:\s*read\s*$') {
    throw "Release workflow does not default to read-only contents permission"
}
if ($workflow -notmatch
    '(?ms)^\s{2}publish-release:.*?^\s{4}permissions:\s*\r?\n\s{6}contents:\s*write\s*$') {
    throw "Release publishing is not isolated in a write-enabled job"
}

Write-Host "Release workflow security contract passed" -ForegroundColor Green
