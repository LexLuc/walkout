#Requires -Version 5.1
# Builds the Walkout binaries that the walkout plugin references into ./bin.
#
# The plugin's slash commands invoke ${CLAUDE_PLUGIN_ROOT}/bin/walkout-ctl.exe.
# The recommended hook wiring (installed into the host's settings, not bundled in
# this manifest) invokes ${CLAUDE_PLUGIN_ROOT}/bin/walkout-hook.exe. Both are
# produced here so a packaged plugin directory is self-contained on Windows x64.
#
# The daemon (walkoutd) is a separate background service and is not built here.

$ErrorActionPreference = "Stop"

$pluginRoot = $PSScriptRoot
$repoRoot = [IO.Path]::GetFullPath((Join-Path $pluginRoot '..\..'))
$binDir = Join-Path $pluginRoot 'bin'
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

$targets = @(
    @{ Name = 'walkout-ctl.exe';  Pkg = './cmd/walkout-ctl'  },
    @{ Name = 'walkout-hook.exe'; Pkg = './cmd/walkout-hook' }
)

Push-Location $repoRoot
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    foreach ($t in $targets) {
        $out = Join-Path $binDir $t.Name
        Write-Host "building $($t.Name) -> $out"
        & go build -o $out $t.Pkg
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $($t.Pkg)" }
    }
}
finally {
    Pop-Location
}

Write-Host "Done. Plugin binaries are in $binDir"
