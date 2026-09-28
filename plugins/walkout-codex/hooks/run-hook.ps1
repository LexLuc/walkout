#Requires -Version 7.0

param(
    [Parameter(Mandatory = $true)]
    [string]$HookExe,
    [ValidateSet('codex')]
    [string]$Provider = 'codex',
    [string]$HostPackageEnv
)

# Codex's managed JS launcher exposes CODEX_MANAGED_PACKAGE_ROOT. The hook
# command passes only that variable's NAME; this always-PowerShell wrapper
# resolves it here, so version detection never depends on which shell (POSIX
# sh vs PowerShell) expanded the invoking command string. Standalone binaries
# leave the variable missing, and the resulting empty version deliberately
# reaches the translator, which records unknown.
try {
    $utf8 = [Text.UTF8Encoding]::new($false)
    [Console]::InputEncoding = $utf8
    [Console]::OutputEncoding = $utf8
    $rawInput = [Console]::In.ReadToEnd()

    $hostPackage = ''
    if ($HostPackageEnv) {
        $packageRoot = [Environment]::GetEnvironmentVariable($HostPackageEnv)
        if ($packageRoot) {
            $hostPackage = Join-Path $packageRoot 'package.json'
        }
    }

    $hostVersion = ''
    if ($hostPackage -and [IO.File]::Exists($hostPackage)) {
        try {
            $package = [IO.File]::ReadAllText($hostPackage, $utf8) | ConvertFrom-Json
            if ($null -ne $package.version) {
                $hostVersion = ([string]$package.version).Trim()
            }
        }
        catch {
            $hostVersion = ''
        }
    }

    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $HookExe
    $startInfo.UseShellExecute = $false
    $startInfo.RedirectStandardInput = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $startInfo.ArgumentList.Add('-provider')
    $startInfo.ArgumentList.Add($Provider)
    $startInfo.ArgumentList.Add('-host-version')
    $startInfo.ArgumentList.Add($hostVersion)

    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    if (-not $process.Start()) {
        exit 0
    }
    # Start the async output readers BEFORE writing stdin. A synchronous stdin
    # write ahead of draining stdout/stderr would deadlock if the child filled
    # its output pipe buffer before consuming all input.
    $stdoutTask = $process.StandardOutput.ReadToEndAsync()
    $stderrTask = $process.StandardError.ReadToEndAsync()
    $process.StandardInput.Write($rawInput)
    $process.StandardInput.Close()
    $process.WaitForExit()
    $stdout = $stdoutTask.GetAwaiter().GetResult()
    $stderr = $stderrTask.GetAwaiter().GetResult()

    if ($process.ExitCode -ne 0) {
        exit 0
    }
    if ($stdout.Length -gt 0) {
        [Console]::Out.Write($stdout)
    }
    if ($stderr.Length -gt 0) {
        [Console]::Error.Write($stderr)
    }
}
catch {
    # Distribution and wrapper failures preserve the hook CLI's silent
    # fail-open contract so host work is never trapped by the guard.
}

exit 0
