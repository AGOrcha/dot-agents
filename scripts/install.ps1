# da installer (PowerShell) — downloads and installs the Go CLI release binary (`da`)
# https://github.com/NikashPrakash/dot-agents
#
# Usage (run in PowerShell; Developer Mode or Administrator recommended for symlinks):
#   irm https://raw.githubusercontent.com/NikashPrakash/dot-agents/main/scripts/install.ps1 | iex
#
# Options (environment variables):
#   $env:DOT_AGENTS_INSTALL_DIR - Installation directory (default: $env:LOCALAPPDATA\Programs\dot-agents)
#   $env:DOT_AGENTS_VERSION     - Specific version tag (default: latest release)
#   $env:DOT_AGENTS_LOCAL_SRC   - Local repo checkout to `go build` from (for testing)

$ErrorActionPreference = 'Stop'

$REPO = "NikashPrakash/dot-agents"
$InstallDir = if ($env:DOT_AGENTS_INSTALL_DIR) { $env:DOT_AGENTS_INSTALL_DIR } elseif ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\dot-agents" }
$Version = $env:DOT_AGENTS_VERSION
$LocalSrc = $env:DOT_AGENTS_LOCAL_SRC

function Write-Info  { Write-Output "[INFO] $args" }
function Write-Ok    { Write-Output "[ OK ] $args" }
function Write-Warn  { Write-Output "[WARN] $args" }
function Write-Fail  { Write-Output "[FAIL] $args"; exit 1 }

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

function Get-DaBinaryFromRelease {
    param([string]$ResolvedVersion)

    $arch = Get-Arch
    $versionNum = $ResolvedVersion.TrimStart('v')
    $filename = "dot-agents_${versionNum}_windows_${arch}.zip"
    $url = "https://github.com/$REPO/releases/download/$ResolvedVersion/$filename"

    Write-Info "Downloading da $ResolvedVersion for windows/$arch..."

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    $zipPath = Join-Path $tmpDir $filename
    Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing
    Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

    return (Join-Path $tmpDir "da.exe")
}

function Get-DaBinaryFromLocalSrc {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Fail "go is required to build from DOT_AGENTS_LOCAL_SRC"
    }
    if (-not (Test-Path (Join-Path $LocalSrc "cmd\da"))) {
        Write-Fail "DOT_AGENTS_LOCAL_SRC must point at a repo checkout with cmd\da"
    }

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    $binary = Join-Path $tmpDir "da.exe"
    Write-Info "Building da from local source $LocalSrc..."
    Push-Location $LocalSrc
    try {
        & go build -o $binary ./cmd/da
        if ($LASTEXITCODE -ne 0) { Write-Fail "go build failed" }
    }
    finally {
        Pop-Location
    }

    return $binary
}

function Install-DotAgents {
    Write-Output ""
    Write-Output "da installer"
    Write-Output "-------------------------------------"
    Write-Output ""

    if ($LocalSrc) {
        $binary = Get-DaBinaryFromLocalSrc
    }
    else {
        if (-not $Version) {
            Write-Info "Fetching latest version..."
            $Version = Get-LatestVersion
            if (-not $Version) { Write-Fail "Could not determine latest version. Set DOT_AGENTS_VERSION manually." }
            Write-Info "Latest version: $Version"
        }
        $binary = Get-DaBinaryFromRelease -ResolvedVersion $Version
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    Copy-Item $binary (Join-Path $InstallDir "da.exe") -Force

    if ($Version) {
        Write-Ok "Installed da $Version to $InstallDir\da.exe"
    }
    else {
        Write-Ok "Installed da to $InstallDir\da.exe"
    }

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

    Write-Output ""
    Write-Output "Run: da --help"
    Write-Output "Initialize: da init"
    Write-Output ""

    Write-Warn "Windows Note: Symlink creation requires Developer Mode or Administrator privileges."
    Write-Warn "Enable Developer Mode: Settings -> System -> For Developers -> Developer Mode"
}

Install-DotAgents
