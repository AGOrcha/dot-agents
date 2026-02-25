#!/bin/bash
# dot-agents/lib/utils/paths.sh
# Path manipulation utilities

# Expand tilde and resolve to absolute path
# Usage: expand_path "~/foo" -> /Users/user/foo
expand_path() {
  local path="$1"
  # Expand tilde
  path="${path/#\~/$HOME}"
  # Convert to absolute if relative
  if [[ "$path" != /* ]]; then
    path="$PWD/$path"
  fi
  # Normalize (remove .., ., double slashes)
  # Use realpath if available, otherwise a basic cleanup
  if command -v realpath &>/dev/null; then
    realpath -m "$path" 2>/dev/null || echo "$path"
  else
    echo "$path" | sed -e 's|/\+|/|g' -e 's|/\.$||'
  fi
}

# Get the canonical path (resolves symlinks)
# Usage: canonical_path "/path/to/symlink" -> /real/path
canonical_path() {
  local path="$1"
  path=$(expand_path "$path")
  if command -v realpath &>/dev/null; then
    realpath "$path" 2>/dev/null || echo "$path"
  elif command -v readlink &>/dev/null; then
    readlink -f "$path" 2>/dev/null || echo "$path"
  else
    echo "$path"
  fi
}

# Get relative path from one path to another
# Usage: relative_path "/a/b/c" "/a/b" -> "c"
relative_path() {
  local target="$1"
  local base="$2"

  if command -v realpath &>/dev/null; then
    realpath --relative-to="$base" "$target" 2>/dev/null || echo "$target"
  else
    # Fallback: just return target
    echo "$target"
  fi
}

# Validate that a path exists
# Usage: path_exists "/path/to/check" && echo "exists"
path_exists() {
  [ -e "$1" ]
}

# Check if path is a directory
is_directory() {
  [ -d "$1" ]
}

# Check if path is a file
is_file() {
  [ -f "$1" ]
}

# Check if path is a symlink
is_symlink() {
  [ -L "$1" ]
}

# Get the parent directory
# Usage: parent_dir "/a/b/c" -> "/a/b"
parent_dir() {
  dirname "$1"
}

# Get the filename from a path
# Usage: filename "/a/b/c.txt" -> "c.txt"
filename() {
  basename "$1"
}

# Get filename without extension
# Usage: stem "/a/b/c.txt" -> "c"
stem() {
  local name
  name=$(basename "$1")
  echo "${name%.*}"
}

# Get file extension
# Usage: extension "/a/b/c.txt" -> "txt"
extension() {
  local name
  name=$(basename "$1")
  if [[ "$name" == *.* ]]; then
    echo "${name##*.}"
  fi
}

# Ensure a directory exists
# Usage: ensure_dir "/path/to/dir"
ensure_dir() {
  local dir="$1"
  if [ ! -d "$dir" ]; then
    mkdir -p "$dir"
  fi
}

# Find git root from current or specified directory
# Usage: git_root [path] -> /path/to/git/root or ""
git_root() {
  local start="${1:-.}"
  local dir
  dir=$(expand_path "$start")

  while [ "$dir" != "/" ]; do
    if [ -d "$dir/.git" ]; then
      echo "$dir"
      return 0
    fi
    dir=$(dirname "$dir")
  done
  return 1
}

# Dot-agents installation repo root (for git commit tracking)
# Usage: dot_agents_repo_root -> /path/to/dot-agents/repo or ""
dot_agents_repo_root() {
  local start="${SRC_DIR:-}"
  [ -z "$start" ] && return 1
  git_root "$start" 2>/dev/null || echo "$(dirname "$start")"
}

# Windows mirror context for WSL-managed repos under /mnt/c/Users/<user>/...
DOT_AGENTS_WINDOWS_MIRROR="${DOT_AGENTS_WINDOWS_MIRROR:-false}"
DOT_AGENTS_WINDOWS_HOME="${DOT_AGENTS_WINDOWS_HOME:-}"

# Set Windows mirror context from a repo path.
# Enables user-level config mirroring to /mnt/c/Users/<user> when applicable.
dot_agents_set_windows_mirror_context() {
  local repo_path="$1"
  DOT_AGENTS_WINDOWS_MIRROR=false
  DOT_AGENTS_WINDOWS_HOME=""

  if [[ "$repo_path" =~ ^/mnt/c/Users/([^/]+)(/|$) ]]; then
    DOT_AGENTS_WINDOWS_MIRROR=true
    DOT_AGENTS_WINDOWS_HOME="/mnt/c/Users/${BASH_REMATCH[1]}"
  fi

  export DOT_AGENTS_WINDOWS_MIRROR DOT_AGENTS_WINDOWS_HOME
}

# Print applicable user home roots, one per line.
# Always includes Linux $HOME, and includes Windows mirror home when enabled.
dot_agents_user_home_roots() {
  echo "$HOME"
  if [ "${DOT_AGENTS_WINDOWS_MIRROR:-false}" = true ] && [ -n "${DOT_AGENTS_WINDOWS_HOME:-}" ] && [ "${DOT_AGENTS_WINDOWS_HOME}" != "$HOME" ]; then
    echo "$DOT_AGENTS_WINDOWS_HOME"
  fi
}

# Standard paths for dot-agents
AGENTS_HOME="${AGENTS_HOME:-$HOME/.agents}"
AGENTS_STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/dot-agents"
AGENTS_CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/dot-agents"

export AGENTS_HOME AGENTS_STATE_DIR AGENTS_CACHE_DIR
