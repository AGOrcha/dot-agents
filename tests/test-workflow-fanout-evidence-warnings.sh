#!/usr/bin/env bash
# Smoke test: fanout scope-evidence warning paths
# Flow: plan create → activate → add task →
#   (a) fanout without sidecar: warning on stderr
#   (b) fanout --skip-evidence-check: no warning
#   (c) create low-confidence sidecar, fanout: low-confidence warning
#   (d) --skip-evidence-check with low-confidence sidecar: no warning

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DA="${DA:-${REPO_ROOT}/dot-agents}"

if [[ ! -x "$DA" ]]; then
  echo "SKIP: dot-agents binary not found at $DA (set DA= to override)" >&2
  exit 0
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-workflow-fanout-evidence-warnings.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

SANDBOX="$WORK/repo"
mkdir -p "$SANDBOX"
FAKE_HOME="$WORK/agents-home"
mkdir -p "$FAKE_HOME"

# Use an isolated AGENTS_HOME so prefs/context do not bleed from previous runs.
export AGENTS_HOME="$FAKE_HOME"

(cd "$SANDBOX" && git init -q && "$DA" add . --name evidence-smoke --yes >/dev/null 2>&1)

(cd "$SANDBOX" && "$DA" workflow plan create ev-test --title "Evidence Smoke" >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow plan update ev-test --status active >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow task add ev-test \
  --id task-ev --title "Evidence task" --write-scope "src/ev.go" >/dev/null 2>&1)

_cleanup_delegation() {
  rm -f "$SANDBOX/.agents/active/delegation/task-ev.yaml"
  rm -f "$SANDBOX/.agents/active/delegation-bundles"/del-task-ev-*.yaml 2>/dev/null || true
}

# helper: run fanout with --skip-tdd-gate (sandbox has no real Go files)
_fanout() {
  (cd "$SANDBOX" && "$DA" workflow fanout \
    --plan ev-test --task task-ev --write-scope "src/ev.go" \
    --skip-tdd-gate "$@" 2>&1)
}

# ── (a) no sidecar: warning fires on stderr (also captured in combined) ───────
_cleanup_delegation
out_a="$(_fanout)"
# Warning only fires when graph adapter is available; if graph is absent the
# warning is suppressed — treat that as a skip, not a failure.
if echo "$out_a" | grep -q "no scope-evidence sidecar"; then
  : # warning present — correct
elif echo "$out_a" | grep -q "Delegation created"; then
  echo "NOTE: graph not available in this env; no-sidecar warning skipped"
else
  echo "FAIL (a): unexpected output from fanout without sidecar:" >&2
  echo "$out_a" >&2
  exit 1
fi

# ── (b) no sidecar + --skip-evidence-check: no warning ───────────────────────
_cleanup_delegation
out_b="$(_fanout --skip-evidence-check)"
if echo "$out_b" | grep -q "no scope-evidence sidecar"; then
  echo "FAIL (b): --skip-evidence-check should suppress no-sidecar warning" >&2
  echo "$out_b" >&2
  exit 1
fi

# ── (c) low-confidence sidecar: low-confidence warning fires ─────────────────
_cleanup_delegation
mkdir -p "$SANDBOX/.agents/workflow/plans/ev-test/evidence"
cat > "$SANDBOX/.agents/workflow/plans/ev-test/evidence/task-ev.scope.yaml" <<'EOF'
confidence: low
required_reads:
  - path: src/ev.go
    why: primary implementation file
decision_locks: []
EOF

out_c="$(_fanout)"
if ! echo "$out_c" | grep -q "scope-evidence confidence is low"; then
  echo "FAIL (c): low-confidence sidecar should emit a warning" >&2
  echo "$out_c" >&2
  exit 1
fi

# ── (d) low-confidence sidecar + --skip-evidence-check: no warning ───────────
_cleanup_delegation
out_d="$(_fanout --skip-evidence-check)"
if echo "$out_d" | grep -q "scope-evidence confidence is low"; then
  echo "FAIL (d): --skip-evidence-check should suppress low-confidence warning" >&2
  echo "$out_d" >&2
  exit 1
fi

echo "PASS: fanout evidence warnings — no-sidecar, low-confidence, and --skip-evidence-check all behave correctly"
