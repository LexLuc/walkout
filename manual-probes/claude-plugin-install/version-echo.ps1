#Requires -Version 5.1
# Records how $CLAUDE_CODE_VERSION reaches a Claude Code command hook, to decide
# whether the bundled plugin hook's `-host-version "$CLAUDE_CODE_VERSION"` gets
# the real version. Writes nothing sensitive — only the CLI version and whether
# the command-string variable expanded.
#
# Interpret the output:
#   env_CLAUDE_CODE_VERSION      — is the env var present in the hook process?
#   cmd_string_expansion         — did $CLAUDE_CODE_VERSION expand in the command
#                                  string? A real version means the plugin hook's
#                                  -host-version gets it; the literal text
#                                  "$CLAUDE_CODE_VERSION" means it did not expand
#                                  (the hook then falls back to "unknown").

param(
    [Parameter(Mandatory = $true)][string]$OutFile,
    [string]$CmdStringVersion = ''
)

$ErrorActionPreference = 'SilentlyContinue'
$envVersion = $env:CLAUDE_CODE_VERSION
$lines = @(
    "captured_at_utc=$([DateTime]::UtcNow.ToString('o'))"
    "env_CLAUDE_CODE_VERSION=$envVersion"
    "cmd_string_expansion=$CmdStringVersion"
)
Set-Content -LiteralPath $OutFile -Value ($lines -join "`n") -Encoding utf8NoBOM
