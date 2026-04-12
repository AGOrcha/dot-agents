#!/bin/sh
# Run kg change brief only when Claude Code is about to execute a git commit via Bash.
set -eu
payload="$(cat || true)"
cmd="$(printf '%s' "$payload" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
case "$cmd" in
*git*commit*) exec dot-agents kg changes --brief ;;
*) exit 0 ;;
esac
