$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = Join-Path $repoRoot '.tmp\walkout-plugin-probe'
# Default (per-user SID) named pipe, so the installed plugin hook — which passes
# no -pipe — connects to this seeded service_paused state.
& (Join-Path $probeRoot 'bin\walkoutd.exe') -database (Join-Path $probeRoot 'state.db')
exit $LASTEXITCODE
