param(
    [Parameter(Mandatory = $true)]
    [string]$HookExe,
    [Parameter(Mandatory = $true)]
    [string]$RecordDir,
    [Parameter(Mandatory = $true)]
    [string]$HostVersion
)

$ErrorActionPreference = "Stop"
$utf8 = [Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = $utf8
[Console]::OutputEncoding = $utf8
$rawInput = [Console]::In.ReadToEnd()
$payload = ConvertFrom-Json -InputObject $rawInput
$marker = '$walkout:continue'
$prompt = [string]$payload.prompt
$isControl = $payload.hook_event_name -eq 'UserPromptSubmit' -and (
    $prompt -eq $marker -or
    $prompt.StartsWith($marker + ' ') -or
    $prompt.StartsWith($marker + "`t")
)
if ($isControl -and $prompt -notmatch "[`r`n]") {
    [IO.Directory]::CreateDirectory($RecordDir) | Out-Null
    $target = [IO.Path]::Combine($RecordDir, 'UserPromptSubmit-' + [Guid]::NewGuid().ToString('N') + '.json')
    [IO.File]::WriteAllText($target, $rawInput, $utf8)
}

$startInfo = [Diagnostics.ProcessStartInfo]::new()
$startInfo.FileName = $HookExe
$startInfo.UseShellExecute = $false
$startInfo.RedirectStandardInput = $true
$startInfo.RedirectStandardOutput = $true
$startInfo.RedirectStandardError = $true
$startInfo.ArgumentList.Add('-provider')
$startInfo.ArgumentList.Add('codex')
$startInfo.ArgumentList.Add('-host-version')
$startInfo.ArgumentList.Add($HostVersion)

$process = [Diagnostics.Process]::new()
$process.StartInfo = $startInfo
if (-not $process.Start()) {
    throw 'failed to start walkout-hook'
}
$process.StandardInput.Write($rawInput)
$process.StandardInput.Close()
$stdout = $process.StandardOutput.ReadToEnd()
$stderr = $process.StandardError.ReadToEnd()
$process.WaitForExit()
if ($stdout.Length -gt 0) {
    [Console]::Out.Write($stdout)
}
if ($stderr.Length -gt 0) {
    [Console]::Error.Write($stderr)
}
exit $process.ExitCode
