#!/bin/bash
# dot-agents Go binary installer
# https://github.com/NikashPrakash/dot-agents
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/dot-agents/dot-agents/main/scripts/install-go.sh | bash
#
# Options (via environment variables):
#   INSTALL_DIR       - Installation directory (default: ~/.local/bin)
#   DOT_AGENTS_VERSION - Specific version to install (default: latest)

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

REPO="dot-agents/dot-agents"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${DOT_AGENTS_VERSION:-}"

info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
error()   { echo -e "${RED}[ERROR]${NC} $1" >&2; }
die()     { error "$1"; exit 1; }

detect_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case "$arch" in
    x86_64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) die "Unsupported architecture: $arch" ;;
  esac

  case "$os" in
    linux|darwin) ;;
    msys*|mingw*|cygwin*) os="windows" ;;
    *) die "Unsupported OS: $os (use install-go.ps1 on Windows)" ;;
  esac

  echo "${os}_${arch}"
}

get_latest_version() {
  local url="https://api.github.com/repos/${REPO}/releases/latest"
  if command -v curl &>/dev/null; then
    curl -fsSL "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(v[^"]*\)".*/\1/'
  elif command -v wget &>/dev/null; then
    wget -qO- "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(v[^"]*\)".*/\1/'
  else
    die "curl or wget is required to download dot-agents"
  fi
}

download_binary() {
  local version="$1"
  local platform="$2"
  local tmpdir
  tmpdir=$(mktemp -d)

  local ext="tar.gz"
  local binary="dot-agents"
  # Windows releases use zip
  if [[ "$platform" == windows* ]]; then
    ext="zip"
    binary="dot-agents.exe"
  fi

  local filename="dot-agents_${version#v}_${platform}.${ext}"
  local url="https://github.com/${REPO}/releases/download/${version}/${filename}"

  info "Downloading dot-agents ${version} for ${platform}..."

  if command -v curl &>/dev/null; then
    curl -fsSL "$url" -o "$tmpdir/$filename"
  else
    wget -qO "$tmpdir/$filename" "$url"
  fi

  if [[ "$ext" == "zip" ]]; then
    unzip -q "$tmpdir/$filename" -d "$tmpdir"
  else
    tar -xzf "$tmpdir/$filename" -C "$tmpdir"
  fi

  echo "$tmpdir/$binary"
}

main() {
  echo -e "${BOLD}dot-agents installer${NC}"
  echo ""

  local platform
  platform=$(detect_platform)

  if [ -z "$VERSION" ]; then
    info "Fetching latest version..."
    VERSION=$(get_latest_version)
    if [ -z "$VERSION" ]; then
      die "Could not determine latest version. Set DOT_AGENTS_VERSION manually."
    fi
    info "Latest version: $VERSION"
  fi

  local binary
  binary=$(download_binary "$VERSION" "$platform")

  mkdir -p "$INSTALL_DIR"
  cp "$binary" "$INSTALL_DIR/dot-agents"
  chmod +x "$INSTALL_DIR/dot-agents"

  success "Installed dot-agents ${VERSION} to ${INSTALL_DIR}/dot-agents"

  # Check if INSTALL_DIR is in PATH
  if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    warn "${INSTALL_DIR} is not in your PATH."
    echo ""
    echo "Add it with:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    echo ""
    echo "Then add to your shell profile (.bashrc, .zshrc, etc.)"
  fi

  echo ""
  echo "Run: dot-agents --help"
  echo "Initialize: dot-agents init"
}

main "$@"
