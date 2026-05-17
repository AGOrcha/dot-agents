#!/bin/sh

set -eu

payload="$(cat || true)"

matches() {
  needle="$1"
  printf '%s' "$payload" | grep -Fqi "$needle"
}

forbidden=""
for pattern in \
  'rm -rf /' \
  'rm -rf ~' \
  'git push --force origin main' \
  'git push --force origin master' \
  'DROP DATABASE' \
  'DROP TABLE' \
  'truncate' \
  ':(){ :|:& };:'; do
  if matches "$pattern"; then
    forbidden="$pattern"
    break
  fi
done

if [ -n "$forbidden" ]; then
  printf 'blocked by guard-commands: matched forbidden pattern: %s\n' "$forbidden" >&2
  exit 2
fi

exit 0
