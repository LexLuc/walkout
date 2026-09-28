$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = Join-Path $repoRoot '.tmp\codex-emergency-probe'
& (Join-Path $probeRoot 'bin\walkoutd.exe') -database (Join-Path $probeRoot 'state.db')
exit $LASTEXITCODE
