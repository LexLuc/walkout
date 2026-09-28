$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = Join-Path $repoRoot '.tmp\codex-emergency-probe'
$binDir = Join-Path $probeRoot 'bin'
$recordDir = Join-Path $probeRoot 'records'
[IO.Directory]::CreateDirectory($recordDir) | Out-Null
$env:WALKOUT_EVENT_PROBE_OUTPUT = Join-Path $recordDir 'health-events.jsonl'
$env:PATH = $binDir + [IO.Path]::PathSeparator + $env:PATH
Set-Location $probeRoot
& codex
exit $LASTEXITCODE
