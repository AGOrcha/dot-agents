#!/bin/sh

set -eu

payload="$(cat || true)"
file_path="$(printf '%s' "$payload" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
if [ -z "$file_path" ]; then
  file_path="$(printf '%s' "$payload" | sed -n 's/.*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
fi

[ -n "$file_path" ] || exit 0
[ -f "$file_path" ] || exit 0

case "$file_path" in
  *.md|*.mdc|*.env.example|*_test.*)
    exit 0
    ;;
esac

if grep -E 'sk-ant-api|AKIA[0-9A-Z]{16}|gh[pso]_|sk_live_|sk_test_|sk-[a-zA-Z0-9]{20,}' "$file_path" 2>/dev/null | \
  grep -Evi 'YOUR_KEY|REPLACE_ME|example|xxxx|test_' >/dev/null 2>&1; then
  printf 'secret-scan warning: likely secret detected in %s\n' "$file_path" >&2
fi

exit 0
