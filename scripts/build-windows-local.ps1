# Build a locally identifiable Windows da.exe, optionally signing it.
#
# Build cmd/da/main.go explicitly: this checkout's CLI package has only that
# production Go file, and the explicit entrypoint avoids this machine's failing
# package-directory linker path under Airlock.
# Usage:
#   pwsh -File scripts/build-windows-local.ps1 -Sign
#   pwsh -File scripts/build-windows-local.ps1 -Output C:\path\to\da.exe -Sign
[CmdletBinding()]
param(
    [string]$Output = (Join-Path $env:LOCALAPPDATA 'dot-agents\bin\da.exe'),
    [switch]$Sign
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-GitValue([string[]]$Arguments, [string]$Description) {
    $value = (& git @Arguments).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($value)) {
        throw "Could not resolve $Description from git. Run this script from a checkout with reachable tags."
    }
    return $value
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Push-Location $repoRoot
try {
    $tag = Get-GitValue @('describe', '--tags', '--abbrev=0') 'the latest tag'
    $shortCommit = Get-GitValue @('rev-parse', '--short=6', 'HEAD') 'the current short commit'
    $commit = Get-GitValue @('rev-parse', 'HEAD') 'the current commit'
    $describe = Get-GitValue @('describe', '--tags', '--always', '--abbrev=6', 'HEAD') 'the current description'
    $version = "${tag}-b${shortCommit}"
    $outputPath = [IO.Path]::GetFullPath($Output)
    [IO.Directory]::CreateDirectory((Split-Path -Parent $outputPath)) | Out-Null
    $ldflags = @(
        "-X github.com/AGOrcha/dot-agents/commands.Version=$version",
        "-X github.com/AGOrcha/dot-agents/commands.Commit=$commit",
        "-X github.com/AGOrcha/dot-agents/commands.Describe=$describe"
    ) -join ' '

    go build -ldflags $ldflags -o $outputPath ./cmd/da/main.go
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    Write-Output "Built da $version at $outputPath"
}
finally { Pop-Location }

if ($Sign) {
    & (Join-Path $PSScriptRoot 'sign-windows-local.ps1') $outputPath
    if ($LASTEXITCODE -ne 0) { throw "local signing failed with exit code $LASTEXITCODE" }
}
