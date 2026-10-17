<#
.SYNOPSIS
  Snapshot how much memory MCP servers spawned by AI agents use on this machine.

.DESCRIPTION
  Walks the process tree under every agent process (Claude Code, Codex, Grok,
  Gemini, Cursor, VS Code, mcpcell) and groups the descendants by MCP server.
  Reports private commit (PrivatePageCount), not just working set: Windows trims
  the working set of idle processes, so RSS/WS badly undercounts idle MCPs while
  their commit still counts against the system commit limit.

  Read-only. Command lines are reduced to a package/script key and never printed
  in full, because they can contain tokens.

.EXAMPLE
  pwsh scripts/measure-mcp.ps1
  pwsh scripts/measure-mcp.ps1 -Json bench/baseline.json -Label "before mcpcell"
#>
param(
  [string]$Json,
  [string]$Label = ''
)

$ErrorActionPreference = 'Stop'

$agentPattern = '^(claude|codex|grok|gemini|cursor|code|code - insiders|windsurf|kiro|opencode|mcpcell|mcpcelld)\.exe$'
# Descendants that are agent tooling rather than MCP servers.
$notServer = '^(conhost|bash|sh|pwsh|powershell|git|git-remote-https|rg|ssh|ssh-agent|less|cat|find|timeout|WerFault|OpenConsole)\.exe$'

$all = Get-CimInstance Win32_Process
$byPid = @{}
$children = @{}
foreach ($p in $all) {
  $byPid[[int]$p.ProcessId] = $p
  $pp = [int]$p.ParentProcessId
  if (-not $children.ContainsKey($pp)) { $children[$pp] = New-Object System.Collections.ArrayList }
  [void]$children[$pp].Add($p)
}

function Get-Descendants([int]$id, [int]$depth = 0) {
  $out = @()
  if ($depth -gt 16 -or -not $children.ContainsKey($id)) { return $out }
  $born = $byPid[$id].CreationDate
  foreach ($c in $children[$id]) {
    $cid = [int]$c.ProcessId
    if ($cid -eq $id) { continue }
    # A child older than its "parent" belongs to an earlier process that had
    # this PID; Windows does not clear ParentProcessId when a parent exits.
    if ($born -and $c.CreationDate -lt $born) { continue }
    # Nested agents (e.g. a Claude subagent process) are reported as their own root.
    if ($c.Name -match $agentPattern) { continue }
    # Agents run tool calls through shells; MCP servers are spawned directly or
    # via a `cmd /c npx` launcher. Skip shell subtrees so dev servers and builds
    # an agent started are not counted as MCP.
    if ($c.Name -match $notServer) { continue }
    if ($c.Name -eq 'cmd.exe' -and $c.CommandLine -notmatch '\b(npx|uvx)\b|[-_]mcp|mcp[-_]') { continue }
    $out += $c
    $out += Get-Descendants $cid ($depth + 1)
  }
  return $out
}

function Get-ServerKey($proc) {
  $cl = [string]$proc.CommandLine
  $m = [regex]::Match($cl, '(@[\w.-]+/[\w.-]+|[\w.-]*mcp[\w.-]*)', 'IgnoreCase')
  if ($m.Success) { return ($m.Value -replace '@(latest|next|[\d^~][\w.^~-]*)$', '') }
  $py = [regex]::Match($cl, '([\w.-]+)\.py\b')
  if ($py.Success) { return $py.Groups[1].Value }
  return $proc.Name -replace '\.exe$', ''
}

function MB($bytes) { [math]::Round([double]$bytes / 1MB, 1) }
function SumPriv($procs) { ($procs | ForEach-Object { [double]$_.PrivatePageCount } | Measure-Object -Sum).Sum }
function SumWS($procs) { ($procs | ForEach-Object { [double]$_.WorkingSetSize } | Measure-Object -Sum).Sum }

$sessions = @()
$serverProcs = @()
foreach ($root in ($all | Where-Object { $_.Name -match $agentPattern })) {
  $desc = @(Get-Descendants ([int]$root.ProcessId))
  $serverProcs += $desc | ForEach-Object { [pscustomobject]@{ Proc = $_; Session = [int]$root.ProcessId } }
  $sessions += [pscustomobject]@{
    Session     = "$($root.Name -replace '\.exe$','')#$($root.ProcessId)"
    AgentPrivMB = MB $root.PrivatePageCount
    McpProcs    = $desc.Count
    McpPrivMB   = MB (SumPriv $desc)
    McpWSMB     = MB (SumWS $desc)
  }
}

$servers = $serverProcs | Group-Object { Get-ServerKey $_.Proc } | ForEach-Object {
  $procs = $_.Group.Proc
  [pscustomobject]@{
    Server   = $_.Name
    Sessions = @($_.Group.Session | Sort-Object -Unique).Count
    Procs    = $procs.Count
    Shims    = @($procs | Where-Object Name -eq 'cmd.exe').Count
    PrivMB   = MB (SumPriv $procs)
    WSMB     = MB (SumWS $procs)
  }
} | Sort-Object PrivMB -Descending

$os = Get-CimInstance Win32_OperatingSystem
$allServerProcs = @($serverProcs.Proc)
$summary = [pscustomobject]@{
  Time             = (Get-Date).ToString('o')
  Label            = $Label
  AgentSessions    = $sessions.Count
  McpProcs         = $allServerProcs.Count
  McpShims         = @($allServerProcs | Where-Object Name -eq 'cmd.exe').Count
  McpPrivMB        = MB (SumPriv $allServerProcs)
  McpWSMB          = MB (SumWS $allServerProcs)
  SystemCommitGB   = [math]::Round(($os.TotalVirtualMemorySize - $os.FreeVirtualMemory) / 1MB, 1)
  SystemCommitMaxGB = [math]::Round($os.TotalVirtualMemorySize / 1MB, 1)
}

'== Agent sessions =='
$sessions | Sort-Object McpPrivMB -Descending | Format-Table -AutoSize | Out-String -Width 200
'== MCP servers (across all sessions) =='
$servers | Format-Table -AutoSize | Out-String -Width 200
'== Summary =='
$summary | Format-List | Out-String -Width 200

if ($Json) {
  $dir = Split-Path -Parent $Json
  if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Force $dir | Out-Null }
  [pscustomobject]@{ Summary = $summary; Sessions = $sessions; Servers = $servers } |
    ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8 $Json
  "written: $Json"
}
