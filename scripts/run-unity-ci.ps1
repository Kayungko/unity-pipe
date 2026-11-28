#requires -Version 7.4
[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string] $Upipe,
    [Parameter(Mandatory)] [string] $Unity,
    [Parameter(Mandatory)] [string] $ProjectPath,
    [Parameter(Mandatory)] [string] $ArtifactDirectory,
    [ValidateSet('all', 'editor', 'playmode')] [string] $Mode = 'all',
    [string] $TestFilter,
    [string] $AffectedFrom,
    [string] $AffectedTo,
    [switch] $RequireMatch,
    [switch] $Build,
    [string[]] $Scene = @(),
    [string] $TestTimeout = '15m',
    [string] $BuildTimeout = '30m'
)

$ErrorActionPreference = 'Stop'
# The CLI's exact exit code is recorded and returned below, including failures.
$PSNativeCommandUseErrorActionPreference = $false

if (!$IsWindows -or !$Upipe -or !$Unity -or !$ProjectPath -or !$ArtifactDirectory -or
    ([bool]$AffectedFrom -ne [bool]$AffectedTo) -or ($TestFilter -and $AffectedFrom)) {
    Write-Error -ErrorAction Continue 'Use Windows, supply the tool and project paths, and choose either a filter or both Git revisions.'
    exit 2
}

$Upipe = [IO.Path]::GetFullPath($Upipe)
$Unity = [IO.Path]::GetFullPath($Unity)
$ProjectPath = [IO.Path]::GetFullPath($ProjectPath)
$ArtifactDirectory = [IO.Path]::GetFullPath($ArtifactDirectory)
if (Test-Path -LiteralPath $ArtifactDirectory) {
    Write-Error -ErrorAction Continue 'ArtifactDirectory must be a new directory for this run.'
    exit 2
}
$reports = Join-Path $ArtifactDirectory 'reports'
New-Item -ItemType Directory -Path $reports | Out-Null

$summary = [ordered]@{
    schema = 1
    startedAt = [DateTimeOffset]::UtcNow.ToString('o')
    completedAt = $null
    exitCode = $null
    stages = [ordered]@{
        test = [ordered]@{ state = 'not_run'; exitCode = $null }
        build = [ordered]@{ state = 'not_requested'; exitCode = $null }
    }
}
if ($Build) { $summary.stages.build.state = 'not_run' }

function Invoke-UnityStage {
    param([string] $Name, [string] $Budget, [string[]] $Arguments)

    $stage = $summary.stages[$Name]
    $stage.state = 'running'
    $log = Join-Path $reports "$Name.editor.log"
    $result = Join-Path $reports "$Name.json"
    $diagnostics = Join-Path $reports "$Name.stderr.log"
    $invocation = @('run', '--json', '--unity', $Unity, '--project-path', $ProjectPath,
        '--timeout', $Budget, '--log-file', $log, '--') + $Arguments
    & $Upipe @invocation 1> $result 2> $diagnostics
    $stage.exitCode = $LASTEXITCODE
    $stage.state = if ($stage.exitCode -eq 0) { 'succeeded' } else { 'failed' }
    Write-Host "$Name finished with exit $($stage.exitCode)"
    return $stage.exitCode
}

$exitCode = 50
try {
    $tests = @('test', '--mode', $Mode, '--nunit', (Join-Path $reports 'nunit.xml'))
    if ($TestFilter) { $tests += "--filter=$TestFilter" }
    if ($AffectedFrom) { $tests += @('--affected', "--from=$AffectedFrom", "--to=$AffectedTo") }
    if ($RequireMatch) { $tests += '--require-match' }
    $exitCode = Invoke-UnityStage -Name test -Budget $TestTimeout -Arguments $tests
    if ($exitCode -eq 0 -and $Build) {
        $player = Join-Path $ArtifactDirectory 'player/Player.exe'
        $buildArgs = @('build', '--confirm', '--target', 'StandaloneWindows64', '--output-path', $player)
        foreach ($scenePath in $Scene) { $buildArgs += "--scene=$scenePath" }
        # A script/runtime failure during this stage must not inherit the successful test exit.
        $exitCode = 50
        $exitCode = Invoke-UnityStage -Name build -Budget $BuildTimeout -Arguments $buildArgs
    }
} catch {
    Write-Error -ErrorAction Continue $_
    $_.ToString() | Set-Content -LiteralPath (Join-Path $reports 'runner-error.log') -Encoding utf8NoBOM
    foreach ($stage in $summary.stages.Values) {
        if ($stage.state -eq 'running') { $stage.state = 'runner_error' }
    }
} finally {
    $summary.exitCode = $exitCode
    $summary.completedAt = [DateTimeOffset]::UtcNow.ToString('o')
    $summary | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $reports 'run.json') -Encoding utf8NoBOM
}
exit $exitCode
