#!/usr/bin/env bash
# Smoke test: ralph-review-gate auto mode reads review-decision.yaml.
# - overall_decision: reject  → exit 2 (GATE_FAIL)
# - overall_decision: accept  → exit 0
# - no review-decision.yaml   → exit 0

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

# ── Test 1: no review-decision.yaml → auto-accept (exit 0) ──────────────────
dir1="$TMPDIR_ROOT/t1"
mkdir -p "$dir1"
setup_repo "$dir1" "t1"
bundle1="$(fake_bundle "$dir1" "t1")"
set +e
(cd "$dir1" && RALPH_REVIEW_GATE_AUTO=1 "$GATE_SCRIPT" --bundle "$bundle1") 2>/dev/null
rc1=$?
set -e
if [[ $rc1 -ne 0 ]]; then
  echo "FAIL: no review-decision.yaml should exit 0, got $rc1" >&2
  exit 1
fi
echo "PASS: no review-decision.yaml → exit 0"

# ── Test 2: overall_decision: accept → exit 0 ────────────────────────────────
dir2="$TMPDIR_ROOT/t2"
mkdir -p "$dir2"
setup_repo "$dir2" "t2"
bundle2="$(fake_bundle "$dir2" "t2")"
cat >"$dir2/.agents/active/verification/t2/review-decision.yaml" <<YAML
overall_decision: accept
phase_1_decision: accept
phase_2_decision: accept
YAML
set +e
(cd "$dir2" && RALPH_REVIEW_GATE_AUTO=1 "$GATE_SCRIPT" --bundle "$bundle2") 2>/dev/null
rc2=$?
set -e
if [[ $rc2 -ne 0 ]]; then
  echo "FAIL: overall_decision=accept should exit 0, got $rc2" >&2
  exit 1
fi
echo "PASS: overall_decision=accept → exit 0"

# ── Test 3: overall_decision: reject → exit 2 (GATE_FAIL) ───────────────────
dir3="$TMPDIR_ROOT/t3"
mkdir -p "$dir3"
setup_repo "$dir3" "t3"
bundle3="$(fake_bundle "$dir3" "t3")"
cat >"$dir3/.agents/active/verification/t3/review-decision.yaml" <<YAML
overall_decision: reject
phase_1_decision: reject
phase_2_decision: accept
YAML
set +e
(cd "$dir3" && RALPH_REVIEW_GATE_AUTO=1 "$GATE_SCRIPT" --bundle "$bundle3") 2>/dev/null
rc3=$?
set -e
if [[ $rc3 -ne 2 ]]; then
  echo "FAIL: overall_decision=reject should exit 2, got $rc3" >&2
  exit 1
fi
echo "PASS: overall_decision=reject → exit 2"

# ── Test 4: overall_decision: escalate → exit 0 (not a block) ───────────────
dir4="$TMPDIR_ROOT/t4"
mkdir -p "$dir4"
setup_repo "$dir4" "t4"
bundle4="$(fake_bundle "$dir4" "t4")"
cat >"$dir4/.agents/active/verification/t4/review-decision.yaml" <<YAML
overall_decision: escalate
phase_1_decision: escalate
phase_2_decision: accept
YAML
set +e
(cd "$dir4" && RALPH_REVIEW_GATE_AUTO=1 "$GATE_SCRIPT" --bundle "$bundle4") 2>/dev/null
rc4=$?
set -e
if [[ $rc4 -ne 0 ]]; then
  echo "FAIL: overall_decision=escalate should exit 0 (not block), got $rc4" >&2
  exit 1
fi
echo "PASS: overall_decision=escalate → exit 0"

echo "ALL PASS"
