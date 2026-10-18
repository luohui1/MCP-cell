<#
.SYNOPSIS
  Start N fresh `claude -p` sessions and measure the MCP processes they cause.

.DESCRIPTION
  Each session is started in stream-json input mode with stdin held open, so
  it connects its MCP servers and then idles without any model call. Reports, per session, the processes Claude
  Code itself spawned for MCP (shell subtrees excluded) and, separately, the
  MCPcell daemon's tree. All memory is private commit. The test sessions'
  trees are killed afterwards.

.EXAMPLE
  pwsh scripts/bench-sessions.ps1 -N 3 -Label before -Json bench/ab-before.json
#>
param(
  [int]$N = 3,
  [int]$WaitSec = 45,
  [string]$Label = '',
  [string]$Json,
  # Load only these servers (--mcp-config ... --strict-mcp-config).
  [string]$McpConfig,
  # Send one tiny haiku message at start so Claude Code reports when its MCP
  # servers were ready and which connected (the init event).
  [switch]$Ready
)
$ErrorActionPreference = 'Stop'

$claude = (Get-Command claude).Source
$work = Join-Path ([IO.Path]::GetTempPath()) 'mcpcell-bench'
New-Item -ItemType Directory -Force $work | Out-Null

$clock = [Diagnostics.Stopwatch]::StartNew()
$events = @{}
$started = foreach ($i in 1..$N) {
  $psi = [Diagnostics.ProcessStartInfo]::new($claude)
  # stream-json input with stdin held open: Claude Code connects its MCP
  # servers at startup, then waits for a message that never comes. No model
  # call is made, so nothing depends on how a model answers.
  foreach ($a in @('-p', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose', '--model', 'haiku')) { $psi.ArgumentList.Add($a) }
  if ($McpConfig) { foreach ($a in @('--mcp-config', $McpConfig, '--strict-mcp-config')) { $psi.ArgumentList.Add($a) } }
  $psi.RedirectStandardInput = $true
  $psi.WorkingDirectory = $work
  $psi.UseShellExecute = $false
  $psi.CreateNoWindow = $true
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  $p = [Diagnostics.Process]::Start($psi)
  $state = [hashtable]::Synchronized(@{ start = $clock.ElapsedMilliseconds; ready = $null; servers = '' })
  $events[$p.Id] = $state
  $null = Register-ObjectEvent -InputObject $p -EventName OutputDataReceived -MessageData @{ s = $state; c = $clock } -Action {
    $line = $EventArgs.Data
    if (-not $line -or $Event.MessageData.s.ready) { return }
    try { $ev = $line | ConvertFrom-Json } catch { return }
    if ($ev.type -eq 'system' -and $ev.subtype -eq 'init') {
      $Event.MessageData.s.ready = $Event.MessageData.c.ElapsedMilliseconds
      $Event.MessageData.s.servers = (($ev.mcp_servers | ForEach-Object { "$($_.name)=$($_.status)" }) -join ' ')
    }
  }
  $p.BeginOutputReadLine()
  if ($Ready) {
    $p.StandardInput.WriteLine('{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Reply with OK only."}]}}')
    $p.StandardInput.Flush()
  }
  $p
}
Write-Host "started $N sessions: $(($started | ForEach-Object Id) -join ', '); waiting $WaitSec s for MCP servers"
# Short sleeps: PowerShell runs event actions only between statements, so a
# single long sleep would delay (and misdate) the init events.
while ($clock.ElapsedMilliseconds -lt $WaitSec * 1000) { Start-Sleep -Milliseconds 100 }

$all = Get-CimInstance Win32_Process
$byPid = @{}
foreach ($p in $all) { $byPid[[int]$p.ProcessId] = $p }
$children = @{}
foreach ($p in $all) {
  $pp = [int]$p.ParentProcessId
  if (-not $children.ContainsKey($pp)) { $children[$pp] = [Collections.ArrayList]::new() }
  [void]$children[$pp].Add($p)
}
$shells = '^(conhost|bash|sh|pwsh|powershell|git|sleep|rg)\.exe$'
function Tree([int]$id, [bool]$mcpOnly) {
  $out = @()
  if (-not $children.ContainsKey($id)) { return $out }
  $born = $byPid[$id].CreationDate
  foreach ($c in $children[$id]) {
    if ([int]$c.ProcessId -eq $id) { continue }
    # Windows keeps a dead parent's PID in ParentProcessId; a process older
    # than its "parent" belongs to an earlier process that had this PID.
    if ($born -and $c.CreationDate -lt $born) { continue }
    if ($mcpOnly -and $c.Name -match $shells) { continue }
    if ($mcpOnly -and $c.Name -eq 'cmd.exe' -and $c.CommandLine -notmatch '\b(npx|uvx)\b|[-_]mcp|mcp[-_]') { continue }
    $out += $c
    $out += Tree ([int]$c.ProcessId) $mcpOnly
  }
  return $out
}
function MB($procs) { [math]::Round((($procs | ForEach-Object { [double]$_.PrivatePageCount } | Measure-Object -Sum).Sum) / 1MB, 1) }

$sessions = foreach ($p in $started) {
  $tree = @(Tree $p.Id $true)
  $st = $events[$p.Id]
  [pscustomobject]@{
    Session  = "claude#$($p.Id)"
    Alive    = -not $p.HasExited
    ReadySec = if ($st.ready) { [math]::Round(($st.ready - $st.start) / 1000, 1) } else { $null }
    Connected = $st.servers
    McpProcs = $tree.Count
    McpPrivMB = MB $tree
    Servers  = (($tree | Where-Object Name -ne 'cmd.exe' | ForEach-Object {
          ([regex]::Match([string]$_.CommandLine, '(@[\w.-]+/[\w.-]+|[\w.-]*mcp[\w.-]*|[\w-]+\.py)', 'IgnoreCase')).Value
        } | Where-Object { $_ } | Sort-Object -Unique) -join ', ')
  }
}
$daemon = $all | Where-Object { $_.Name -match '^mcpcelld?\.exe$' } | Select-Object -First 1
$daemonTree = @()
if ($daemon) { $daemonTree = @($daemon) + @(Tree ([int]$daemon.ProcessId) $false) }

$alive = @($sessions | Where-Object Alive)
foreach ($p in $started | Where-Object HasExited) {
  Write-Host "session $($p.Id) exited early (code $($p.ExitCode)): $($p.StandardOutput.ReadToEnd().Trim()) $($p.StandardError.ReadToEnd().Trim())"
}
$sessionMB = ($alive | Measure-Object McpPrivMB -Sum).Sum
$summary = [pscustomobject]@{
  Label           = $Label
  Time            = (Get-Date).ToString('o')
  Sessions        = $N
  AliveSessions   = $alive.Count
  SessionMcpProcs = ($alive | Measure-Object McpProcs -Sum).Sum
  SessionMcpMB    = $sessionMB
  PerSessionMB    = if ($alive.Count) { [math]::Round($sessionMB / $alive.Count, 1) } else { 0 }
  MaxReadySec     = ($sessions | Measure-Object ReadySec -Maximum).Maximum
  AvgReadySec     = [math]::Round(($sessions | Measure-Object ReadySec -Average).Average, 1)
  FailedServers   = @($sessions | ForEach-Object { ($_.Connected -split ' ') } | Where-Object { $_ -and $_ -notmatch '=connected$' }).Count
  McpcellProcs    = $daemonTree.Count
  McpcellMB       = MB $daemonTree
  TotalMB         = [math]::Round($sessionMB + (MB $daemonTree), 1)
}
$sessions | Format-Table -AutoSize | Out-String -Width 220
$summary | Format-List | Out-String -Width 200

# Kill the test sessions with everything they spawned.
foreach ($p in $started) {
  foreach ($c in @(Tree $p.Id $false)) { Stop-Process -Id $c.ProcessId -Force -ErrorAction SilentlyContinue }
  Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
}

if ($Json) {
  $dir = Split-Path -Parent $Json
  if ($dir) { New-Item -ItemType Directory -Force $dir | Out-Null }
  [pscustomobject]@{ Summary = $summary; Sessions = $sessions } | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8 $Json
}
