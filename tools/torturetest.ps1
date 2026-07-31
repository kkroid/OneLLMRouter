param(
    [int]$RouterPort = 4867,
    [int]$MockPort = 4868,
    [int]$DurationSeconds = 300,
    [int]$TimeoutSeconds = 10,
    [string]$Binary = ".\dist\onellm-router-v1.3.2.exe",
    [int]$Clients = 1,
    [switch]$Worker,
    [string]$BaseUrl = "",
    [string]$WorkerCaseLog = "",
    [string]$ClientId = "client-1",
    [switch]$IncludeSilentCases,
    [int]$SilentTimeoutMs = 1000
)

$ErrorActionPreference = "Stop"

function Assert-FreePort([int]$Port) {
    $inUse = Test-NetConnection 127.0.0.1 -Port $Port -InformationLevel Quiet -WarningAction SilentlyContinue
    if ($inUse) {
        throw "port $Port is already in use"
    }
}

function Write-TempFile([string]$Path, [string]$Content) {
    Set-Content -LiteralPath $Path -Value $Content -Encoding UTF8
}

function Wait-HttpOk([string]$Url, [int]$Seconds) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $r = Invoke-WebRequest -UseBasicParsing $Url -TimeoutSec 1
            if ($r.StatusCode -eq 200) { return }
        } catch {
            Start-Sleep -Milliseconds 250
        }
    }
    throw "timeout waiting for $Url"
}

function Stop-Proc($Proc) {
    if ($null -eq $Proc) { return }
    if ($Proc.HasExited) { return }
    try {
        $Proc.CloseMainWindow() | Out-Null
        if (-not $Proc.WaitForExit(2000)) {
            $Proc.Kill()
            $Proc.WaitForExit(5000)
        }
    } catch {
        try { $Proc.Kill() } catch {}
    }
}

function Stop-ListenerOnPort([int]$Port) {
    $listeners = Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    foreach ($listener in $listeners) {
        try {
            Stop-Process -Id $listener.OwningProcess -Force -ErrorAction SilentlyContinue
        } catch {}
    }
}

function Invoke-JsonPost([string]$Url, [object]$Body) {
    $json = $Body | ConvertTo-Json -Depth 30 -Compress
    Invoke-WebRequest -UseBasicParsing -Method POST $Url -ContentType "application/json" -Body $json -TimeoutSec $TimeoutSeconds
}

function Invoke-JsonPostAllowError([string]$Url, [object]$Body) {
    $json = $Body | ConvertTo-Json -Depth 30 -Compress
    Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Method POST $Url -ContentType "application/json" -Body $json -TimeoutSec $TimeoutSeconds
}

function Invoke-CurlPost([string]$Url, [object]$Body) {
    $json = $Body | ConvertTo-Json -Depth 30 -Compress
    $out = & curl.exe -sS -N --max-time $TimeoutSeconds -X POST $Url -H "Content-Type: application/json" -d $json
    if ($LASTEXITCODE -ne 0) {
        throw "curl failed with exit code $LASTEXITCODE"
    }
    return ($out -join "`n")
}

function Read-CancelStream([string]$Url, [object]$Body, [int]$ReadBytes) {
    $client = [System.Net.Http.HttpClient]::new()
    $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
    $json = $Body | ConvertTo-Json -Depth 30 -Compress
    $content = [System.Net.Http.StringContent]::new($json, [System.Text.Encoding]::UTF8, "application/json")
    $req = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, $Url)
    $req.Content = $content
    $resp = $client.Send($req, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead)
    if (-not $resp.IsSuccessStatusCode) {
        throw "stream request failed: $($resp.StatusCode)"
    }
    $stream = $resp.Content.ReadAsStream()
    $buffer = New-Object byte[] $ReadBytes
    [void]$stream.Read($buffer, 0, $buffer.Length)
    $stream.Dispose()
    $resp.Dispose()
    $client.Dispose()
}

function Add-Result([hashtable]$Stats, [string]$Name, [scriptblock]$Block) {
    $Stats.total++
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $ok = $false
    $errMsg = ""
    try {
        & $Block
        $sw.Stop()
        $ok = $true
        $Stats.ok++
        $Stats.latencies.Add([int]$sw.ElapsedMilliseconds) | Out-Null
        if (-not $Stats.by_case.ContainsKey($Name)) { $Stats.by_case[$Name] = @{ ok = 0; fail = 0 } }
        $Stats.by_case[$Name].ok++
    } catch {
        $sw.Stop()
        $errMsg = $_.Exception.Message
        $Stats.fail++
        if (-not $Stats.by_case.ContainsKey($Name)) { $Stats.by_case[$Name] = @{ ok = 0; fail = 0 } }
        $Stats.by_case[$Name].fail++
        if ($Stats.first_error -eq "") {
            $Stats.first_error = "$Name failed: $($_.Exception.Message)"
        }
    } finally {
        if ($Stats.case_log -ne "") {
            $line = [ordered]@{
                time = (Get-Date).ToString("o")
                client = $Stats.client_id
                case = $Name
                ok = $ok
                elapsed_ms = [int]$sw.ElapsedMilliseconds
                error = $errMsg
            } | ConvertTo-Json -Compress
            Add-Content -LiteralPath $Stats.case_log -Value $line -Encoding UTF8
        }
    }
}

function Percentile([System.Collections.Generic.List[int]]$Values, [double]$P) {
    if ($Values.Count -eq 0) { return 0 }
    $arr = @($Values | Sort-Object)
    $idx = [Math]::Min($arr.Count - 1, [Math]::Max(0, [int][Math]::Ceiling($arr.Count * $P) - 1))
    return $arr[$idx]
}

function New-Stats([string]$CaseLogPath, [string]$StatsClientId) {
    return @{
        total = 0
        ok = 0
        fail = 0
        first_error = ""
        by_case = @{}
        latencies = [System.Collections.Generic.List[int]]::new()
        case_log = $CaseLogPath
        client_id = $StatsClientId
    }
}

function Invoke-TortureCases([hashtable]$Stats, [string]$Base, [datetime]$Deadline, [scriptblock]$RouterExited) {
    while ((Get-Date) -lt $Deadline) {
        Add-Result $Stats "anthropic_text" {
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "hi" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content[0].text -ne "ok") { throw "bad anthropic text" }
        }
        Add-Result $Stats "anthropic_stream_cancel" {
            Read-CancelStream "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "hi" }) } 64
        }
        Add-Result $Stats "openai_text" {
            $r = Invoke-JsonPost "$Base/openai/v1/chat/completions" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "hi" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.choices[0].message.content -ne "ok") { throw "bad openai text" }
        }
        Add-Result $Stats "openai_tool_stream_cancel_id" {
            Read-CancelStream "$Base/openai/v1/chat/completions" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "weather" }); tools = @(@{ type = "function"; function = @{ name = "get_weather"; parameters = @{ type = "object" } } }) } 160
        }
        Add-Result $Stats "openai_retry_after_cancel" {
            $r = Invoke-JsonPost "$Base/openai/v1/chat/completions" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "after cancel" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.object -ne "chat.completion") { throw "retry response shape invalid" }
        }
        Add-Result $Stats "openai_upstream_cut_then_retry" {
            try {
                Read-CancelStream "$Base/openai/v1/chat/completions" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "upstream_cut" }) } 64
            } catch {}
            $r = Invoke-JsonPost "$Base/openai/v1/chat/completions" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "retry" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.choices[0].message.content -ne "ok") { throw "retry after cut failed" }
        }
        Add-Result $Stats "anthropic_slow_cancel_then_retry" {
            try {
                Read-CancelStream "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "slow_first_byte" }) } 32
            } catch {}
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "retry" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content[0].text -ne "ok") { throw "anthropic retry after slow cancel failed" }
        }
        Add-Result $Stats "anthropic_thinking_block" {
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "thinking_block" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content[0].type -ne "thinking" -or $j.content[1].text -ne "done") { throw "thinking block response mismatch" }
        }
        Add-Result $Stats "anthropic_multi_block" {
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "multi_block" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content.Count -ne 2 -or $j.content[0].text -ne "part-a" -or $j.content[1].text -ne "part-b") { throw "multi block response mismatch" }
        }
        Add-Result $Stats "anthropic_text_tool_mix" {
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "text_tool_mix" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.stop_reason -ne "tool_use" -or $j.content[0].text -ne "checking" -or $j.content[1].name -ne "get_weather") { throw "text tool mix response mismatch" }
        }
        Add-Result $Stats "anthropic_max_tokens_stop" {
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "max_tokens_stop" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.stop_reason -ne "max_tokens") { throw "max_tokens stop_reason mismatch" }
        }
        Add-Result $Stats "anthropic_empty_content" {
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "empty_content" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content.Count -ne 0) { throw "empty content response mismatch" }
        }
        Add-Result $Stats "anthropic_error_statuses" {
            $r500 = Invoke-JsonPostAllowError "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "bad_status_500" }) }
            if ([int]$r500.StatusCode -ne 500) { throw "expected 500, got $($r500.StatusCode)" }
            $r429 = Invoke-JsonPostAllowError "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "bad_status_429" }) }
            if ([int]$r429.StatusCode -ne 429) { throw "expected 429, got $($r429.StatusCode)" }
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "retry after status" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content[0].text -ne "ok") { throw "retry after status failed" }
        }
        Add-Result $Stats "anthropic_malformed_json_then_retry" {
            $bad = Invoke-JsonPostAllowError "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "malformed_json" }) }
            if ([int]$bad.StatusCode -ne 200 -or $bad.Content -notmatch '"id":"broken"') { throw "malformed passthrough response mismatch: status=$($bad.StatusCode)" }
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "retry after malformed" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content[0].text -ne "ok") { throw "retry after malformed failed" }
        }
        Add-Result $Stats "anthropic_stream_ping" {
            $out = Invoke-CurlPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "stream_ping" }) }
            if ($out -notmatch "event: ping" -or $out -notmatch "event: message_stop") { throw "stream ping output mismatch" }
        }
        Add-Result $Stats "anthropic_stream_missing_stop_then_retry" {
            $out = Invoke-CurlPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "stream_missing_stop" }) }
            if ($out -notmatch "partial") { throw "missing-stop stream did not return partial output" }
            $r = Invoke-JsonPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "retry after missing stop" }) }
            $j = $r.Content | ConvertFrom-Json
            if ($j.content[0].text -ne "ok") { throw "retry after missing stop failed" }
        }

        if (& $RouterExited) { throw "router process exited during torture test" }
    }
}

function Invoke-SilentCases([hashtable]$Stats, [string]$Base) {
    Add-Result $Stats "anthropic_silent_nonstream_timeout" {
        $r = Invoke-JsonPostAllowError "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; messages = @(@{ role = "user"; content = "silent_nonstream" }) }
        if ([int]$r.StatusCode -ne 502 -or $r.Content -notmatch "timeout waiting for upstream response") {
            throw "expected non-stream timeout, got status=$($r.StatusCode) body=$($r.Content)"
        }
    }
    Add-Result $Stats "anthropic_stream_first_event_timeout" {
        $out = Invoke-CurlPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "stream_stall_before_event" }) }
        if ($out -ne "") {
            throw "expected empty stalled stream before first event, got $out"
        }
    }
    Add-Result $Stats "anthropic_stream_idle_timeout" {
        $out = Invoke-CurlPost "$Base/anthropic/v1/messages" @{ model = "mk/mock-model"; max_tokens = 16; stream = $true; messages = @(@{ role = "user"; content = "stream_stall_after_event" }) }
        if ($out -notmatch "event: message_start") {
            throw "expected partial stream before idle timeout, got $out"
        }
    }
}

function Merge-CaseLogs([string[]]$Paths) {
    $stats = New-Stats "" "all"
    foreach ($path in $Paths) {
        if (-not (Test-Path -LiteralPath $path)) {
            throw "case log not found: $path"
        }
        foreach ($line in (Get-Content -LiteralPath $path)) {
            if ($line.Trim() -eq "") { continue }
            $item = $line | ConvertFrom-Json
            $stats.total++
            if (-not $stats.by_case.ContainsKey($item.case)) {
                $stats.by_case[$item.case] = @{ ok = 0; fail = 0 }
            }
            if ($item.ok) {
                $stats.ok++
                $stats.by_case[$item.case].ok++
                $stats.latencies.Add([int]$item.elapsed_ms) | Out-Null
            } else {
                $stats.fail++
                $stats.by_case[$item.case].fail++
                if ($stats.first_error -eq "") {
                    $stats.first_error = "$($item.client) $($item.case) failed: $($item.error)"
                }
            }
        }
    }
    return $stats
}

function Write-Summary([string]$Title, [hashtable]$Stats, [string]$SummaryCaseLog) {
    $p50 = Percentile $Stats.latencies 0.50
    $p95 = Percentile $Stats.latencies 0.95
    $max = if ($Stats.latencies.Count -eq 0) { 0 } else { ($Stats.latencies | Measure-Object -Maximum).Maximum }

    Write-Host $Title
    Write-Host "router_port: $RouterPort"
    Write-Host "mock_port: $MockPort"
    Write-Host "log_dir: $tempDir"
    Write-Host "case_log: $SummaryCaseLog"
    Write-Host "router_stdout: $routerOutLog"
    Write-Host "router_stderr: $routerErrLog"
    Write-Host "mock_stdout: $mockOutLog"
    Write-Host "mock_stderr: $mockErrLog"
    Write-Host "duration_seconds: $DurationSeconds"
    Write-Host "clients: $Clients"
    Write-Host "requests: $($Stats.total)"
    Write-Host "ok: $($Stats.ok)"
    Write-Host "failures: $($Stats.fail)"
    Write-Host "p50_ms: $p50"
    Write-Host "p95_ms: $p95"
    Write-Host "max_ms: $max"
    foreach ($key in ($Stats.by_case.Keys | Sort-Object)) {
        Write-Host ("case {0}: ok={1} fail={2}" -f $key, $Stats.by_case[$key].ok, $Stats.by_case[$key].fail)
    }
    if ($Stats.fail -gt 0) {
        throw $Stats.first_error
    }
}

if ($Worker) {
    if ($BaseUrl -eq "") { throw "worker requires -BaseUrl" }
    if ($WorkerCaseLog -eq "") { throw "worker requires -WorkerCaseLog" }
    $stats = New-Stats $WorkerCaseLog $ClientId
    Invoke-TortureCases $stats $BaseUrl (Get-Date).AddSeconds($DurationSeconds) { $false }
    if ($stats.fail -gt 0) {
        throw $stats.first_error
    }
    exit 0
}

Assert-FreePort $RouterPort
Assert-FreePort $MockPort
if (-not (Test-Path -LiteralPath $Binary)) {
    throw "binary not found: $Binary"
}

$tempDir = Join-Path $env:TEMP ("onellm-torture-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tempDir | Out-Null
$mockFile = Join-Path $tempDir "mock.go"
$mockExe = Join-Path $tempDir "mock.exe"
$configFile = Join-Path $tempDir "onellm-router.yaml"
$mockOutLog = Join-Path $tempDir "mock.out.log"
$mockErrLog = Join-Path $tempDir "mock.err.log"
$routerOutLog = Join-Path $tempDir "router.out.log"
$routerErrLog = Join-Path $tempDir "router.err.log"
$caseLog = Join-Path $tempDir "cases.jsonl"

$mockSource = @"
package main

import (
  "fmt"
  "io"
  "net/http"
  "strings"
  "time"
)

func main() {
  mux := http.NewServeMux()
  mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
  mux.HandleFunc("/anthropic/messages", anthropic)
  mux.HandleFunc("/openai/v1/chat/completions", openai)
  http.ListenAndServe("127.0.0.1:$MockPort", mux)
}

func anthropic(w http.ResponseWriter, r *http.Request) {
  body, _ := io.ReadAll(r.Body)
  s := string(body)
  if strings.Contains(s, "silent_nonstream") {
    time.Sleep(10 * time.Second)
    return
  }
  if strings.Contains(s, "stream_stall_before_event") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    f.Flush()
    time.Sleep(10 * time.Second)
    return
  }
  if strings.Contains(s, "stream_stall_after_event") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"mock\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
    f.Flush()
    time.Sleep(10 * time.Second)
    return
  }
  if strings.Contains(s, "upstream_cut") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
    f.Flush()
    return
  }
  if strings.Contains(s, "slow_first_byte") {
    time.Sleep(1500 * time.Millisecond)
  }
  if strings.Contains(s, "bad_status_500") {
    http.Error(w, "{\"error\":{\"type\":\"server_error\",\"message\":\"mock 500\"}}", http.StatusInternalServerError)
    return
  }
  if strings.Contains(s, "bad_status_429") {
    http.Error(w, "{\"error\":{\"type\":\"rate_limit_error\",\"message\":\"mock 429\"}}", http.StatusTooManyRequests)
    return
  }
  if strings.Contains(s, "malformed_json") {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, "{\"id\":\"broken\",\"content\":[")
    return
  }
  if strings.Contains(s, "empty_content") {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, "{\"id\":\"msg_empty\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock\",\"content\":[],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}")
    return
  }
  if strings.Contains(s, "thinking_block") {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, "{\"id\":\"msg_thinking\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock\",\"content\":[{\"type\":\"thinking\",\"thinking\":\"considering\",\"signature\":\"sig\"},{\"type\":\"text\",\"text\":\"done\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}")
    return
  }
  if strings.Contains(s, "multi_block") {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, "{\"id\":\"msg_multi\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock\",\"content\":[{\"type\":\"text\",\"text\":\"part-a\"},{\"type\":\"text\",\"text\":\"part-b\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}")
    return
  }
  if strings.Contains(s, "text_tool_mix") {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, "{\"id\":\"msg_mix\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock\",\"content\":[{\"type\":\"text\",\"text\":\"checking\"},{\"type\":\"tool_use\",\"id\":\"toolu_mix\",\"name\":\"get_weather\",\"input\":{\"city\":\"Paris\"}}],\"stop_reason\":\"tool_use\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}")
    return
  }
  if strings.Contains(s, "max_tokens_stop") {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, "{\"id\":\"msg_length\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock\",\"content\":[{\"type\":\"text\",\"text\":\"truncated\"}],\"stop_reason\":\"max_tokens\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}")
    return
  }
  if strings.Contains(s, "stream_missing_stop") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"mock\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
    fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
    fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n")
    f.Flush()
    return
  }
  if strings.Contains(s, "stream_ping") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"mock\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
    fmt.Fprintf(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
    fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
    fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\n")
    fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
    fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
    f.Flush()
    return
  }
  if strings.Contains(s, "\"stream\":true") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"mock\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
    f.Flush()
    time.Sleep(20 * time.Millisecond)
    fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
    fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
    fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
    fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
    fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
    f.Flush()
    return
  }
  w.Header().Set("Content-Type", "application/json")
  if strings.Contains(s, "tools") {
    fmt.Fprintf(w, "{\"id\":\"msg_tool\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"get_weather\",\"input\":{\"city\":\"Paris\"}}],\"stop_reason\":\"tool_use\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}")
    return
  }
  fmt.Fprintf(w, "{\"id\":\"msg_text\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"mock\",\"content\":[{\"type\":\"text\",\"text\":\"ok\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}")
}

func openai(w http.ResponseWriter, r *http.Request) {
  body, _ := io.ReadAll(r.Body)
  s := string(body)
  if strings.Contains(s, "upstream_cut") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
    f.Flush()
    return
  }
  if strings.Contains(s, "\"stream\":true") {
    w.Header().Set("Content-Type", "text/event-stream")
    f, _ := w.(http.Flusher)
    fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
    f.Flush()
    if strings.Contains(s, "tools") {
      fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\"}]}}]}\n\n")
      f.Flush()
      time.Sleep(20 * time.Millisecond)
      fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n")
      f.Flush()
      time.Sleep(20 * time.Millisecond)
      fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"Paris\\\"}\"}}]}}]}\n\n")
      fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
    } else {
      fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
      fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
    }
    fmt.Fprintf(w, "data: [DONE]\n\n")
    f.Flush()
    return
  }
  w.Header().Set("Content-Type", "application/json")
  if strings.Contains(s, "tools") {
    fmt.Fprintf(w, "{\"id\":\"chat_tool\",\"object\":\"chat.completion\",\"model\":\"mock\",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Paris\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}")
    return
  }
  fmt.Fprintf(w, "{\"id\":\"chat_text\",\"object\":\"chat.completion\",\"model\":\"mock\",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}")
}
"@

$config = @"
server:
  host: "127.0.0.1"
  http_port: $RouterPort
  bell: false
log:
  level: "info"
  dir: "$($tempDir.Replace('\','/'))/logs"
  max_age_days: 1
proxy:
  socks5: ""
providers:
  - name: "Mock"
    prefix: "mk"
    base_url: "http://127.0.0.1:$MockPort/anthropic"
    openai_base_url: "http://127.0.0.1:$MockPort/openai"
    api_key: "sk-mock"
    proxy: false
    models: ["mock-model"]
model_slots:
  default: "mk/mock-model"
  opus: "mk/mock-model"
  sonnet: "mk/mock-model"
  haiku: "mk/mock-model"
  fable: "mk/mock-model"
"@

Write-TempFile $mockFile $mockSource
Write-TempFile $configFile $config

if ($Clients -le 1) {
    Write-Host "single-client torture test starting"
} else {
    Write-Host "multi-client torture test starting"
}
Write-Host "router_port: $RouterPort"
Write-Host "mock_port: $MockPort"
Write-Host "clients: $Clients"
Write-Host "log_dir: $tempDir"
Write-Host "case_log: $caseLog"
Write-Host "router_stdout: $routerOutLog"
Write-Host "router_stderr: $routerErrLog"
Write-Host "mock_stdout: $mockOutLog"
Write-Host "mock_stderr: $mockErrLog"

$mockProc = $null
$routerProc = $null
$timeoutEnvNames = @(
    "ONELLM_EXTERNAL_REQUEST_TIMEOUT_MS",
    "ONELLM_EXTERNAL_STREAM_TIMEOUT_MS",
    "ONELLM_OPENAI_REQUEST_TIMEOUT_MS",
    "ONELLM_STREAM_FIRST_EVENT_TIMEOUT_MS",
    "ONELLM_STREAM_IDLE_TIMEOUT_MS"
)
$oldTimeoutEnv = @{}
foreach ($name in $timeoutEnvNames) {
    $oldTimeoutEnv[$name] = [Environment]::GetEnvironmentVariable($name, "Process")
}

try {
    if ($IncludeSilentCases) {
        $env:ONELLM_EXTERNAL_REQUEST_TIMEOUT_MS = [string]$SilentTimeoutMs
        $env:ONELLM_EXTERNAL_STREAM_TIMEOUT_MS = [string]([Math]::Max($SilentTimeoutMs * 5, 5000))
        $env:ONELLM_OPENAI_REQUEST_TIMEOUT_MS = [string]$SilentTimeoutMs
        $env:ONELLM_STREAM_FIRST_EVENT_TIMEOUT_MS = [string]$SilentTimeoutMs
        $env:ONELLM_STREAM_IDLE_TIMEOUT_MS = [string]$SilentTimeoutMs
    }

    & go build -o $mockExe $mockFile 1>> $mockOutLog 2>> $mockErrLog
    if ($LASTEXITCODE -ne 0) {
        throw "mock build failed; see $mockErrLog"
    }
    $mockProc = Start-Process -FilePath $mockExe -RedirectStandardOutput $mockOutLog -RedirectStandardError $mockErrLog -PassThru -WindowStyle Hidden
    Wait-HttpOk "http://127.0.0.1:$MockPort/health" 20

    $routerProc = Start-Process -FilePath $Binary -ArgumentList @("--no-pid", "--config", $configFile) -RedirectStandardOutput $routerOutLog -RedirectStandardError $routerErrLog -PassThru -WindowStyle Hidden
    Wait-HttpOk "http://127.0.0.1:$RouterPort/health" 20

    $base = "http://127.0.0.1:$RouterPort"
    $deadline = (Get-Date).AddSeconds($DurationSeconds)

    if ($Clients -le 1) {
        $stats = New-Stats $caseLog "client-1"
        if ($IncludeSilentCases) {
            Invoke-SilentCases $stats $base
        }
        Invoke-TortureCases $stats $base $deadline { $routerProc.HasExited }
        Write-Summary "single-client torture test" $stats $caseLog
    } else {
        $silentLog = ""
        if ($IncludeSilentCases) {
            $silentLog = Join-Path $tempDir "cases-silent.jsonl"
            $silentStats = New-Stats $silentLog "silent"
            Invoke-SilentCases $silentStats $base
        }
        $clientLogs = @()
        $jobs = @()
        for ($i = 1; $i -le $Clients; $i++) {
            $clientLog = Join-Path $tempDir ("cases-client-{0}.jsonl" -f $i)
            $clientLogs += $clientLog
            $jobs += Start-Job -Name ("torture-client-{0}" -f $i) -ArgumentList $PSCommandPath, $base, $clientLog, $DurationSeconds, $TimeoutSeconds, $i -ScriptBlock {
                param($scriptPath, $workerBase, $workerLog, $duration, $timeout, $workerIndex)
                & pwsh -NoProfile -File $scriptPath -Worker -BaseUrl $workerBase -WorkerCaseLog $workerLog -DurationSeconds $duration -TimeoutSeconds $timeout -ClientId ("client-{0}" -f $workerIndex)
                if ($LASTEXITCODE -ne 0) {
                    throw "client-$workerIndex exited with code $LASTEXITCODE"
                }
            }
        }

        while (($jobs | Where-Object { $_.State -eq "Running" }).Count -gt 0) {
            if ($routerProc.HasExited) {
                throw "router process exited during torture test"
            }
            Wait-Job -Job $jobs -Any -Timeout 1 | Out-Null
        }

        $jobErrors = @()
        foreach ($job in $jobs) {
            try {
                Receive-Job -Job $job -ErrorAction Stop
            } catch {
                $jobErrors += "$($job.Name): $($_.Exception.Message)"
            }
            if ($job.State -ne "Completed") {
                $jobErrors += "$($job.Name) ended with state $($job.State)"
            }
        }
        Remove-Job -Job $jobs

        $logsToMerge = $clientLogs
        if ($silentLog -ne "") {
            $logsToMerge = @($silentLog) + $clientLogs
        }
        Get-Content -LiteralPath $logsToMerge | Set-Content -LiteralPath $caseLog -Encoding UTF8
        $stats = Merge-CaseLogs $logsToMerge
        Write-Summary "multi-client torture test" $stats $caseLog
        if ($jobErrors.Count -gt 0) {
            throw ($jobErrors -join "; ")
        }
    }
} finally {
    Stop-Proc $routerProc
    Stop-Proc $mockProc
    Stop-ListenerOnPort $RouterPort
    Stop-ListenerOnPort $MockPort
    foreach ($name in $timeoutEnvNames) {
        [Environment]::SetEnvironmentVariable($name, $oldTimeoutEnv[$name], "Process")
    }
}
