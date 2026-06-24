#!/usr/bin/env bash
# migrate-project.sh — move a repo out of a TCC-protected folder (e.g. ~/Documents)
# into ~/code/<name>, PRESERVING Claude Code session history + memory and remapping
# Codex's session→cwd index, so neither tool "loses" its history after the move.
#
# Why: macOS TCC protects ~/Documents (and Desktop/Downloads/iCloud). A repo there
# forces FDA grants that are fragile (brew-cdhash grants die on upgrade; a detached
# tmux server is its own responsible process). A repo under ~/code is NOT protected,
# so the whole class of "Operation not permitted" disappears with no FDA babysitting.
#
# Claude keys storage by ABSOLUTE PATH:
#   transcripts+memory -> ~/.claude/projects/<abspath with / and . -> ->/
#   per-project config -> ~/.claude.json  projects["<abspath>"]
# Move the repo and those keys no longer match -> history "disappears" (orphaned,
# not deleted). This script re-keys them to the new path.
#
# Codex keys sessions by the cwd recorded INSIDE each rollout; ~/.codex/session_index.jsonl
# maps session->cwd. We rewrite the index cwd (cheap, drives the history picker); the
# per-event cwd inside rollout files is only rewritten with --rewrite-codex-sessions.
#
# Usage:
#   migrate-project.sh <repo-path> [dest-parent]   # dest-parent default: $HOME/code
#   migrate-project.sh ~/Documents/dot-agents             # -> ~/code/dot-agents
#   migrate-project.sh ~/Documents/payout ~/code          # -> ~/code/payout
# Flags:
#   --dry-run                  show every action, change nothing
#   --rewrite-codex-sessions   also rewrite cwd inside Codex rollout files (heavier)
#
# SAFE: backs up ~/.claude.json + the project's ~/.claude/projects dir + the codex
# index to a timestamped backup dir BEFORE touching anything; prompts before the move.
set -euo pipefail

DRY_RUN=0; REWRITE_CODEX_SESSIONS=0; ARGS=()
for a in "$@"; do
  case "$a" in
    --dry-run) DRY_RUN=1 ;;
    --rewrite-codex-sessions) REWRITE_CODEX_SESSIONS=1 ;;
    -*) echo "unknown flag: $a" >&2; exit 2 ;;
    *) ARGS+=("$a") ;;
  esac
done
[[ ${#ARGS[@]} -ge 1 ]] || { echo "usage: $0 <repo-path> [dest-parent] [--dry-run] [--rewrite-codex-sessions]" >&2; exit 2; }

command -v jq >/dev/null || { echo "error: jq is required (brew install jq)" >&2; exit 1; }

# Resolve absolute paths (without requiring the dest to exist yet).
abspath() { local p="$1"; python3 -c 'import os,sys; print(os.path.abspath(os.path.expanduser(sys.argv[1])))' "$p"; }
OLD_ABS="$(abspath "${ARGS[0]}")"
DEST_PARENT="$(abspath "${ARGS[1]:-$HOME/code}")"
NAME="$(basename "$OLD_ABS")"
NEW_ABS="$DEST_PARENT/$NAME"

# Claude's dir-encoding: replace '/' and '.' with '-'.
encode() { local s="$1"; printf '%s' "$s" | sed 's#[/.]#-#g'; }
CL_PROJECTS="$HOME/.claude/projects"
OLD_KEYDIR="$CL_PROJECTS/$(encode "$OLD_ABS")"
NEW_KEYDIR="$CL_PROJECTS/$(encode "$NEW_ABS")"
CLAUDE_JSON="$HOME/.claude.json"
CODEX_INDEX="$HOME/.codex/session_index.jsonl"
CODEX_SESSIONS="$HOME/.codex/sessions"

run() { if [[ $DRY_RUN -eq 1 ]]; then echo "DRY: $*"; else echo "+ $*"; eval "$*"; fi; }

echo "== migrate-project =="
echo "  repo:        $OLD_ABS  ->  $NEW_ABS"
echo "  claude dir:  $OLD_KEYDIR  ->  $NEW_KEYDIR"
echo "  claude.json: projects[\"$OLD_ABS\"] -> projects[\"$NEW_ABS\"]"
echo "  codex index: rewrite cwd $OLD_ABS -> $NEW_ABS (rollout files: $([[ $REWRITE_CODEX_SESSIONS -eq 1 ]] && echo yes || echo no))"
echo

# --- preflight ---
[[ -d "$OLD_ABS" ]] || { echo "error: repo not found: $OLD_ABS" >&2; exit 1; }
[[ -e "$NEW_ABS" ]] && { echo "error: destination already exists: $NEW_ABS (move/merge it manually first)" >&2; exit 1; }
if pgrep -fl tmux >/dev/null 2>&1; then echo "note: tmux is running — close panes in the OLD path before moving."; fi

if [[ $DRY_RUN -eq 0 ]]; then
  read -r -p "Proceed with the move + history re-key? [y/N] " ans
  [[ "$ans" == "y" || "$ans" == "Y" ]] || { echo "aborted."; exit 0; }
fi

# --- backup ---
TS="$(date +%Y%m%d-%H%M%S)"
BACKUP="$HOME/.migrate-project-backup/$NAME-$TS"
run "mkdir -p '$BACKUP'"
[[ -f "$CLAUDE_JSON" ]] && run "cp -p '$CLAUDE_JSON' '$BACKUP/claude.json.bak'"
[[ -d "$OLD_KEYDIR" ]] && run "cp -R '$OLD_KEYDIR' '$BACKUP/claude-projectdir.bak'"
[[ -f "$CODEX_INDEX" ]] && run "cp -p '$CODEX_INDEX' '$BACKUP/codex-session_index.jsonl.bak'"
echo "  backup -> $BACKUP"

# --- 1) move the repo ---
run "mkdir -p '$DEST_PARENT'"
run "mv '$OLD_ABS' '$NEW_ABS'"

# --- 2) Claude transcripts + memory: re-key the project dir ---
if [[ -d "$OLD_KEYDIR" ]]; then
  if [[ -e "$NEW_KEYDIR" ]]; then
    # target exists (e.g. a stub from an earlier attempt): merge contents in.
    run "cp -R '$OLD_KEYDIR/.' '$NEW_KEYDIR/'"
    run "rm -rf '$OLD_KEYDIR'"
  else
    run "mv '$OLD_KEYDIR' '$NEW_KEYDIR'"
  fi
else
  echo "  note: no Claude project dir at old key (nothing to move)."
fi

# --- 3) ~/.claude.json: rename/merge the projects key ---
if [[ -f "$CLAUDE_JSON" ]]; then
  if [[ $DRY_RUN -eq 1 ]]; then
    echo "DRY: jq rename projects[\"$OLD_ABS\"] -> projects[\"$NEW_ABS\"] in $CLAUDE_JSON"
  else
    tmp="$(mktemp)"
    jq --arg old "$OLD_ABS" --arg new "$NEW_ABS" '
      if (.projects[$old]) then
        .projects[$new] = (.projects[$new] // {}) * .projects[$old]
        | del(.projects[$old])
      else . end
    ' "$CLAUDE_JSON" > "$tmp" && mv "$tmp" "$CLAUDE_JSON"
    echo "+ re-keyed ~/.claude.json projects entry"
  fi
fi

# --- 4) Codex: rewrite cwd in the session index (drives history-by-cwd) ---
if [[ -f "$CODEX_INDEX" ]]; then
  if [[ $DRY_RUN -eq 1 ]]; then
    echo "DRY: jq rewrite .cwd/.payload.cwd $OLD_ABS -> $NEW_ABS in $CODEX_INDEX"
  else
    tmp="$(mktemp)"
    # rewrite any cwd field (top-level or nested payload.cwd) that equals OLD_ABS
    jq -c --arg old "$OLD_ABS" --arg new "$NEW_ABS" '
      if (.cwd? == $old) then .cwd = $new else . end
      | if (.payload?.cwd? == $old) then .payload.cwd = $new else . end
    ' "$CODEX_INDEX" > "$tmp" && mv "$tmp" "$CODEX_INDEX"
    echo "+ rewrote Codex session_index cwd"
  fi
fi

# --- 4b) optional: rewrite cwd inside the Codex rollout files themselves ---
if [[ $REWRITE_CODEX_SESSIONS -eq 1 && -d "$CODEX_SESSIONS" ]]; then
  echo "  scanning Codex rollouts referencing the old path..."
  # back up the whole sessions dir once, then rewrite only files that mention OLD_ABS
  run "cp -R '$CODEX_SESSIONS' '$BACKUP/codex-sessions.bak'"
  while IFS= read -r f; do
    if [[ $DRY_RUN -eq 1 ]]; then echo "DRY: sed cwd in $f";
    else
      # JSON-safe: only the exact OLD_ABS string is replaced with NEW_ABS
      python3 - "$f" "$OLD_ABS" "$NEW_ABS" <<'PY'
import sys
f,old,new=sys.argv[1],sys.argv[2],sys.argv[3]
s=open(f).read()
open(f,'w').write(s.replace(old,new))
PY
      echo "+ rewrote $f"
    fi
  done < <(grep -rl -- "$OLD_ABS" "$CODEX_SESSIONS" 2>/dev/null || true)
fi

echo
echo "== done =="
echo "  repo now at: $NEW_ABS"
echo "  next: open a NEW shell (fresh tmux: 'tmux kill-server' first), cd $NEW_ABS,"
echo "        and 'claude --resume' / 'codex resume' should list the prior sessions."
echo "  rollback: restore from $BACKUP and 'mv $NEW_ABS $OLD_ABS' if anything looks off."
