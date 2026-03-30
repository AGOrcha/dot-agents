#!/bin/bash
# dot-agents installer
# https://github.com/NikashPrakash/dot-agents
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/dot-agents/dot-agents/master/scripts/install.sh | bash
#
# Options (via environment variables):
#   DOT_AGENTS_INSTALL_DIR  - Installation directory (default: ~/.local/bin)
#   DOT_AGENTS_NO_MODIFY_PATH - Set to 1 to skip PATH modification
#   DOT_AGENTS_VERSION - Specific version to install (default: latest)

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Configuration
REPO="dot-agents/dot-agents"
INSTALL_DIR="${DOT_AGENTS_INSTALL_DIR:-$HOME/.local/bin}"
LIB_DIR="${DOT_AGENTS_LIB_DIR:-$HOME/.local/lib/dot-agents}"
SHARE_DIR="${DOT_AGENTS_SHARE_DIR:-$HOME/.local/share/dot-agents}"
LOCAL_SRC="${DOT_AGENTS_LOCAL_SRC:-}"  # Set to local src/ directory for testing

# Logging functions
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }
die() { error "$1"; exit 1; }

# Check for existing installations that might conflict
check_existing_installs() {
  local homebrew_bin=""

  # Check Homebrew locations
  if [ -x "/opt/homebrew/bin/dot-agents" ]; then
    homebrew_bin="/opt/homebrew/bin/dot-agents"
  elif [ -x "/usr/local/bin/dot-agents" ]; then
    homebrew_bin="/usr/local/bin/dot-agents"
  fi

  if [ -n "$homebrew_bin" ]; then
    local hb_version
    hb_version=$("$homebrew_bin" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || echo "unknown")

    echo ""
    warn "Homebrew installation detected!"
    echo ""
    echo "  Found: $homebrew_bin (v$hb_version)"
    echo ""
    echo "  Installing via curl will create a second installation."
    echo "  This can cause version confusion and PATH conflicts."
    echo ""
    echo "  Recommended: Use Homebrew for updates:"
    echo "    brew upgrade dot-agents"
    echo ""

    if [ "${DOT_AGENTS_FORCE_INSTALL:-}" != "1" ]; then
      echo -n "Continue with curl install anyway? [y/N]: "
      read -r response < /dev/tty
      case "$response" in
        [yY][eE][sS]|[yY]) ;;
        *)
          info "Aborted. Use 'brew upgrade dot-agents' instead."
          exit 0
          ;;
      esac
      echo ""
    fi
  fi

  # Check for existing curl install
  if [ -x "$HOME/.local/bin/dot-agents" ]; then
    local curl_version
    curl_version=$("$HOME/.local/bin/dot-agents" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || echo "unknown")
    info "Existing curl installation found (v$curl_version) - will be upgraded"
  fi
}

# Header
echo ""
echo -e "${BOLD}dot-agents installer${NC}"
echo "─────────────────────────────────────"
echo ""

# Check for required commands
check_requirements() {
  local missing=()

  for cmd in curl tar; do
    if ! command -v "$cmd" &>/dev/null; then
      missing+=("$cmd")
    fi
  done

  if [ ${#missing[@]} -gt 0 ]; then
    die "Missing required commands: ${missing[*]}"
  fi
}

# Detect OS
detect_os() {
  case "$(uname -s)" in
    Darwin*) echo "macos" ;;
    Linux*)  echo "linux" ;;
    *)       die "Unsupported operating system: $(uname -s)" ;;
  esac
}

# Detect architecture
detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "x86_64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)             die "Unsupported architecture: $(uname -m)" ;;
  esac
}

# Get latest version from GitHub
get_latest_version() {
  if [ -n "${DOT_AGENTS_VERSION:-}" ]; then
    echo "$DOT_AGENTS_VERSION"
    return
  fi

  local version
  version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
    grep '"tag_name":' |
    sed -E 's/.*"([^"]+)".*/\1/' || echo "")

  if [ -z "$version" ]; then
    # Fallback to master branch if no releases yet
    echo "master"
  else
    echo "$version"
  fi
}

# Download and install
install_dot_agents() {
  local version="$1"
  local src_dir=""

  # Check for local source (for testing)
  if [ -n "$LOCAL_SRC" ]; then
    info "Installing from local source: $LOCAL_SRC"
    if [ ! -d "$LOCAL_SRC/lib" ] || [ ! -d "$LOCAL_SRC/bin" ]; then
      die "Invalid local source: missing lib/ or bin/ directory"
    fi
    src_dir="$LOCAL_SRC"
  else
    # Download from GitHub
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    info "Installing dot-agents ${version}..."

    local download_url
    if [ "$version" = "master" ]; then
      download_url="https://github.com/$REPO/archive/refs/heads/master.tar.gz"
    else
      download_url="https://github.com/$REPO/archive/refs/tags/${version}.tar.gz"
    fi

    info "Downloading from $download_url"
    curl -fsSL "$download_url" -o "$tmp_dir/dot-agents.tar.gz" || die "Download failed"

    info "Extracting..."
    tar -xzf "$tmp_dir/dot-agents.tar.gz" -C "$tmp_dir" || die "Extraction failed"

    local extracted_dir
    extracted_dir=$(find "$tmp_dir" -maxdepth 1 -type d -name "dot-agents*" | head -1)

    if [ -z "$extracted_dir" ] || [ ! -d "$extracted_dir/src" ]; then
      die "Invalid archive structure"
    fi
    src_dir="$extracted_dir/src"
  fi

  # Create installation directories
  info "Installing to $INSTALL_DIR"
  mkdir -p "$INSTALL_DIR"
  mkdir -p "$LIB_DIR"
  mkdir -p "$SHARE_DIR"

  # Copy files
  cp -r "$src_dir/lib/"* "$LIB_DIR/"
  cp -r "$src_dir/share/"* "$SHARE_DIR/"

  # Copy VERSION file (located in repo root, parent of src_dir)
  local repo_root="$(dirname "$src_dir")"
  if [ -f "$repo_root/VERSION" ]; then
    cp "$repo_root/VERSION" "$SHARE_DIR/VERSION"
  fi

  # Install main script with updated paths
  local script_content
  script_content=$(cat "$src_dir/bin/dot-agents")

  # Rewrite paths for installed location
  script_content=$(echo "$script_content" | sed "s|SRC_DIR=\"\$(dirname \"\$BIN_DIR\")\"|SRC_DIR=\"$HOME/.local\"|")
  script_content=$(echo "$script_content" | sed "s|LIB_DIR=\"\$SRC_DIR/lib\"|LIB_DIR=\"$LIB_DIR\"|")
  script_content=$(echo "$script_content" | sed "s|SHARE_DIR=\"\$SRC_DIR/share\"|SHARE_DIR=\"$SHARE_DIR\"|")

  echo "$script_content" > "$INSTALL_DIR/dot-agents"
  chmod +x "$INSTALL_DIR/dot-agents"

  success "Installed dot-agents to $INSTALL_DIR/dot-agents"
}

# Add to PATH if needed
setup_path() {
  if [ "${DOT_AGENTS_NO_MODIFY_PATH:-}" = "1" ]; then
    return
  fi

  # Check if already in PATH
  if echo "$PATH" | tr ':' '\n' | grep -q "^$INSTALL_DIR$"; then
    return
  fi

  local shell_name
  shell_name=$(basename "$SHELL")
  local rc_file=""

  case "$shell_name" in
    bash)
      if [ -f "$HOME/.bash_profile" ]; then
        rc_file="$HOME/.bash_profile"
      else
        rc_file="$HOME/.bashrc"
      fi
      ;;
    zsh)
      rc_file="$HOME/.zshrc"
      ;;
    fish)
      rc_file="$HOME/.config/fish/config.fish"
      ;;
    *)
      warn "Unknown shell: $shell_name. Add $INSTALL_DIR to your PATH manually."
      return
      ;;
  esac

  if [ -n "$rc_file" ]; then
    local path_line="export PATH=\"$INSTALL_DIR:\$PATH\""

    if [ "$shell_name" = "fish" ]; then
      path_line="set -gx PATH $INSTALL_DIR \$PATH"
    fi

    # Check if already added
    if grep -q "dot-agents" "$rc_file" 2>/dev/null; then
      return
    fi

    echo "" >> "$rc_file"
    echo "# dot-agents" >> "$rc_file"
    echo "$path_line" >> "$rc_file"

    info "Added $INSTALL_DIR to PATH in $rc_file"
    warn "Restart your shell or run: source $rc_file"
  fi
}

# Verify installation
verify_installation() {
  if [ -x "$INSTALL_DIR/dot-agents" ]; then
    success "Installation verified"
    return 0
  else
    error "Installation verification failed"
    return 1
  fi
}

# Main
main() {
  check_requirements
  check_existing_installs

  local os arch version
  os=$(detect_os)
  arch=$(detect_arch)
  version=$(get_latest_version)

  info "Detected: $os ($arch)"
  info "Version: $version"
  echo ""

  install_dot_agents "$version"
  setup_path
  verify_installation

  echo ""
  echo "─────────────────────────────────────"
  success "dot-agents installed successfully!"
  echo ""
  echo "Next steps:"
  echo "  1. Restart your shell (or run: source ~/.zshrc)"
  echo "  2. Run: dot-agents init"
  echo ""
  echo "For help: dot-agents --help"
  echo ""
}

main "$@"
