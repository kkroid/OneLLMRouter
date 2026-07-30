param(
    [Parameter(Mandatory = $true)][string]$Tray,
    [int[]]$ProtectedPorts = @(3456, 3457)
)

$ErrorActionPreference = "Stop"
$trayPath = (Resolve-Path -LiteralPath $Tray -ErrorAction Stop).Path
if (-not (Test-Path -LiteralPath $trayPath -PathType Leaf)) { throw "Tray executable not found: $trayPath" }

$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
try {
    $listener.Start()
    $port = ([Net.IPEndPoint]$listener.LocalEndpoint).Port
}
finally {
    $listener.Stop()
}
if ($ProtectedPorts -contains $port) { throw "Dynamic test port $port is protected" }

$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("onellm-desktop-smoke-" + [guid]::NewGuid().ToString("N"))
$tempHome = Join-Path $tempRoot "home"
$tempLog = Join-Path $tempRoot "logs"
$configPath = Join-Path $tempRoot "onellm-router.yaml"
$resultPath = Join-Path $tempRoot "smoke-result.json"
$trayProcess = $null
$trayStarted = $false
$observedHealth = $null
$identityConfirmed = $false
$cleanupProcessesExited = $true

function ConvertTo-YamlSingleQuoted {
    param([Parameter(Mandatory = $true)][string]$Value)
    return "'" + $Value.Replace("'", "''") + "'"
}

function Get-DynamicHealth {
    param([Net.Http.HttpClient]$Client, [int]$DynamicPort)
    if ($ProtectedPorts -contains $DynamicPort) { throw "Refusing to query protected port $DynamicPort" }
    try {
        $payload = $Client.GetStringAsync("http://127.0.0.1:$DynamicPort/health").GetAwaiter().GetResult()
        return $payload | ConvertFrom-Json -ErrorAction Stop
    }
    catch {
        return $null
    }
}

function Test-DynamicPortOpen {
    param([int]$DynamicPort)
    if ($ProtectedPorts -contains $DynamicPort) { throw "Refusing to query protected port $DynamicPort" }
    $client = [Net.Sockets.TcpClient]::new()
    try {
        $connect = $client.ConnectAsync([Net.IPAddress]::Loopback, $DynamicPort)
        return $connect.Wait(500) -and $client.Connected
    }
    catch {
        return $false
    }
    finally {
        $client.Dispose()
    }
}

$httpHandler = [Net.Http.HttpClientHandler]::new()
$httpHandler.UseProxy = $false
$httpClient = [Net.Http.HttpClient]::new($httpHandler)
$httpClient.Timeout = [TimeSpan]::FromSeconds(1)

try {
    $catalogDirs = @(
        $tempHome,
        $tempLog,
        (Join-Path $tempHome ".onellm"),
        (Join-Path $tempHome ".codex")
    )
    New-Item -ItemType Directory -Path $catalogDirs -Force | Out-Null
    $quotedLog = ConvertTo-YamlSingleQuoted $tempLog
    $config = @"
server:
  host: "127.0.0.1"
  http_port: $port
log:
  level: "info"
  dir: $quotedLog
  max_age_days: 1
proxy:
  socks5: ""
codex:
  overwrite_catalog: false
providers:
  - name: "Desktop Smoke Test"
    prefix: "smoke"
    responses_base_url: "http://127.0.0.1:9"
    api_key: "test-only"
    proxy: false
    models: ["gpt-5.6-sol"]
"@
    Set-Content -LiteralPath $configPath -Value $config -Encoding utf8NoBOM

    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $trayPath
    $startInfo.WorkingDirectory = Split-Path -Parent $trayPath
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.ArgumentList.Add("--config")
    $startInfo.ArgumentList.Add($configPath)
    $startInfo.ArgumentList.Add("--smoke-test")
    $startInfo.ArgumentList.Add($resultPath)
    $startInfo.Environment["USERPROFILE"] = $tempHome
    $startInfo.Environment["HOME"] = $tempHome

    $trayProcess = [Diagnostics.Process]::new()
    $trayProcess.StartInfo = $startInfo
    if (-not $trayProcess.Start()) { throw "Failed to start tray smoke process" }
    $trayStarted = $true

    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    while ([DateTime]::UtcNow -lt $deadline -and -not $trayProcess.HasExited) {
        $health = Get-DynamicHealth -Client $httpClient -DynamicPort $port
        if ($null -ne $health) {
            if ($health.service -ne "onellm-router" -or [int]$health.http_port -ne $port -or [int]$health.pid -le 0) {
                throw "Dynamic port returned an unexpected service identity"
            }
            $observedHealth = $health
            $identityConfirmed = $true
            break
        }
        Start-Sleep -Milliseconds 200
    }
    if (-not $identityConfirmed) {
        throw "Tray did not expose a verified OneLLMRouter service on dynamic port $port"
    }

    $remaining = [Math]::Max(1, [int]($deadline - [DateTime]::UtcNow).TotalMilliseconds)
    if (-not $trayProcess.WaitForExit($remaining)) { throw "Tray smoke process did not finish within 45 seconds" }
    if ($trayProcess.ExitCode -ne 0) { throw "Tray smoke process exited with code $($trayProcess.ExitCode)" }
    if (-not (Test-Path -LiteralPath $resultPath -PathType Leaf)) { throw "Tray smoke result was not written" }

    $result = Get-Content -LiteralPath $resultPath -Raw | ConvertFrom-Json -ErrorAction Stop
    if ($result.service -ne "onellm-router" -or $result.ownership -ne "owned" -or $result.healthy -ne $true) {
        throw "Tray smoke result did not report an owned healthy OneLLMRouter"
    }
    if ([int]$result.pid -ne [int]$observedHealth.pid -or [int]$result.port -ne $port) {
        throw "Tray smoke result does not match the verified dynamic service"
    }

    $closeDeadline = [DateTime]::UtcNow.AddSeconds(10)
    while ([DateTime]::UtcNow -lt $closeDeadline -and (Test-DynamicPortOpen -DynamicPort $port)) {
        Start-Sleep -Milliseconds 200
    }
    if (Test-DynamicPortOpen -DynamicPort $port) {
        throw "Dynamic service port remained open after graceful tray shutdown"
    }
    Write-Host "Desktop smoke passed on isolated port $port" -ForegroundColor Green
}
finally {
    if ($trayStarted -and -not $trayProcess.HasExited) {
        if ($identityConfirmed) {
            $trayProcess.Kill()
            $null = $trayProcess.WaitForExit(5000)
        }
        else {
            $cleanupProcessesExited = $false
        }
    }

    if ($identityConfirmed -and $null -ne $observedHealth) {
        $currentHealth = Get-DynamicHealth -Client $httpClient -DynamicPort $port
        if ($null -ne $currentHealth -and $currentHealth.service -eq "onellm-router" -and
            [int]$currentHealth.http_port -eq $port -and [int]$currentHealth.pid -eq [int]$observedHealth.pid) {
            try {
                $coreProcess = [Diagnostics.Process]::GetProcessById([int]$observedHealth.pid)
                $coreProcess.Kill()
                $null = $coreProcess.WaitForExit(5000)
                $coreProcess.Dispose()
            }
            catch [ArgumentException] {
                # The verified core already exited.
            }
            catch [InvalidOperationException] {
                # The verified core exited between lookup and cleanup.
            }
        }
    }

    if ($null -ne $trayProcess) {
        if ($trayStarted -and -not $trayProcess.HasExited) { $cleanupProcessesExited = $false }
        $trayProcess.Dispose()
    }
    $httpClient.Dispose()
    $httpHandler.Dispose()

    if ($cleanupProcessesExited -and -not (Test-DynamicPortOpen -DynamicPort $port)) {
        if (Test-Path -LiteralPath $tempRoot) {
            Remove-Item -LiteralPath $tempRoot -Recurse -Force
        }
    }
    else {
        Write-Warning "Smoke temp directory retained because test processes are still active: $tempRoot"
    }
}
