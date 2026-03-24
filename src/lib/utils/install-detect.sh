#!/bin/bash
# dot-agents/lib/utils/install-detect.sh
# Utilities for detecting multiple dot-agents installations

# Detect all dot-agents installations
# Returns space-separated list of "method:path" entries
detect_installations() {
  local installs=()

  # Check curl install location
  if [ -x "$HOME/.local/bin/dot-agents" ]; then
    installs+=("curl:$HOME/.local/bin/dot-agents")
  fi

  # Check Homebrew locations (macOS Apple Silicon)
  if [ -x "/opt/homebrew/bin/dot-agents" ]; then
    installs+=("homebrew:/opt/homebrew/bin/dot-agents")
  # Check Homebrew locations (macOS Intel)
  elif [ -x "/usr/local/bin/dot-agents" ]; then
    installs+=("homebrew:/usr/local/bin/dot-agents")
  fi

  # Check Linuxbrew
  if [ -x "$HOME/.linuxbrew/bin/dot-agents" ]; then
    installs+=("linuxbrew:$HOME/.linuxbrew/bin/dot-agents")
  fi

  echo "${installs[*]+"${installs[*]}"}"
}

# Check if there are multiple installations (conflict)
has_install_conflict() {
  local installs
  read -ra installs <<< "$(detect_installations)"
  [ ${#installs[@]} -gt 1 ]
}

# Get the currently active installation path
get_active_install() {
  command -v dot-agents 2>/dev/null
}

# Get installation method for a given path
get_install_method() {
  local path="$1"
  case "$path" in
    "$HOME/.local/bin/dot-agents") echo "curl" ;;
    /opt/homebrew/bin/dot-agents) echo "homebrew" ;;
    /usr/local/bin/dot-agents) echo "homebrew" ;;
    "$HOME/.linuxbrew/bin/dot-agents") echo "linuxbrew" ;;
    *) echo "unknown" ;;
  esac
}

export -f detect_installations has_install_conflict get_active_install get_install_method
