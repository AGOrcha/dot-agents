#!/bin/sh

set -u

agents_home="${AGENTS_HOME:-$HOME/.agents}"
project_dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"

project_name() {
  if [ -f "$project_dir/.agentsrc.json" ]; then
    name=$(sed -n 's/.*"project"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$project_dir/.agentsrc.json" | head -n 1)
    if [ -n "${name:-}" ]; then
      printf '%s\n' "$name"
      return
    fi
  fi
  basename "$project_dir"
}

print_fallback_orient() {
  project="$(project_name)"
  checkpoint_path="$agents_home/context/$project/checkpoint.yaml"
  session_proposals_dir="$agents_home/proposals"

  git_branch="unknown"
  git_sha="unknown"
  dirty_count="0"
  recent_commits=""
  warnings=""
  if git -C "$project_dir" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git_branch=$(git -C "$project_dir" rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'unknown')
    git_sha=$(git -C "$project_dir" rev-parse --short HEAD 2>/dev/null || printf 'unknown')
    dirty_count=$(git -C "$project_dir" status --short 2>/dev/null | wc -l | tr -d ' ')
    recent_commits=$(git -C "$project_dir" log --oneline -5 2>/dev/null || true)
  else
    warnings="${warnings}git repo not detected
"
  fi

  pending_proposals="0"
  if [ -d "$session_proposals_dir" ]; then
    pending_proposals=$(find "$session_proposals_dir" -maxdepth 1 -type f -name '*.yaml' 2>/dev/null | wc -l | tr -d ' ')
  fi

  next_action=""
  checkpoint_timestamp=""
  checkpoint_branch=""
  checkpoint_sha=""
  checkpoint_verification=""
  checkpoint_summary=""
  if [ -f "$checkpoint_path" ]; then
    checkpoint_timestamp=$(sed -n 's/^timestamp:[[:space:]]*"\{0,1\}\(.*[^"]\)"\{0,1\}[[:space:]]*$/\1/p' "$checkpoint_path" | head -n 1)
    checkpoint_branch=$(sed -n 's/^  branch:[[:space:]]*"\{0,1\}\(.*[^"]\)"\{0,1\}[[:space:]]*$/\1/p' "$checkpoint_path" | head -n 1)
    checkpoint_sha=$(sed -n 's/^  sha:[[:space:]]*"\{0,1\}\(.*[^"]\)"\{0,1\}[[:space:]]*$/\1/p' "$checkpoint_path" | head -n 1)
    checkpoint_verification=$(sed -n 's/^  status:[[:space:]]*"\{0,1\}\(.*[^"]\)"\{0,1\}[[:space:]]*$/\1/p' "$checkpoint_path" | head -n 1)
    checkpoint_summary=$(sed -n 's/^  summary:[[:space:]]*"\{0,1\}\(.*[^"]\)"\{0,1\}[[:space:]]*$/\1/p' "$checkpoint_path" | head -n 1)
    next_action=$(sed -n 's/^next_action:[[:space:]]*"\{0,1\}\(.*[^"]\)"\{0,1\}[[:space:]]*$/\1/p' "$checkpoint_path" | head -n 1)
  fi

  printf '# Project\n\n'
  printf -- '- name: %s\n' "$project"
  printf -- '- path: %s\n' "$project_dir"
  printf -- '- branch: %s\n' "$git_branch"
  printf -- '- sha: %s\n' "$git_sha"
  printf -- '- dirty files: %s\n\n' "$dirty_count"

  printf '# Active Plans\n\n'
  plans_found=0
  for plan in "$project_dir"/.agents/active/*.plan.md; do
    [ -f "$plan" ] || continue
    plans_found=1
    title=$(sed -n '1s/^# *//p' "$plan")
    [ -n "$title" ] || title=$(basename "$plan")
    printf '## %s\n' "$title"
    printf -- '- path: %s\n' "$plan"
    pending_items=$(grep '^- \[ \]' "$plan" 2>/dev/null | head -n 3 | sed 's/^- \[ \] /- /' || true)
    if [ -n "$pending_items" ]; then
      printf '%s\n' "$pending_items"
    else
      printf -- '- no pending items found\n'
    fi
    printf '\n'
  done
  if [ "$plans_found" -eq 0 ]; then
    printf -- '- none\n\n'
  fi

  printf '# Last Checkpoint\n\n'
  if [ -f "$checkpoint_path" ]; then
    printf -- '- timestamp: %s\n' "${checkpoint_timestamp:-unknown}"
    printf -- '- branch: %s\n' "${checkpoint_branch:-unknown}"
    printf -- '- sha: %s\n' "${checkpoint_sha:-unknown}"
    printf -- '- verification: %s\n' "${checkpoint_verification:-unknown}"
    if [ -n "${checkpoint_summary:-}" ]; then
      printf -- '- summary: %s\n' "$checkpoint_summary"
    fi
    printf -- '- next action: %s\n\n' "${next_action:-Review active plan}"
  else
    printf -- '- none\n\n'
  fi

  printf '# Pending Handoffs\n\n'
  handoffs_found=0
  for handoff in "$project_dir"/.agents/active/handoffs/*.md; do
    [ -f "$handoff" ] || continue
    handoffs_found=1
    title=$(sed -n '1s/^# *//p' "$handoff")
    [ -n "$title" ] || title=$(basename "$handoff")
    printf -- '- %s (%s)\n' "$title" "$handoff"
  done
  if [ "$handoffs_found" -eq 0 ]; then
    printf -- '- none\n'
  fi
  printf '\n'

  printf '# Recent Lessons\n\n'
  lessons_file=""
  if [ -f "$project_dir/.agents/lessons/index.md" ]; then
    lessons_file="$project_dir/.agents/lessons/index.md"
  elif [ -f "$project_dir/.agents/lessons.md" ]; then
    lessons_file="$project_dir/.agents/lessons.md"
  fi
  if [ -n "$lessons_file" ]; then
    tail -n 10 "$lessons_file" | sed '/^[[:space:]]*[-*][[:space:]]/!s/^/- /'
    printf '\n'
  else
    printf -- '- none\n\n'
    warnings="${warnings}lessons index not found
"
  fi

  printf '# Pending Proposals\n\n'
  printf -- '- count: %s\n\n' "$pending_proposals"

  printf '# Next Action\n\n'
  if [ -z "$next_action" ]; then
    next_action=$(grep '^- \[ \]' "$project_dir"/.agents/active/*.plan.md 2>/dev/null | head -n 1 | sed 's/^- \[ \] //' || true)
  fi
  if [ -z "$next_action" ]; then
    next_action="Review active plan"
  fi
  printf -- '- %s\n' "$next_action"

  if [ -n "$recent_commits" ]; then
    printf '\n# Recent Commits\n\n'
    printf '%s\n' "$recent_commits"
  fi

  if [ -n "$warnings" ]; then
    printf '\n# Warnings\n\n'
    printf '%s' "$warnings" | sed '/^$/d; s/^/- /'
  fi
}

main() {
  if command -v dot-agents >/dev/null 2>&1; then
    if (
      cd "$project_dir" &&
      dot-agents workflow orient
    ); then
      return 0
    fi
    printf 'session-orient warning: dot-agents workflow orient failed, using shell fallback\n' >&2
  fi
  print_fallback_orient
}

if ! main; then
  printf 'session-orient warning: hook failed unexpectedly\n' >&2
fi

exit 0
