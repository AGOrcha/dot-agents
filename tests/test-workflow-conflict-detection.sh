#!/usr/bin/env bash
# Smoke test: write-scope conflict detection in eligible output
# Flow: plan create → activate → add beta+epsilon (same scope) + delta (distinct) →
#       eligible --json: beta↔epsilon mutually conflict, max_batch excludes one,
#       delta always in max_batch

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DA="${DA:-${REPO_ROOT}/dot-agents}"

if [[ ! -x "$DA" ]]; then
  echo "SKIP: dot-agents binary not found at $DA (set DA= to override)" >&2
  exit 0
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-workflow-conflict-detection.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

SANDBOX="$WORK/repo"
mkdir -p "$SANDBOX"
FAKE_HOME="$WORK/agents-home"
mkdir -p "$FAKE_HOME"

# Use an isolated AGENTS_HOME so prefs/context do not bleed from previous runs.
export AGENTS_HOME="$FAKE_HOME"

(cd "$SANDBOX" && git init -q && "$DA" add . --name conflict-smoke --yes >/dev/null 2>&1)

# ── create plan with two conflicting tasks and one non-conflicting ────────────
(cd "$SANDBOX" && "$DA" workflow plan create conflict-test --title "Conflict Smoke" >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow plan update conflict-test --status active >/dev/null 2>&1)

(cd "$SANDBOX" && "$DA" workflow task add conflict-test \
  --id task-beta    --title "Beta"    --write-scope "src/shared.go" >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow task add conflict-test \
  --id task-epsilon --title "Epsilon" --write-scope "src/shared.go" >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow task add conflict-test \
  --id task-delta   --title "Delta"   --write-scope "src/delta.go"  >/dev/null 2>&1)

# ── eligible --json: validate conflict graph and max_batch ───────────────────
eligible_json="$(cd "$SANDBOX" && "$DA" workflow eligible --limit 8 --json 2>/dev/null)"

echo "$eligible_json" | python3 -c "
import sys, json

d = json.load(sys.stdin)
graph  = d['conflict_graph']
batch  = set(d['max_batch'])

# beta and epsilon must mutually conflict
beta_conflicts    = graph.get('task-beta',    [])
epsilon_conflicts = graph.get('task-epsilon', [])

assert 'task-epsilon' in beta_conflicts, \
    f'task-beta should list task-epsilon as conflict; graph={graph}'
assert 'task-beta' in epsilon_conflicts, \
    f'task-epsilon should list task-beta as conflict; graph={graph}'

# At most one of beta/epsilon in max_batch
assert not ('task-beta' in batch and 'task-epsilon' in batch), \
    f'max_batch must not contain both conflicting tasks; batch={batch}'

# delta has no conflict and must be in max_batch
assert 'task-delta' in batch, \
    f'task-delta should always be in max_batch (no conflict); batch={batch}'

delta_conflicts = graph.get('task-delta', [])
assert delta_conflicts == [], \
    f'task-delta should have no conflicts; got {delta_conflicts}'
" || { echo "FAIL: conflict graph or max_batch validation" >&2; exit 1; }

echo "PASS: conflict detection — beta↔epsilon mutual conflict, max_batch correct, delta unaffected"
