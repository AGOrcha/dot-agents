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

write_fallback_checkpoint() {
  timestamp="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  project="$(project_name)"
  context_dir="$agents_home/context/$project"
  checkpoint_path="$context_dir/checkpoint.yaml"
  session_log_path="$context_dir/session-log.md"

  mkdir -p "$context_dir"

  git_branch="unknown"
  git_sha="unknown"
  dirty_count="0"
  modified_files=""
  if git -C "$project_dir" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git_branch=$(git -C "$project_dir" rev-parse --abbrev-ref HEAD 2>/dev/null || printf 'unknown')
    git_sha=$(git -C "$project_dir" rev-parse --short HEAD 2>/dev/null || printf 'unknown')
    modified_files=$(git -C "$project_dir" status --short 2>/dev/null | sed 's/^...//' || true)
    dirty_count=$(printf '%s\n' "$modified_files" | sed '/^$/d' | wc -l | tr -d ' ')
  fi

  next_action=$(sed -n 's/^next_action:[[:space:]]*"\{0,1\}\(.*[^"]\)"\{0,1\}[[:space:]]*$/\1/p' "$checkpoint_path" 2>/dev/null | head -n 1 || true)
  if [ -z "$next_action" ]; then
    next_action=$(grep '^- \[ \]' "$project_dir"/.agents/active/*.plan.md 2>/dev/null | head -n 1 | sed 's/^- \[ \] //' || true)
  fi
  if [ -z "$next_action" ]; then
    next_action="Review active plan"
  fi

  {
    printf 'schema_version: 1\n'
    printf 'timestamp: "%s"\n' "$timestamp"
    printf 'project:\n'
    printf '  name: "%s"\n' "$project"
    printf '  path: "%s"\n' "$project_dir"
    printf 'git:\n'
    printf '  branch: "%s"\n' "$git_branch"
    printf '  sha: "%s"\n' "$git_sha"
    printf '  dirty_file_count: %s\n' "$dirty_count"
    printf 'files:\n'
    if [ "$dirty_count" -eq 0 ]; then
      printf '  modified: []\n'
    else
      printf '  modified:\n'
      printf '%s\n' "$modified_files" | sed '/^$/d; s/^/    - "/; s/$/"/'
    fi
    printf 'message: ""\n'
    printf 'verification:\n'
    printf '  status: "unknown"\n'
    printf '  summary: ""\n'
    printf 'next_action: "%s"\n' "$next_action"
    printf 'blockers: []\n'
  } >"$checkpoint_path"

  {
    printf '## %s\n' "$timestamp"
    printf 'branch: %s\n' "$git_branch"
    printf 'sha: %s\n' "$git_sha"
    printf 'files: %s\n' "$dirty_count"
    printf 'verification: unknown\n'
    printf 'message: \n'
    printf 'next_action: %s\n\n' "$next_action"
  } >>"$session_log_path"
}

main() {
  if command -v dot-agents >/dev/null 2>&1; then
    if (
      cd "$project_dir" &&
      dot-agents workflow checkpoint --verification-status unknown --verification-summary ""
    ); then
      return 0
    fi
    printf 'session-capture warning: dot-agents workflow checkpoint failed, using shell fallback\n' >&2
  fi
  write_fallback_checkpoint
}

if ! main; then
  printf 'session-capture warning: hook failed unexpectedly\n' >&2
fi

exit 0
