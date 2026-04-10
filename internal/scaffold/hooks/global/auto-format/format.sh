#!/bin/sh

set -eu

payload="$(cat || true)"
file_path="$(printf '%s' "$payload" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
if [ -z "$file_path" ]; then
  file_path="$(printf '%s' "$payload" | sed -n 's/.*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
fi

[ -n "$file_path" ] || exit 0
[ -f "$file_path" ] || exit 0

run_if_available() {
  tool="$1"
  shift
  if command -v "$tool" >/dev/null 2>&1; then
    "$@" >/dev/null 2>&1 || true
    return 0
  fi
  return 1
}

case "$file_path" in
  *.go)
    run_if_available gofmt gofmt -w "$file_path"
    ;;
  *.py)
    run_if_available ruff ruff format --quiet "$file_path" || run_if_available black black --quiet "$file_path"
    ;;
  *.ts|*.tsx|*.js|*.jsx|*.css|*.scss|*.json|*.yaml|*.yml)
    if command -v npx >/dev/null 2>&1; then
      npx prettier --write "$file_path" >/dev/null 2>&1 || true
    fi
    ;;
  *.rs)
    run_if_available rustfmt rustfmt "$file_path"
    ;;
esac

exit 0
