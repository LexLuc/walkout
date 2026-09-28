param(
    [switch]$RemoveArtifacts
)

$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$probeRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp\codex-emergency-probe'))
$tempRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.tmp'))
if (-not $probeRoot.StartsWith($tempRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "unsafe probe path: $probeRoot"
}

$probeDaemonPath = [IO.Path]::GetFullPath((Join-Path $probeRoot 'bin\walkoutd.exe'))
$probeDaemonRunning = Get-Process -Name 'walkoutd' -ErrorAction SilentlyContinue | Where-Object {
    try {
        [IO.Path]::GetFullPath($_.Path) -eq $probeDaemonPath
    }
    catch {
        $false
    }
}
if ($probeDaemonRunning) {
    throw 'the probe daemon is still running; stop terminal A with Ctrl+C before cleanup'
}

$pluginList = (& codex plugin list 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) {
    throw 'failed to inspect installed Codex plugins; probe artifacts were preserved'
}
if ($pluginList -match '(?i)walkout') {
    & codex plugin remove walkout@walkout-probe
    if ($LASTEXITCODE -ne 0) {
        throw 'failed to remove walkout@walkout-probe; probe artifacts were preserved'
    }
}
else {
    Write-Host 'Temporary walkout plugin is already absent.'
}

$marketplaceList = (& codex plugin marketplace list 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) {
    throw 'failed to inspect Codex marketplaces; probe artifacts were preserved'
}
if ($marketplaceList -match '(?i)walkout-probe') {
    & codex plugin marketplace remove walkout-probe
    if ($LASTEXITCODE -ne 0) {
        throw 'failed to remove walkout-probe marketplace; probe artifacts were preserved'
    }
}
else {
    Write-Host 'Temporary walkout-probe marketplace is already absent.'
}

if ($RemoveArtifacts -and (Test-Path -LiteralPath $probeRoot)) {
    Remove-Item -LiteralPath $probeRoot -Recurse -Force
    Write-Host "Removed probe artifacts: $probeRoot"
}
elseif (Test-Path -LiteralPath $probeRoot) {
    Write-Host "Preserved probe artifacts for fixture review: $probeRoot"
}
Write-Host 'Removed temporary Codex plugin and marketplace configuration.'
