param(
    [ValidateSet('walkout', 'overtime')]
    [string]$State = 'walkout'
)
$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp\codex-emergency-probe'))
$tempRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp'))
if (-not $probeRoot.StartsWith($tempRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "unsafe probe path: $probeRoot"
}

foreach ($databaseName in @('state.db', 'state.db-shm', 'state.db-wal')) {
    $databasePath = Join-Path $probeRoot $databaseName
    if (Test-Path -LiteralPath $databasePath) {
        Remove-Item -LiteralPath $databasePath -Force
    }
}
$recordDir = Join-Path $probeRoot 'records'
if (Test-Path -LiteralPath $recordDir) {
    Get-ChildItem -LiteralPath $recordDir -Filter '*.json' -File | Remove-Item -Force
    $eventProbePath = Join-Path $recordDir 'health-events.jsonl'
    if (Test-Path -LiteralPath $eventProbePath) {
        Remove-Item -LiteralPath $eventProbePath -Force
    }
}
& (Join-Path $probeRoot 'bin\seed-paused.exe') (Join-Path $probeRoot 'state.db') $State
if ($LASTEXITCODE -ne 0) { throw "failed to seed $State state" }
Write-Host "Reset complete: $State, no emergency lease, no captured control payload."
