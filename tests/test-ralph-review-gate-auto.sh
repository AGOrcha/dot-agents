#!/usr/bin/env bash
# Smoke test: ralph-review-gate auto mode delegates deterministic evaluation to
# `dot-agents workflow delegation gate`.
# - outcome: accept   → exit 0
# - outcome: reject   → exit 2
# - outcome: escalate → exit 3

set -euo pipefail

REPO_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE_SCRIPT="$REPO_SOURCE/bin/tests/ralph-review-gate"
TMPDIR_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ralph-gate-auto.XXXXXX")"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

setup_repo() {
  local dir="$1" task_id="$2"
  cd "$dir"
  git init -q
  mkdir -p ".agents/active/verification/$task_id" ".agents/active/merge-back" ".agents/active/delegation-bundles"
  cat >".agents/active/merge-back/${task_id}.md" <<YAML
task_id: $task_id
YAML
}

fake_bundle() {
  local dir="$1" task_id="$2"
  mkdir -p "$dir/.agents/active/delegation-bundles"
  cat >"$dir/.agents/active/delegation-bundles/del-${task_id}.yaml" <<YAML
task_id: $task_id
plan_id: p1
YAML
  printf '%s' "$dir/.agents/active/delegation-bundles/del-${task_id}.yaml"
}

write_fake_da() {
  local dir="$1" outcome="$2" closeout_allowed="$3" planning_required="$4" reason="$5"
  cat >"$dir/fake-dot-agents" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [[ "\${1:-}" == "--json" && "\${2:-}" == "workflow" && "\${3:-}" == "delegation" && "\${4:-}" == "gate" ]]; then
  cat <<'JSON'
{
  "schema_version": 1,
  "task_id": "t1",
  "plan_id": "p1",
  "outcome": "${outcome}",
  "closeout_allowed": ${closeout_allowed},
  "planning_required": ${planning_required},
  "reason": "${reason}"
}
JSON
  exit 0
fi
echo "unexpected fake dot-agents invocation: \$*" >&2
exit 1
EOF
  chmod +x "$dir/fake-dot-agents"
}

run_case() {
  local task_id="$1" outcome="$2" closeout_allowed="$3" planning_required="$4" reason="$5" want_rc="$6" label="$7"
  local dir="$TMPDIR_ROOT/$task_id"
  mkdir -p "$dir"
  setup_repo "$dir" "$task_id"
  bundle="$(fake_bundle "$dir" "$task_id")"
  write_fake_da "$dir" "$outcome" "$closeout_allowed" "$planning_required" "$reason"

  set +e
  (cd "$dir" && DOT_AGENTS="$dir/fake-dot-agents" RALPH_REVIEW_GATE_AUTO=1 "$GATE_SCRIPT" --bundle "$bundle") 2>/dev/null
  rc=$?
  set -e

  if [[ $rc -ne $want_rc ]]; then
    echo "FAIL: $label should exit $want_rc, got $rc" >&2
    exit 1
  fi
  echo "PASS: $label → exit $want_rc"
}

run_case "t1" "accept" "true" "false" "accepted" 0 "outcome=accept"
run_case "t2" "reject" "false" "false" "failed gate" 2 "outcome=reject"
run_case "t3" "escalate" "false" "true" "planning review required" 3 "outcome=escalate"

echo "ALL PASS"
