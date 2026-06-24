#!/usr/bin/env bash
# find-claude-session.sh — locate and tail the most recent Claude Code session
# transcript for a given repo path. Cross-tool resume helper: lets a secondary
# tool pick up where a primary-agent session left off without an unbounded
# `find ~/.claude`.
#
# Claude Code stores transcripts under
#   ~/.claude/projects/<encoded-path>/<session-id>.jsonl
# where <encoded-path> is the absolute repo path with every "/" and "." replaced
# by "-". This script computes that key, globs the directory, and tails the
# most-recently-modified transcript.
#
# IMPORTANT: this "/" and "." -> "-" encoding MUST stay in sync with
# scripts/migrate-project.sh (the repo-move script), which renames this same
# session directory when the repo moves. If one changes the encoding, change
# both.
#
# Usage:
#   scripts/find-claude-session.sh [repo-path] [--lines N] [--path-only]
#
# Arguments:
#   repo-path     Absolute repo path (default: current working directory).
#   --lines N     Number of trailing lines to print (default 40).
#   --path-only   Print the resolved transcript path and exit (no tail).
#
# Exit status:
#   0  found and tailed (or printed) a transcript.
#   3  no session directory or no transcript found (degrades cleanly).

set -euo pipefail

usage() {
  sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-2}"
}

REPO_PATH=""
LINES=40
PATH_ONLY=0

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage 0 ;;
    --lines)
      [ $# -ge 2 ] || { echo "error: --lines needs a value" >&2; usage 2; }
      LINES="$2"; shift 2 ;;
    --path-only) PATH_ONLY=1; shift ;;
    --) shift; break ;;
    -*) echo "error: unknown option: $1" >&2; usage 2 ;;
    *)
      if [ -z "$REPO_PATH" ]; then REPO_PATH="$1"; shift
      else echo "error: unexpected argument: $1" >&2; usage 2; fi ;;
  esac
done

REPO_PATH="${REPO_PATH:-$PWD}"

# Normalize to an absolute path (the encoding is computed from the absolute
# path, matching how Claude Code records it).
if [ -d "$REPO_PATH" ]; then
  REPO_PATH="$(cd "$REPO_PATH" && pwd)"
fi

# Encode: replace every "/" and "." with "-". MUST match migrate-project.sh.
encoded="$(printf '%s' "$REPO_PATH" | tr '/.' '--')"

projects_root="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/projects"
session_dir="${projects_root}/${encoded}"

if [ ! -d "$session_dir" ]; then
  echo "No Claude session directory for ${REPO_PATH}" >&2
  echo "  expected: ${session_dir}" >&2
  exit 3
fi

# Most-recently-modified .jsonl transcript in that directory. Avoid `ls`
# parsing: collect matches, sort by mtime via stat (portable on macOS + Linux).
latest=""
latest_mtime=0
for f in "$session_dir"/*.jsonl; do
  [ -e "$f" ] || continue
  # macOS: stat -f %m ; GNU: stat -c %Y. Try both.
  m="$(stat -f %m "$f" 2>/dev/null || stat -c %Y "$f" 2>/dev/null || echo 0)"
  if [ "$m" -gt "$latest_mtime" ]; then
    latest_mtime="$m"
    latest="$f"
  fi
done

if [ -z "$latest" ]; then
  echo "No .jsonl transcript in ${session_dir}" >&2
  exit 3
fi

if [ "$PATH_ONLY" -eq 1 ]; then
  printf '%s\n' "$latest"
  exit 0
fi

echo "Tailing ${latest} (last ${LINES} lines):" >&2
tail -n "$LINES" "$latest"
