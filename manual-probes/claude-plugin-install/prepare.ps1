#Requires -Version 5.1
# Prepares the Claude Code plugin-install probe: builds the plugin's shipped
# binaries, seeds a self-consistent walkout state, and lays out a probe
# project whose side-car SessionStart hook records how $CLAUDE_CODE_VERSION
# reaches a hook command string (the same mechanism the bundled plugin hook uses
# to pass -host-version). It deliberately does NOT add the marketplace or install
# the plugin — those persist changes in the user's Claude Code config, so they
# stay as two explicit commands printed at the end.

$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp\walkout-plugin-probe'))
$tempRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp'))
if (-not $probeRoot.StartsWith($tempRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "unsafe probe path: $probeRoot"
}

$binDir = Join-Path $probeRoot 'bin'
$projectDir = Join-Path $probeRoot 'project'
$claudeDir = Join-Path $projectDir '.claude'
foreach ($d in @($binDir, $projectDir, $claudeDir)) {
    [IO.Directory]::CreateDirectory($d) | Out-Null
}

$env:GOCACHE = Join-Path $repoRoot '.tmp\go-build'
$env:GOMODCACHE = Join-Path $repoRoot '.tmp\go-mod'

# 1. Build the plugin's shipped binaries into plugins/walkout/bin — what the
#    installed plugin references via ${CLAUDE_PLUGIN_ROOT}/bin/*.exe.
& pwsh -NoProfile -File (Join-Path $repoRoot 'plugins\walkout\build.ps1')
if ($LASTEXITCODE -ne 0) { throw 'failed to build plugin binaries' }

# 2. Build the daemon and the shared state seeder into the probe bin.
Push-Location $repoRoot
try {
    & go build -o (Join-Path $binDir 'walkoutd.exe') ./cmd/walkoutd
    if ($LASTEXITCODE -ne 0) { throw 'failed to build walkoutd' }
    & go build -o (Join-Path $binDir 'seed-paused.exe') ./manual-probes/codex-emergency-continue/seed-paused
    if ($LASTEXITCODE -ne 0) { throw 'failed to build seed-paused' }
}
finally {
    Pop-Location
}

# 3. Seed a self-consistent walkout state (debt already at the limit).
foreach ($n in @('state.db', 'state.db-shm', 'state.db-wal')) {
    $p = Join-Path $probeRoot $n
    if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Force }
}
& (Join-Path $binDir 'seed-paused.exe') (Join-Path $probeRoot 'state.db')
if ($LASTEXITCODE -ne 0) { throw 'failed to seed walkout state' }

# 4. Side-car version-echo hook. Claude Code parses a command hook via a POSIX
#    shell on Windows, so whether $CLAUDE_CODE_VERSION expands here is the same
#    question as in the bundled plugin hook. version-echo.ps1 records both the
#    command-string expansion and the raw env var.
$echoScript = ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot 'version-echo.ps1'))).Replace('\', '/')
$echoOut = (Join-Path $probeRoot 'version-echo.txt').Replace('\', '/')
$echoCommand = 'pwsh -NoProfile -File "{0}" -OutFile "{1}" -CmdStringVersion "$CLAUDE_CODE_VERSION"' -f $echoScript, $echoOut
$settings = [ordered]@{
    hooks = [ordered]@{
        SessionStart = @(
            [ordered]@{
                hooks = @(
                    [ordered]@{ type = 'command'; command = $echoCommand; timeout = 5 }
                )
            }
        )
    }
}
$settings | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $claudeDir 'settings.json') -Encoding utf8NoBOM
$echoOutWindows = $echoOut -replace '/', '\'
if (Test-Path -LiteralPath $echoOutWindows) { Remove-Item -LiteralPath $echoOutWindows -Force }

Write-Host "Prepared Claude plugin-install probe at $probeRoot"
Write-Host ""
Write-Host "Persistent Claude Code config changes (run explicitly; undo with cleanup.ps1):"
Write-Host "  claude plugin marketplace add `"$repoRoot`""
Write-Host "  claude plugin install walkout@lexicon"
Write-Host "  claude plugin details walkout@lexicon   # confirm bin/*.exe shipped"
Write-Host ""
Write-Host "Then two terminals:"
Write-Host "  A: pwsh -NoProfile -File `"$(Join-Path $PSScriptRoot 'start-daemon.ps1')`""
Write-Host "  B: cd `"$projectDir`"; claude"
Write-Host ""
Write-Host "After launch, read version-echo output:"
Write-Host "  Get-Content `"$($echoOut -replace '/', '\')`""
