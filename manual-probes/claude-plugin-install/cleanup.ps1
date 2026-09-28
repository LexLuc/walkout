param(
    [switch]$RemoveArtifacts
)

$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp\walkout-plugin-probe'))
$tempRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp'))
if (-not $probeRoot.StartsWith($tempRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "unsafe probe path: $probeRoot"
}

$probeDaemon = [IO.Path]::GetFullPath((Join-Path $probeRoot 'bin\walkoutd.exe'))
$running = Get-Process -Name 'walkoutd' -ErrorAction SilentlyContinue | Where-Object {
    try { [IO.Path]::GetFullPath($_.Path) -eq $probeDaemon } catch { $false }
}
if ($running) {
    throw 'the probe daemon is still running; stop terminal A with Ctrl+C before cleanup'
}

$plugins = (& claude plugin list 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) { throw 'failed to inspect installed Claude Code plugins; probe artifacts preserved' }
if ($plugins -match '(?i)walkout') {
    & claude plugin uninstall walkout@lexicon
    if ($LASTEXITCODE -ne 0) { throw 'failed to uninstall walkout@lexicon; probe artifacts preserved' }
}
else {
    Write-Host 'walkout plugin already absent.'
}

$markets = (& claude plugin marketplace list 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) { throw 'failed to inspect Claude Code marketplaces; probe artifacts preserved' }
if ($markets -match '(?i)lexicon') {
    & claude plugin marketplace remove lexicon
    if ($LASTEXITCODE -ne 0) { throw 'failed to remove lexicon marketplace; probe artifacts preserved' }
}
else {
    Write-Host 'lexicon marketplace already absent.'
}

if ($RemoveArtifacts -and (Test-Path -LiteralPath $probeRoot)) {
    Remove-Item -LiteralPath $probeRoot -Recurse -Force
    Write-Host "Removed probe artifacts: $probeRoot"
}
elseif (Test-Path -LiteralPath $probeRoot) {
    Write-Host "Preserved probe artifacts: $probeRoot (use -RemoveArtifacts to delete)"
}
Write-Host 'Removed temporary Claude Code plugin and marketplace configuration.'
