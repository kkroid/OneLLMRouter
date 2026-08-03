param(
    [Parameter(Mandatory = $true)][string]$Setup,
    [int[]]$ProtectedPorts = @(3456, 3457)
)

$ErrorActionPreference = "Stop"
if ($env:GITHUB_ACTIONS -ne "true" -or
    $env:RUNNER_ENVIRONMENT -ne "github-hosted") {
    throw "This test may run only on a disposable GitHub-hosted runner"
}

$setupPath = (Resolve-Path -LiteralPath $Setup -ErrorAction Stop).Path
$installDir = Join-Path $env:LOCALAPPDATA "Programs\OneLLMRouter"
$trayPath = Join-Path $installDir "onellm-router-tray.exe"
$configDir = Join-Path $env:USERPROFILE ".onellm"
$configPath = Join-Path $configDir "onellm-router.yaml"
$logDir = Join-Path $env:RUNNER_TEMP "onellm-running-upgrade-logs"
$runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$legacyValueName = "OneLLMRouter"
$desktopValueName = "OneLLMRouter Desktop"
$uninstaller = Join-Path $installDir "unins000.exe"
$originalTray = $null
$httpHandler = $null
$httpClient = $null
$uninstalled = $false

if (Test-Path -LiteralPath $installDir) {
    throw "Refusing to test over an existing installation: $installDir"
}
if (Test-Path -LiteralPath $configPath) {
    throw "Refusing to test over an existing user configuration: $configPath"
}

$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
try {
    $listener.Start()
    $port = ([Net.IPEndPoint]$listener.LocalEndpoint).Port
}
finally {
    $listener.Stop()
}
if ($ProtectedPorts -contains $port) {
    throw "Dynamic test port $port is protected"
}

function Invoke-SilentInstallerProcess {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Operation,
        [string[]]$AdditionalArguments = @()
    )
    $arguments = @("/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART") +
        $AdditionalArguments
    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $Path
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    foreach ($argument in $arguments) {
        $startInfo.ArgumentList.Add($argument)
    }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    try {
        Write-Host "$Operation process: $Path"
        if (-not $process.Start()) {
            throw "Failed to start $Operation process"
        }
        $process.WaitForExit()
        if ($process.ExitCode -ne 0) {
            throw "$Operation failed with exit code $($process.ExitCode)"
        }
    }
    finally {
        $process.Dispose()
    }
}

function Invoke-Setup {
    param([Parameter(Mandatory = $true)][string]$Path)
    Invoke-SilentInstallerProcess -Path $Path -Operation "Setup" `
        -AdditionalArguments @('/TASKS="autostart"')
}

function Get-OptionalRegistryValue {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Name
    )
    $values = Get-ItemProperty -Path $Path -ErrorAction SilentlyContinue
    if ($null -eq $values) {
        return $null
    }
    $property = $values.PSObject.Properties[$Name]
    if ($null -eq $property) {
        return $null
    }
    return $property.Value
}

function Get-DynamicHealth {
    param([Parameter(Mandatory = $true)][int]$DynamicPort)
    if ($ProtectedPorts -contains $DynamicPort) {
        throw "Refusing to query protected port $DynamicPort"
    }
    try {
        $payload = $httpClient.GetStringAsync(
            "http://127.0.0.1:$DynamicPort/health").GetAwaiter().GetResult()
        return $payload | ConvertFrom-Json -ErrorAction Stop
    }
    catch {
        return $null
    }
}

function Wait-DynamicHealth {
    param(
        [Parameter(Mandatory = $true)][int]$DynamicPort,
        [Parameter(Mandatory = $true)][string]$ExpectedConfig,
        [int]$DifferentFromPID = 0
    )
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    while ([DateTime]::UtcNow -lt $deadline) {
        $health = Get-DynamicHealth -DynamicPort $DynamicPort
        if ($null -ne $health -and
            $health.service -eq "onellm-router" -and
            [int]$health.http_port -eq $DynamicPort -and
            [int]$health.pid -gt 0 -and
            [IO.Path]::GetFullPath([string]$health.config_path).Equals(
                [IO.Path]::GetFullPath($ExpectedConfig),
                [StringComparison]::OrdinalIgnoreCase) -and
            ($DifferentFromPID -le 0 -or
             [int]$health.pid -ne $DifferentFromPID)) {
            return $health
        }
        Start-Sleep -Milliseconds 200
    }
    throw "Verified OneLLMRouter health did not appear on port $DynamicPort"
}

function Test-DynamicPortOpen {
    param([Parameter(Mandatory = $true)][int]$DynamicPort)
    if ($ProtectedPorts -contains $DynamicPort) {
        throw "Refusing to query protected port $DynamicPort"
    }
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

function Wait-DynamicPortClosed {
    param([Parameter(Mandatory = $true)][int]$DynamicPort)
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        if (-not (Test-DynamicPortOpen -DynamicPort $DynamicPort)) {
            return
        }
        Start-Sleep -Milliseconds 200
    }
    throw "Dynamic service port $DynamicPort remained open"
}

function Assert-ConfigUnchanged {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$ExpectedHash,
        [Parameter(Mandatory = $true)][string]$Phase
    )
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf) -or
        (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash -ne
            $ExpectedHash) {
        throw "$Phase changed the user configuration"
    }
}

$httpHandler = [Net.Http.HttpClientHandler]::new()
$httpHandler.UseProxy = $false
$httpClient = [Net.Http.HttpClient]::new($httpHandler)
$httpClient.Timeout = [TimeSpan]::FromSeconds(1)

try {
    New-Item -Path $runKey -Force | Out-Null
    New-ItemProperty -Path $runKey -Name $legacyValueName `
        -Value "OneLLMRouter upgrade test marker" -PropertyType String `
        -Force | Out-Null

    Invoke-Setup -Path $setupPath
    if (-not (Test-Path -LiteralPath $trayPath -PathType Leaf)) {
        throw "Installed tray executable is missing"
    }
    if ($null -ne (Get-OptionalRegistryValue -Path $runKey `
            -Name $legacyValueName)) {
        throw "Initial install left the legacy autostart value behind"
    }
    $expectedRunValue = """$trayPath"" --config ""$configPath"""
    if ((Get-OptionalRegistryValue -Path $runKey -Name $desktopValueName) -ne
        $expectedRunValue) {
        throw "Initial install did not create the expected desktop autostart value"
    }

    New-Item -ItemType Directory -Path $configDir, $logDir -Force | Out-Null
    $marker = "# OneLLMRouter running upgrade marker $([guid]::NewGuid())"
    $quotedLog = "'" + $logDir.Replace("'", "''") + "'"
    $config = @"
$marker
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
  - name: "Installer Upgrade Test"
    prefix: "upgrade"
    responses_base_url: "http://127.0.0.1:9"
    api_key: "test-only"
    proxy: false
    models: ["gpt-5.6-sol"]
"@
    Set-Content -LiteralPath $configPath -Value $config -Encoding utf8NoBOM
    $configHash = (Get-FileHash -LiteralPath $configPath -Algorithm SHA256).Hash

    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $trayPath
    $startInfo.WorkingDirectory = $installDir
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.ArgumentList.Add("--config")
    $startInfo.ArgumentList.Add($configPath)
    $originalTray = [Diagnostics.Process]::new()
    $originalTray.StartInfo = $startInfo
    if (-not $originalTray.Start()) {
        throw "Failed to start the installed tray"
    }

    $before = Wait-DynamicHealth -DynamicPort $port `
        -ExpectedConfig $configPath
    $beforeCorePID = [int]$before.pid

    Invoke-Setup -Path $setupPath
    if (-not $originalTray.WaitForExit(30000)) {
        throw "Setup did not close the original tray process"
    }
    if ($originalTray.ExitCode -ne 0) {
        throw "Original tray exited abnormally with code $($originalTray.ExitCode)"
    }
    Assert-ConfigUnchanged -Path $configPath -ExpectedHash $configHash `
        -Phase "Running upgrade"
    if ((Get-OptionalRegistryValue -Path $runKey -Name $desktopValueName) -ne
        $expectedRunValue) {
        throw "Running upgrade changed the desktop autostart value"
    }

    $after = Wait-DynamicHealth -DynamicPort $port `
        -ExpectedConfig $configPath -DifferentFromPID $beforeCorePID
    if ([int]$after.pid -eq $beforeCorePID) {
        throw "Running upgrade did not replace the owned core process"
    }
    Start-Sleep -Seconds 2
    $stable = Get-DynamicHealth -DynamicPort $port
    if ($null -eq $stable -or [int]$stable.pid -ne [int]$after.pid) {
        throw "Replacement core did not remain stable after the upgrade"
    }

    Invoke-SilentInstallerProcess -Path $uninstaller -Operation "Uninstall"
    $uninstalled = $true
    Wait-DynamicPortClosed -DynamicPort $port
    Assert-ConfigUnchanged -Path $configPath -ExpectedHash $configHash `
        -Phase "Uninstall"
    if ($null -ne (Get-OptionalRegistryValue -Path $runKey `
            -Name $legacyValueName) -or
        $null -ne (Get-OptionalRegistryValue -Path $runKey `
            -Name $desktopValueName)) {
        throw "Uninstall left an autostart value behind"
    }

    Write-Host "Installer running upgrade passed on isolated port $port" `
        -ForegroundColor Green
}
finally {
    if (-not $uninstalled -and
        (Test-Path -LiteralPath $uninstaller -PathType Leaf)) {
        try {
            Invoke-SilentInstallerProcess -Path $uninstaller `
                -Operation "Cleanup uninstall"
        }
        catch {
            Write-Warning $_
        }
    }
    if ($null -ne $originalTray) {
        $originalTray.Dispose()
    }
    if ($null -ne $httpClient) {
        $httpClient.Dispose()
    }
    if ($null -ne $httpHandler) {
        $httpHandler.Dispose()
    }
}
