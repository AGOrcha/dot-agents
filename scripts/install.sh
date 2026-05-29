#!/bin/bash
# da multi-target installer
# https://github.com/NikashPrakash/dot-agents
#
# Usage:
#   curl --proto "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/NikashPrakash/dot-agents/main/scripts/install.sh | bash
#   curl --proto "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/NikashPrakash/dot-agents/main/scripts/install.sh | bash -s -- --port ts
#
# Options:
#   --port go|ts                 Install target (default: go)
#
# Environment:
#   DOT_AGENTS_PORT              Install target fallback when --port is omitted
#   DOT_AGENTS_INSTALL_DIR       Binary install directory (default: ~/.local/bin)
#   DOT_AGENTS_LIB_DIR           TS runtime install dir (default: ~/.local/lib/dot-agents-ts)
#   DOT_AGENTS_VERSION           Specific version tag (default: latest release)
#   DOT_AGENTS_LOCAL_SRC         Local repo checkout for testing / local installs

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

REPO="NikashPrakash/dot-agents"
INSTALL_DIR="${DOT_AGENTS_INSTALL_DIR:-${INSTALL_DIR:-$HOME/.local/bin}}"
TS_LIB_DIR="${DOT_AGENTS_LIB_DIR:-$HOME/.local/lib/dot-agents-ts}"
PORT="${DOT_AGENTS_PORT:-go}"
VERSION="${DOT_AGENTS_VERSION:-}"
LOCAL_SRC="${DOT_AGENTS_LOCAL_SRC:-}"

info()    { local msg="$1"; echo -e "${BLUE}[INFO]${NC} $msg"; }
success() { local msg="$1"; echo -e "${GREEN}[OK]${NC} $msg"; }
warn()    { local msg="$1"; echo -e "${YELLOW}[WARN]${NC} $msg"; }
error()   { local msg="$1"; echo -e "${RED}[ERROR]${NC} $msg" >&2; }
die()     { local msg="$1"; error "$msg"; exit 1; }

usage() {
  cat <<'EOF'
Usage: install.sh [--port go|ts]

Targets:
  go  Install the Go CLI release binary (`da`). This is the default.
  ts  Install the TypeScript port launcher (`da-ts`) from the repo source bundle.

Environment:
  DOT_AGENTS_PORT=go|ts
  DOT_AGENTS_INSTALL_DIR=/path/to/bin
  DOT_AGENTS_LIB_DIR=/path/to/ts/runtime
  DOT_AGENTS_VERSION=vX.Y.Z
  DOT_AGENTS_LOCAL_SRC=/path/to/repo
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    local arg="$1"
    case "$arg" in
      --port)
        [[ $# -ge 2 ]] || die "--port requires a value"
        PORT="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "Unknown argument: $arg"
        ;;
    esac
  done
  case "$PORT" in
    go|ts) ;;
    *) die "Unsupported port: $PORT (expected go or ts)" ;;
  esac
}

check_requirements() {
  if ! command -v curl >/dev/null 2>&1; then
    die "curl is required"
  fi
}

ensure_install_dir_on_path() {
  if echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    return
  fi
  warn "${INSTALL_DIR} is not in your PATH."
  echo ""
  echo "Add it with:"
  echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
}

get_latest_version() {
  if [[ -n "$VERSION" ]]; then
    echo "$VERSION"
    return
  fi
  local version
  version=$(curl --proto "=https" --tlsv1.2 -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null |
    grep '"tag_name"' |
    sed 's/.*"tag_name": *"\(v[^"]*\)".*/\1/' || true)
  if [[ -n "$version" ]]; then
    echo "$version"
  else
    echo "main"
  fi
}

run_go_installer() {
  local script_path=""
  local ref="main"
  if [[ -n "$VERSION" ]]; then
    ref="$VERSION"
  fi
  if [[ -n "$LOCAL_SRC" ]] && [[ -f "$LOCAL_SRC/scripts/install-go.sh" ]]; then
    script_path="$LOCAL_SRC/scripts/install-go.sh"
  elif [[ "${BASH_SOURCE[0]:-}" != "" ]] && [[ -f "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-go.sh" ]]; then
    script_path="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-go.sh"
  fi

  if [[ -n "$script_path" ]]; then
    DOT_AGENTS_INSTALL_DIR="$INSTALL_DIR" DOT_AGENTS_VERSION="$VERSION" bash "$script_path"
    return
  fi

  local tmp
  tmp=$(mktemp)
  trap 'rm -f "$tmp"' RETURN
  info "Fetching Go installer..."
  curl --proto "=https" --tlsv1.2 -fsSL "https://raw.githubusercontent.com/${REPO}/${ref}/scripts/install-go.sh" -o "$tmp"
  DOT_AGENTS_INSTALL_DIR="$INSTALL_DIR" DOT_AGENTS_VERSION="$VERSION" bash "$tmp"
}

require_node() {
  command -v node >/dev/null 2>&1 || die "node is required for --port ts"
  local major
  major=$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo "0")
  if [[ "$major" -lt 20 ]]; then
    die "Node.js 20+ is required for --port ts"
  fi
}

build_ts_dist_if_needed() {
  local ts_root="$1"
  if [[ -f "$ts_root/dist/cli.js" ]]; then
    info "Using prebuilt TypeScript dist/"
    return
  fi
  command -v npm >/dev/null 2>&1 || die "npm is required to build the TypeScript port"
  info "Building TypeScript port..."
  (
    cd "$ts_root"
    if [[ -f package-lock.json ]]; then
      npm ci
    else
      npm install
    fi
    npm run build
  )
}

install_ts_target() {
  require_node
  local version repo_root ts_root tmpdir=""
  version=$(get_latest_version)
  info "Installing TypeScript port target (da-ts) from ${version}..."
  if [[ -n "$LOCAL_SRC" ]]; then
    [[ -d "$LOCAL_SRC/ports/typescript" ]] || die "DOT_AGENTS_LOCAL_SRC must point at a repo checkout with ports/typescript"
    repo_root="$LOCAL_SRC"
  else
    local url
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' RETURN
    if [[ "$version" = "main" ]]; then
      url="https://github.com/${REPO}/archive/refs/heads/main.tar.gz"
    else
      url="https://github.com/${REPO}/archive/refs/tags/${version}.tar.gz"
    fi
    info "Downloading source bundle ${version}..."
    curl --proto "=https" --tlsv1.2 -fsSL "$url" -o "$tmpdir/dot-agents.tar.gz"
    tar -xzf "$tmpdir/dot-agents.tar.gz" -C "$tmpdir"
    repo_root=$(find "$tmpdir" -maxdepth 1 -type d -name 'dot-agents*' | head -1)
    [[ -n "$repo_root" ]] || die "Could not resolve extracted source bundle"
  fi
  ts_root="$repo_root/ports/typescript"
  [[ -d "$ts_root" ]] || die "TypeScript port directory not found in source bundle"

  build_ts_dist_if_needed "$ts_root"

  mkdir -p "$INSTALL_DIR" "$TS_LIB_DIR"
  rm -rf "$TS_LIB_DIR/dist"
  cp -R "$ts_root/dist" "$TS_LIB_DIR/"

  cat > "$INSTALL_DIR/da-ts" <<EOF
#!/bin/sh
exec node "$TS_LIB_DIR/dist/cli.js" "\$@"
EOF
  chmod +x "$INSTALL_DIR/da-ts"

  success "Installed da-ts to ${INSTALL_DIR}/da-ts"
  ensure_install_dir_on_path
  echo ""
  echo "Run: da-ts --help"
  echo "Initialize: da-ts init"
}

main() {
  parse_args "$@"
  check_requirements

  echo ""
  echo -e "${BOLD}da installer${NC}"
  echo "─────────────────────────────────────"
  echo ""
  info "Selected target: ${PORT}"

  case "$PORT" in
    go) run_go_installer ;;
    ts) install_ts_target ;;
    *) die "Unsupported port: $PORT" ;;
  esac
}

main "$@"
