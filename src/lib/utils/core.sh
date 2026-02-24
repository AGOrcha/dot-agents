#!/bin/bash
# dot-agents/lib/utils/core.sh
# Core utilities - source this file to get all utilities
# Usage: source "$LIB_DIR/utils/core.sh"

# Prevent double-sourcing
if [ -n "${_DOT_AGENTS_CORE_LOADED:-}" ]; then
  return 0
fi
_DOT_AGENTS_CORE_LOADED=1

# Determine library directory from this file's location
UTILS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB_DIR="$(dirname "$UTILS_DIR")"
SRC_DIR="$(dirname "$LIB_DIR")"

# Source all utility files
source "$UTILS_DIR/colors.sh"
source "$UTILS_DIR/logging.sh"
source "$UTILS_DIR/paths.sh"
source "$UTILS_DIR/symlink.sh"
source "$UTILS_DIR/json.sh"
source "$UTILS_DIR/interactive.sh"
source "$UTILS_DIR/install-detect.sh"

# Source platform modules (if they exist)
PLATFORMS_DIR="$LIB_DIR/platforms"
if [ -d "$PLATFORMS_DIR" ]; then
  [ -f "$PLATFORMS_DIR/cursor.sh" ] && source "$PLATFORMS_DIR/cursor.sh"
  [ -f "$PLATFORMS_DIR/claude-code.sh" ] && source "$PLATFORMS_DIR/claude-code.sh"
  [ -f "$PLATFORMS_DIR/codex.sh" ] && source "$PLATFORMS_DIR/codex.sh"
  [ -f "$PLATFORMS_DIR/opencode.sh" ] && source "$PLATFORMS_DIR/opencode.sh"
  [ -f "$PLATFORMS_DIR/github-copilot.sh" ] && source "$PLATFORMS_DIR/github-copilot.sh"
fi

# Global flags (can be overridden by CLI)
DRY_RUN="${DRY_RUN:-false}"
FORCE="${FORCE:-false}"
VERBOSE="${VERBOSE:-false}"
JSON_OUTPUT="${JSON_OUTPUT:-false}"
YES="${YES:-false}"           # Auto-confirm prompts
INTERACTIVE="${INTERACTIVE:-false}"  # Force interactive mode

# Version info - read from VERSION file if available
# Check multiple locations based on install method:
#   1. Homebrew: VERSION is at $SRC_DIR/VERSION (libexec/VERSION)
#   2. Development: VERSION is at repo root (parent of src/)
#   3. curl install: VERSION is at $SHARE_DIR/VERSION
if [ -f "$SRC_DIR/VERSION" ]; then
  DOT_AGENTS_VERSION="$(cat "$SRC_DIR/VERSION" | tr -d '[:space:]')"
elif [ -f "$(dirname "$SRC_DIR")/VERSION" ]; then
  DOT_AGENTS_VERSION="$(cat "$(dirname "$SRC_DIR")/VERSION" | tr -d '[:space:]')"
elif [ -f "${SHARE_DIR:-}/VERSION" ]; then
  DOT_AGENTS_VERSION="$(cat "$SHARE_DIR/VERSION" | tr -d '[:space:]')"
else
  DOT_AGENTS_VERSION="0.1.0"  # Hardcoded fallback
fi
DOT_AGENTS_VERSION_DATE="$(date +%Y-%m-%d)"

# Warn if multiple installations detected (silent in CI/non-interactive)
if [ -t 1 ] && [ -z "${CI:-}" ] && [ -z "${_DOT_AGENTS_INSTALL_CHECK_DONE:-}" ]; then
  _DOT_AGENTS_INSTALL_CHECK_DONE=1
  if has_install_conflict; then
    echo -e "${YELLOW}Warning: Multiple dot-agents installations detected. Run 'dot-agents doctor' for details.${NC}" >&2
  fi
fi

# Export for subshells
export DRY_RUN FORCE VERBOSE JSON_OUTPUT YES INTERACTIVE
export DOT_AGENTS_VERSION DOT_AGENTS_VERSION_DATE
export UTILS_DIR LIB_DIR SRC_DIR

# Parse common flags from arguments
# Usage: parse_common_flags "$@"
# Sets: DRY_RUN, FORCE, VERBOSE, JSON_OUTPUT, YES, INTERACTIVE
# Returns remaining args in REMAINING_ARGS array
parse_common_flags() {
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --dry-run)
        DRY_RUN=true
        shift
        ;;
      --force|-f)
        FORCE=true
        shift
        ;;
      --verbose|-v)
        VERBOSE=true
        shift
        ;;
      --json)
        JSON_OUTPUT=true
        shift
        ;;
      --yes|-y)
        YES=true
        shift
        ;;
      --interactive|-i)
        INTERACTIVE=true
        shift
        ;;
      --help|-h)
        SHOW_HELP=true
        shift
        ;;
      *)
        REMAINING_ARGS+=("$1")
        shift
        ;;
    esac
  done
}

# Show mode banner (for commands that modify things)
show_mode_banner() {
  if [ "$DRY_RUN" = true ]; then
    log_warn "DRY RUN MODE - no changes will be made"
    echo ""
  elif [ "$FORCE" = true ]; then
    log_warn "FORCE MODE - will overwrite existing files"
    echo ""
  fi
}

# Check if a command exists
command_exists() {
  command -v "$1" &>/dev/null
}

# Require a command to exist
require_command() {
  local cmd="$1"
  local install_hint="${2:-}"
  if ! command_exists "$cmd"; then
    die "Required command '$cmd' not found${install_hint:+. Install: $install_hint}"
  fi
}

# Check if running in CI
is_ci() {
  [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ] || [ -n "${CIRCLECI:-}" ]
}

# Get OS type
get_os() {
  case "$(uname -s)" in
    Darwin*) echo "macos" ;;
    Linux*)  echo "linux" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *)       echo "unknown" ;;
  esac
}

# Safe read file contents
# Usage: read_file "/path/to/file" -> contents or empty
read_file() {
  local file="$1"
  if [ -f "$file" ]; then
    cat "$file"
  fi
}

# Check if yq is available (for JSON operations)
# Note: Full JSON utilities are in json.sh
has_yq() {
  command_exists yq
}

# Confirm action with user
# Usage: confirm "Delete all files?" && rm -rf *
confirm() {
  local prompt="${1:-Continue?}"
  if [ "$FORCE" = true ]; then
    return 0
  fi
  echo -n -e "${YELLOW}$prompt [y/N]: ${NC}"
  read -r response
  case "$response" in
    [yY][eE][sS]|[yY]) return 0 ;;
    *) return 1 ;;
  esac
}

export -f command_exists require_command is_ci get_os read_file has_yq json_get confirm
