$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp\codex-emergency-probe'))
$tempRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp'))
if (-not $probeRoot.StartsWith($tempRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "unsafe probe path: $probeRoot"
}

$binDir = Join-Path $probeRoot 'bin'
$recordDir = Join-Path $probeRoot 'records'
$hookConfigDir = Join-Path $probeRoot '.codex'
$marketplaceRoot = Join-Path $probeRoot 'marketplace'
$marketplacePluginDir = Join-Path $marketplaceRoot 'plugins\walkout-codex'
foreach ($directory in @($binDir, $recordDir, $hookConfigDir, $marketplaceRoot)) {
    [IO.Directory]::CreateDirectory($directory) | Out-Null
}

$taskGoCache = Join-Path $repoRoot '.tmp\go-build'
$taskGoModCache = Join-Path $repoRoot '.tmp\go-mod'
$env:GOCACHE = $taskGoCache
$env:GOMODCACHE = $taskGoModCache
Push-Location $repoRoot
try {
    & go build -o (Join-Path $binDir 'walkout-ctl.exe') ./cmd/walkout-ctl
    if ($LASTEXITCODE -ne 0) { throw 'failed to build walkout-ctl' }
    & go build -o (Join-Path $binDir 'walkoutd.exe') ./cmd/walkoutd
    if ($LASTEXITCODE -ne 0) { throw 'failed to build walkoutd' }
    & go build -o (Join-Path $binDir 'seed-paused.exe') ./manual-probes/codex-emergency-continue/seed-paused
    if ($LASTEXITCODE -ne 0) { throw 'failed to build seed-paused' }
}
finally {
    Pop-Location
}

& (Join-Path $repoRoot 'plugins\walkout-codex\build.ps1')
if ($LASTEXITCODE -ne 0) { throw 'failed to build the production Codex plugin hook' }

foreach ($databaseName in @('state.db', 'state.db-shm', 'state.db-wal')) {
    $databasePath = Join-Path $probeRoot $databaseName
    if (Test-Path -LiteralPath $databasePath) {
        Remove-Item -LiteralPath $databasePath -Force
    }
}
& (Join-Path $binDir 'seed-paused.exe') (Join-Path $probeRoot 'state.db')
if ($LASTEXITCODE -ne 0) { throw 'failed to seed walkout state' }

Get-ChildItem -LiteralPath $recordDir -Filter '*.json' -File | Remove-Item -Force
$eventProbePath = Join-Path $recordDir 'health-events.jsonl'
if (Test-Path -LiteralPath $eventProbePath) {
    Remove-Item -LiteralPath $eventProbePath -Force
}
$staleProjectHook = Join-Path $hookConfigDir 'hooks.json'
if (Test-Path -LiteralPath $staleProjectHook) {
    Remove-Item -LiteralPath $staleProjectHook -Force
}

if (Test-Path -LiteralPath $marketplacePluginDir) {
    $resolvedPluginDir = [IO.Path]::GetFullPath($marketplacePluginDir)
    if (-not $resolvedPluginDir.StartsWith([IO.Path]::GetFullPath($marketplaceRoot) + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw "unsafe marketplace plugin path: $resolvedPluginDir"
    }
    Remove-Item -LiteralPath $resolvedPluginDir -Recurse -Force
}
[IO.Directory]::CreateDirectory((Split-Path -Parent $marketplacePluginDir)) | Out-Null
Copy-Item -LiteralPath (Join-Path $repoRoot 'plugins\walkout-codex') -Destination $marketplacePluginDir -Recurse
$marketplaceConfigDir = Join-Path $marketplaceRoot '.agents\plugins'
[IO.Directory]::CreateDirectory($marketplaceConfigDir) | Out-Null
$marketplace = [ordered]@{
    name = 'walkout-probe'
    interface = [ordered]@{ displayName = 'Walkout Probe' }
    plugins = @(
        [ordered]@{
            name = 'walkout'
            source = [ordered]@{ source = 'local'; path = './plugins/walkout-codex' }
            policy = [ordered]@{ installation = 'AVAILABLE'; authentication = 'ON_INSTALL' }
            category = 'Productivity'
        }
    )
}
$marketplace | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $marketplaceConfigDir 'marketplace.json') -Encoding utf8NoBOM

Write-Host "Prepared Codex emergency-continue probe at $probeRoot"
Write-Host 'The probe has no project hook; lifecycle events come from the copied production plugin.'
Write-Host "Sanitized event metadata will be written to $eventProbePath"
Write-Host "Next: add the temporary marketplace, install walkout, then use start-daemon.ps1 and start-codex.ps1 in separate terminals."
