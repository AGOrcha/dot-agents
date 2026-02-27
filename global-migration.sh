#!/usr/bin/env bash
set -euo pipefail

# One-time migration helper:
# Ensure all legacy user skills in ~/.agents/skills/global/*
# are discoverable at ~/.agents/skills/* (Codex/Copilot user scope).

APPLY=false
if [[ "${1:-}" == "--apply" ]]; then
  APPLY=true
fi

AGENTS_HOME="${AGENTS_HOME:-$HOME/.agents}"
SKILLS_ROOT="$AGENTS_HOME/skills"
LEGACY_GLOBAL="$SKILLS_ROOT/global"

echo "AGENTS_HOME: $AGENTS_HOME"
echo "SKILLS_ROOT: $SKILLS_ROOT"
echo "LEGACY_GLOBAL: $LEGACY_GLOBAL"
echo

if [[ ! -d "$LEGACY_GLOBAL" ]]; then
  echo "No legacy global folder found at: $LEGACY_GLOBAL"
  exit 0
fi

mkdir -p "$SKILLS_ROOT"

created=0
already=0
conflicts=0
invalid=0

shopt -s nullglob
for skill_dir in "$LEGACY_GLOBAL"/*/; do
  [[ -d "$skill_dir" ]] || continue
  [[ -f "$skill_dir/SKILL.md" ]] || { ((invalid++)); continue; }

  name="$(basename "$skill_dir")"
  target="$SKILLS_ROOT/$name"

  if [[ -L "$target" ]]; then
    current="$(readlink "$target" || true)"
    if [[ "$current" == "$skill_dir" || "$current" == "${skill_dir%/}" ]]; then
      echo "OK     $name (already linked)"
      ((already++))
    else
      echo "WARN   $name (symlink exists but points elsewhere: $current)"
      ((conflicts++))
    fi
    continue
  fi

  if [[ -e "$target" ]]; then
    echo "WARN   $name (path exists and is not a symlink: $target)"
    ((conflicts++))
    continue
  fi

  if $APPLY; then
    ln -s "$skill_dir" "$target"
    echo "LINK   $name -> $skill_dir"
  else
    echo "PLAN   ln -s \"$skill_dir\" \"$target\""
  fi
  ((created++))
done
shopt -u nullglob

echo
echo "Summary:"
echo "  planned/created: $created"
echo "  already-linked:  $already"
echo "  conflicts:       $conflicts"
echo "  invalid-skill:   $invalid"

if ! $APPLY; then
  echo
  echo "Dry-run only. Re-run with --apply to perform changes."
fi