#!/usr/bin/env bash

set -euo pipefail

REPO_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLOSEOUT_SCRIPT="$REPO_SOURCE/bin/tests/ralph-closeout"
TMPDIR_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ralph-closeout-gate.XXXXXX")"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

setup_repo() {
  local dir="$1" task_id="$2" plan_id="$3"
  mkdir -p "$dir"
  cd "$dir"
  git init -q
  git config user.name "Test User"
  git config user.email "test@example.com"
  mkdir -p ".agents/active/merge-back" ".agents/active/delegation"
  cat >".agents/active/merge-back/${task_id}.md" <<YAML
task_id: ${task_id}
parent_plan_id: ${plan_id}
summary: done
YAML
  cat >".agents/active/delegation/${task_id}.yaml" <<YAML
id: del-${task_id}
parent_plan_id: ${plan_id}
parent_task_id: ${task_id}
status: completed
YAML
}

write_fake_da() {
  local dir="$1" task_id="$2" plan_id="$3" outcome="$4" closeout_allowed="$5" planning_required="$6"
  cat >"$dir/fake-dot-agents" <<EOF
#!/usr/bin/env bash
set -euo pipefail
cmd="\$*"
if [[ "\${1:-}" == "--json" && "\${2:-}" == "workflow" && "\${3:-}" == "delegation" && "\${4:-}" == "gate" ]]; then
  cat <<'JSON'
{
  "schema_version": 1,
  "task_id": "${task_id}",
  "plan_id": "${plan_id}",
  "outcome": "${outcome}",
  "closeout_allowed": ${closeout_allowed},
  "planning_required": ${planning_required},
  "reason": "gate ${outcome}"
}
JSON
  exit 0
fi
if [[ "\${1:-}" == "workflow" && "\${2:-}" == "delegation" && "\${3:-}" == "closeout" ]]; then
  echo "closeout-called" >>"${dir}/calls.log"
  exit 0
fi
if [[ "\${1:-}" == "workflow" && "\${2:-}" == "advance" ]]; then
  echo "advance-called" >>"${dir}/calls.log"
  exit 0
fi
echo "unexpected fake dot-agents invocation: \$cmd" >&2
exit 1
EOF
  chmod +x "$dir/fake-dot-agents"
}

run_case() {
  local case_id="$1" outcome="$2" closeout_allowed="$3" planning_required="$4" want_rc="$5" want_calls="$6"
  local dir="$TMPDIR_ROOT/$case_id"
  setup_repo "$dir" "t1" "p1"
  write_fake_da "$dir" "t1" "p1" "$outcome" "$closeout_allowed" "$planning_required"

  set +e
  (cd "$dir" && DOT_AGENTS="$dir/fake-dot-agents" RALPH_NO_LOG=1 "$CLOSEOUT_SCRIPT") >/dev/null 2>&1
  rc=$?
  set -e

  if [[ $rc -ne $want_rc ]]; then
    echo "FAIL: outcome=$outcome expected closeout rc=$want_rc, got $rc" >&2
    exit 1
  fi

  calls=""
  if [[ -f "$dir/calls.log" ]]; then
    calls="$(tr '\n' ' ' <"$dir/calls.log" | sed 's/[[:space:]]*$//')"
  fi
  if [[ "$calls" != "$want_calls" ]]; then
    echo "FAIL: outcome=$outcome expected calls '$want_calls', got '$calls'" >&2
    exit 1
  fi

  echo "PASS: outcome=$outcome rc=$want_rc calls='$want_calls'"
}

run_case "accept" "accept" "true" "false" 0 "closeout-called advance-called"
run_case "reject" "reject" "false" "false" 1 ""
run_case "escalate" "escalate" "false" "true" 1 ""

echo "ALL PASS"
