#Requires -Version 5.1
# Builds the Windows x64 hook binary referenced by .codex/hooks.json into ./bin.
# The walkoutd daemon remains a separately installed background service.

$ErrorActionPreference = 'Stop'

$pluginRoot = $PSScriptRoot
$repoRoot = [IO.Path]::GetFullPath((Join-Path $pluginRoot '..\..'))
$binDir = Join-Path $pluginRoot 'bin'
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

Push-Location $repoRoot
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $output = Join-Path $binDir 'walkout-hook.exe'
    & go build -o $output ./cmd/walkout-hook
    if ($LASTEXITCODE -ne 0) {
        throw 'go build failed for ./cmd/walkout-hook'
    }
}
finally {
    Pop-Location
}

Write-Host "Done. Codex plugin hook binary is in $binDir"
