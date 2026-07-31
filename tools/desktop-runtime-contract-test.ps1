param(
    [Parameter(Mandatory = $true)][string]$StageDirectory
)

$ErrorActionPreference = "Stop"
$stagePath = (Resolve-Path -LiteralPath $StageDirectory -ErrorAction Stop).Path
$requiredRuntimeFiles = @(
    "msvcp140.dll",
    "msvcp140_1.dll",
    "msvcp140_2.dll",
    "vcruntime140.dll",
    "vcruntime140_1.dll"
)

$missing = @(
    foreach ($fileName in $requiredRuntimeFiles) {
        $runtimePath = Join-Path $stagePath $fileName
        if (-not (Test-Path -LiteralPath $runtimePath -PathType Leaf)) {
            $fileName
        }
    }
)
if ($missing.Count -gt 0) {
    throw "Desktop stage is missing MSVC runtime files: $($missing -join ', ')"
}

Write-Host "Desktop MSVC runtime contract passed" -ForegroundColor Green
