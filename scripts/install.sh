#!/bin/bash
# da installer — downloads and installs the Go CLI release binary (`da`)
# https://github.com/NikashPrakash/dot-agents
#
# Usage:
#   curl --proto "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/NikashPrakash/dot-agents/main/scripts/install.sh | bash
#
# Environment:
#   DOT_AGENTS_INSTALL_DIR  Binary install directory (default: ~/.local/bin)
#   DOT_AGENTS_VERSION      Specific version tag (default: latest release)
#   DOT_AGENTS_LOCAL_SRC    Local repo checkout to `go build` from (for testing)

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

REPO="NikashPrakash/dot-agents"
INSTALL_DIR="${DOT_AGENTS_INSTALL_DIR:-${INSTALL_DIR:-$HOME/.local/bin}}"
VERSION="${DOT_AGENTS_VERSION:-}"
LOCAL_SRC="${DOT_AGENTS_LOCAL_SRC:-}"

info()    { local msg="$1"; echo -e "${BLUE}[INFO]${NC} $msg"; }
success() { local msg="$1"; echo -e "${GREEN}[OK]${NC} $msg"; }
warn()    { local msg="$1"; echo -e "${YELLOW}[WARN]${NC} $msg"; }
error()   { local msg="$1"; echo -e "${RED}[ERROR]${NC} $msg" >&2; }
die()     { local msg="$1"; error "$msg"; exit 1; }

usage() {
  cat <<'EOF'
Usage: install.sh

Installs the Go CLI release binary (`da`).

Environment:
  DOT_AGENTS_INSTALL_DIR=/path/to/bin
  DOT_AGENTS_VERSION=vX.Y.Z
  DOT_AGENTS_LOCAL_SRC=/path/to/repo
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    local arg="$1"
    case "$arg" in
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "Unknown argument: $arg"
        ;;
    esac
  done
}

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
    *) die "Unsupported OS: $os (use install.ps1 on Windows)" ;;
  esac

  echo "${os}_${arch}"
}

get_latest_version() {
  local url="https://api.github.com/repos/${REPO}/releases/latest"
  if command -v curl >/dev/null 2>&1; then
    curl --proto "=https" --tlsv1.2 -fsSL "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(v[^"]*\)".*/\1/'
  elif command -v wget >/dev/null 2>&1; then
    wget --https-only --max-redirect=0 -qO- "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(v[^"]*\)".*/\1/'
  else
    die "curl or wget is required to download da"
  fi
}

download_binary() {
  local version="$1"
  local platform="$2"
  local tmpdir
  tmpdir=$(mktemp -d)

  local ext="tar.gz"
  local binary="da"
  if [[ "$platform" == windows* ]]; then
    ext="zip"
    binary="da.exe"
  fi

  local filename="dot-agents_${version#v}_${platform}.${ext}"
  local url="https://github.com/${REPO}/releases/download/${version}/${filename}"

  info "Downloading da ${version} for ${platform}..." >&2

  if command -v curl >/dev/null 2>&1; then
    curl --proto "=https" --tlsv1.2 -fsSL "$url" -o "$tmpdir/$filename"
  else
    wget --https-only -qO "$tmpdir/$filename" "$url"
  fi

  if [[ "$ext" == "zip" ]]; then
    unzip -q "$tmpdir/$filename" -d "$tmpdir"
  else
    tar -xzf "$tmpdir/$filename" -C "$tmpdir"
  fi

  echo "$tmpdir/$binary"
}

build_from_local_src() {
  command -v go >/dev/null 2>&1 || die "go is required to build from DOT_AGENTS_LOCAL_SRC"
  [[ -d "$LOCAL_SRC/cmd/da" ]] || die "DOT_AGENTS_LOCAL_SRC must point at a repo checkout with cmd/da"
  local tmpdir
  tmpdir=$(mktemp -d)
  info "Building da from local source ${LOCAL_SRC}..." >&2
  (cd "$LOCAL_SRC" && go build -o "$tmpdir/da" ./cmd/da)
  echo "$tmpdir/da"
}

ensure_install_dir_on_path() {
  if echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    return
  fi
  warn "${INSTALL_DIR} is not in your PATH."
  echo ""
  echo "Add it with:"
  echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
  echo ""
  echo "Then add to your shell profile (.bashrc, .zshrc, etc.)"
}

main() {
  parse_args "$@"

  echo ""
  echo -e "${BOLD}da installer${NC}"
  echo "─────────────────────────────────────"
  echo ""

  local binary
  if [[ -n "$LOCAL_SRC" ]]; then
    binary=$(build_from_local_src)
  else
    local platform
    platform=$(detect_platform)
    if [[ -z "$VERSION" ]]; then
      info "Fetching latest version..."
      VERSION=$(get_latest_version)
      [[ -n "$VERSION" ]] || die "Could not determine latest version. Set DOT_AGENTS_VERSION manually."
      info "Latest version: $VERSION"
    fi
    binary=$(download_binary "$VERSION" "$platform")
  fi

  mkdir -p "$INSTALL_DIR"
  cp "$binary" "$INSTALL_DIR/da"
  chmod +x "$INSTALL_DIR/da"

  if [[ -n "$VERSION" ]]; then
    success "Installed da ${VERSION} to ${INSTALL_DIR}/da"
  else
    success "Installed da to ${INSTALL_DIR}/da"
  fi

  ensure_install_dir_on_path

  echo ""
  echo "Run: da --help"
  echo "Initialize: da init"
}

main "$@"
