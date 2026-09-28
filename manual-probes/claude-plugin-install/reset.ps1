param(
    [ValidateSet('walkout', 'overtime')]
    [string]$State = 'walkout'
)
$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp\walkout-plugin-probe'))
$tempRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp'))
if (-not $probeRoot.StartsWith($tempRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "unsafe probe path: $probeRoot"
}

foreach ($n in @('state.db', 'state.db-shm', 'state.db-wal')) {
    $p = Join-Path $probeRoot $n
    if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Force }
}
$echo = Join-Path $probeRoot 'version-echo.txt'
if (Test-Path -LiteralPath $echo) { Remove-Item -LiteralPath $echo -Force }

& (Join-Path $probeRoot 'bin\seed-paused.exe') (Join-Path $probeRoot 'state.db') $State
if ($LASTEXITCODE -ne 0) { throw "failed to seed $State state" }
Write-Host "Reset complete: $State, no emergency lease, version-echo cleared."
