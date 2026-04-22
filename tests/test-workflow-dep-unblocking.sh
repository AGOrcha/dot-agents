#!/usr/bin/env bash
# Smoke test: dependency unblocking flow
# Flow: plan create → activate → add alpha+beta(dep:alpha) →
#       eligible shows alpha only → advance alpha completed → eligible shows beta

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DA="${DA:-${REPO_ROOT}/dot-agents}"

if [[ ! -x "$DA" ]]; then
  echo "SKIP: dot-agents binary not found at $DA (set DA= to override)" >&2
  exit 0
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-workflow-dep-unblocking.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

SANDBOX="$WORK/repo"
mkdir -p "$SANDBOX"
FAKE_HOME="$WORK/agents-home"
mkdir -p "$FAKE_HOME"

# Use an isolated AGENTS_HOME so prefs/context do not bleed from previous runs.
export AGENTS_HOME="$FAKE_HOME"

# Bootstrap a minimal dot-agents project in the sandbox.
(cd "$SANDBOX" && git init -q && "$DA" add . --name dep-smoke --yes >/dev/null 2>&1)

# ── step 1: create plan and activate it ──────────────────────────────────────
(cd "$SANDBOX" && "$DA" workflow plan create dep-test --title "Dep Smoke" >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow plan update dep-test --status active >/dev/null 2>&1)

# ── step 2: add alpha (no deps) and beta (depends on alpha) ──────────────────
(cd "$SANDBOX" && "$DA" workflow task add dep-test \
  --id alpha --title "Alpha" --write-scope "src/alpha.go" >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow task add dep-test \
  --id beta --title "Beta" --write-scope "src/beta.go" --depends-on "alpha" >/dev/null 2>&1)

# ── step 3: eligible should show alpha, not beta ─────────────────────────────
eligible_json="$(cd "$SANDBOX" && "$DA" workflow eligible --limit 8 --json 2>/dev/null)"

echo "$eligible_json" | python3 -c "
import sys, json
d = json.load(sys.stdin)
ids = [t['task_id'] for t in d['eligible_tasks']]
assert 'alpha' in ids, f'alpha should be eligible; got {ids}'
assert 'beta' not in ids, f'beta should be blocked by alpha; got {ids}'
" || { echo "FAIL: pre-advance eligible check" >&2; exit 1; }

# ── step 4: advance alpha to completed ───────────────────────────────────────
(cd "$SANDBOX" && "$DA" workflow advance dep-test --task alpha --status completed >/dev/null 2>&1)

# ── step 5: eligible should now show beta, not alpha ─────────────────────────
eligible_json2="$(cd "$SANDBOX" && "$DA" workflow eligible --limit 8 --json 2>/dev/null)"

echo "$eligible_json2" | python3 -c "
import sys, json
d = json.load(sys.stdin)
ids = [t['task_id'] for t in d['eligible_tasks']]
assert 'beta' in ids, f'beta should be eligible after alpha completed; got {ids}'
assert 'alpha' not in ids, f'completed alpha should not be eligible; got {ids}'
" || { echo "FAIL: post-advance eligible check" >&2; exit 1; }

echo "PASS: dep unblocking — alpha→completed unblocks beta"
