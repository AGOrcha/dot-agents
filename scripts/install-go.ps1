# dot-agents Windows installer (PowerShell)
# https://github.com/dot-agents/dot-agents
#
# Usage (run in PowerShell as Administrator, or with Developer Mode enabled):
#   irm https://raw.githubusercontent.com/dot-agents/dot-agents/main/scripts/install-go.ps1 | iex
#
# Options (environment variables):
#   $env:INSTALL_DIR         - Installation directory (default: $env:LOCALAPPDATA\Programs\dot-agents)
#   $env:DOT_AGENTS_VERSION  - Specific version (default: latest)

$ErrorActionPreference = 'Stop'

$REPO = "dot-agents/dot-agents"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\dot-agents" }
$Version = $env:DOT_AGENTS_VERSION

function Write-Info  { Write-Host "[INFO] $args" -ForegroundColor Cyan }
function Write-Ok    { Write-Host "[ OK ] $args" -ForegroundColor Green }
function Write-Warn  { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Fail  { Write-Host "[FAIL] $args" -ForegroundColor Red; exit 1 }

function Get-Arch {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        'X64'   { return 'amd64' }
        'Arm64' { return 'arm64' }
        default { Write-Fail "Unsupported architecture: $arch" }
    }
}

function Get-LatestVersion {
    $url = "https://api.github.com/repos/$REPO/releases/latest"
    $release = Invoke-RestMethod -Uri $url -UseBasicParsing
    return $release.tag_name
}

function Install-DotAgents {
    Write-Host ""
    Write-Host "dot-agents installer" -ForegroundColor White -BackgroundColor DarkBlue
    Write-Host ""

    $arch = Get-Arch

    if (-not $Version) {
        Write-Info "Fetching latest version..."
        $Version = Get-LatestVersion
        Write-Info "Latest version: $Version"
    }

    $versionNum = $Version.TrimStart('v')
    $filename = "dot-agents_${versionNum}_windows_${arch}.zip"
    $url = "https://github.com/$REPO/releases/download/$Version/$filename"

    Write-Info "Downloading dot-agents $Version for windows/$arch..."

    $tmpDir = [System.IO.Path]::GetTempPath() + [System.Guid]::NewGuid().ToString()
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    $zipPath = "$tmpDir\$filename"
    Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing

    Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

    # Create install dir
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    Copy-Item "$tmpDir\dot-agents.exe" "$InstallDir\dot-agents.exe" -Force
    Remove-Item $tmpDir -Recurse -Force

    Write-Ok "Installed dot-agents $Version to $InstallDir\dot-agents.exe"

    # Add to PATH if not already there
    $currentPath = [System.Environment]::GetEnvironmentVariable('PATH', 'User')
    if ($currentPath -notlike "*$InstallDir*") {
        [System.Environment]::SetEnvironmentVariable(
            'PATH',
            "$InstallDir;$currentPath",
            'User'
        )
        Write-Ok "Added $InstallDir to user PATH"
        Write-Warn "Restart your terminal for PATH changes to take effect"
    }

    Write-Host ""
    Write-Host "Run: dot-agents --help"
    Write-Host "Initialize: dot-agents init"
    Write-Host ""

    # Note about symlinks on Windows
    Write-Warn "Windows Note: Symlink creation requires Developer Mode or Administrator privileges."
    Write-Warn "Enable Developer Mode: Settings → System → For Developers → Developer Mode"
}

Install-DotAgents
