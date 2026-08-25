# Thin wrapper: install the unity-pipe harness skills into both agent
# harnesses (AI Mana ~/.claude/skills and Codex ~/.codex/skills).
# Usage:  pwsh harness/install.ps1 [-Claude] [-Codex] [-DryRun]
param(
    [switch]$Claude,
    [switch]$Codex,
    [switch]$DryRun
)

$repoRoot = Split-Path -Parent $PSScriptRoot   # harness/ -> repo root

$commonArgs = @('plugin', 'install', '--from', $repoRoot)
if ($Claude) { $commonArgs += '--claude' }
if ($Codex)  { $commonArgs += '--codex' }
if ($DryRun) { $commonArgs += '--dry-run' }

# Prefer an installed upipe on PATH; fall back to building from source.
$upipe = Get-Command upipe -ErrorAction SilentlyContinue
if ($upipe) {
    & upipe @commonArgs
} else {
    Write-Host "upipe not on PATH - building from source..." -ForegroundColor Yellow
    & go run ./cmd/upipe @commonArgs
}
exit $LASTEXITCODE
