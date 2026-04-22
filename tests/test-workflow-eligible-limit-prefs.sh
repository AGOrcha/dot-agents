#!/usr/bin/env bash
# Smoke test: eligible limit and prefs round-trip
# Flow: plan create → activate → add 3 no-dep tasks →
#       eligible (pref=1, shows 1) → prefs set max_parallel_workers=3 →
#       eligible (shows 3, footer=pref) → eligible --limit 2 (footer=--limit)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DA="${DA:-${REPO_ROOT}/dot-agents}"

if [[ ! -x "$DA" ]]; then
  echo "SKIP: dot-agents binary not found at $DA (set DA= to override)" >&2
  exit 0
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-workflow-eligible-limit-prefs.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

SANDBOX="$WORK/repo"
FAKE_HOME="$WORK/agents-home"
mkdir -p "$SANDBOX" "$FAKE_HOME"

# Use an isolated AGENTS_HOME so local prefs don't bleed from previous runs.
export AGENTS_HOME="$FAKE_HOME"

(cd "$SANDBOX" && git init -q && "$DA" add . --name pref-smoke --yes >/dev/null 2>&1)

# ── create and activate plan with 3 no-dep tasks ─────────────────────────────
(cd "$SANDBOX" && "$DA" workflow plan create pref-test --title "Pref Smoke" >/dev/null 2>&1)
(cd "$SANDBOX" && "$DA" workflow plan update pref-test --status active >/dev/null 2>&1)

for id in alpha beta gamma; do
  (cd "$SANDBOX" && "$DA" workflow task add pref-test \
    --id "task-$id" --title "Task $id" --write-scope "src/$id.go" >/dev/null 2>&1)
done

# ── pref=1 (default): eligible should return 1 task ──────────────────────────
eligible_json="$(cd "$SANDBOX" && "$DA" workflow eligible --json 2>/dev/null)"
count="$(echo "$eligible_json" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['eligible_tasks']))")"
if [[ "$count" != "1" ]]; then
  echo "FAIL: default pref=1 should yield 1 eligible task; got $count" >&2
  exit 1
fi

# ── human output footer should say max_parallel_workers=1 ────────────────────
human_out="$(cd "$SANDBOX" && "$DA" workflow eligible 2>/dev/null)"
if ! echo "$human_out" | grep -q "max_parallel_workers=1"; then
  echo "FAIL: footer should say 'max_parallel_workers=1' when using pref" >&2
  echo "$human_out" >&2
  exit 1
fi

# ── set pref to 3, eligible should now return 3 ──────────────────────────────
(cd "$SANDBOX" && "$DA" workflow prefs set-local execution.max_parallel_workers 3 >/dev/null 2>&1)

eligible_json2="$(cd "$SANDBOX" && "$DA" workflow eligible --json 2>/dev/null)"
count2="$(echo "$eligible_json2" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['eligible_tasks']))")"
if [[ "$count2" != "3" ]]; then
  echo "FAIL: pref=3 should yield 3 eligible tasks; got $count2" >&2
  exit 1
fi

human_out2="$(cd "$SANDBOX" && "$DA" workflow eligible 2>/dev/null)"
if ! echo "$human_out2" | grep -q "max_parallel_workers=3"; then
  echo "FAIL: footer should say 'max_parallel_workers=3' after pref set" >&2
  echo "$human_out2" >&2
  exit 1
fi

# ── explicit --limit 2 overrides pref=3; footer says --limit=2 ───────────────
human_out3="$(cd "$SANDBOX" && "$DA" workflow eligible --limit 2 2>/dev/null)"
count3="$(echo "$human_out3" | grep -c "\[pref-test/task-" || true)"
if [[ "$count3" != "2" ]]; then
  echo "FAIL: --limit 2 should show 2 tasks; got $count3" >&2
  echo "$human_out3" >&2
  exit 1
fi
if ! echo "$human_out3" | grep -q -- "--limit=2"; then
  echo "FAIL: footer should say '--limit=2' when explicit limit overrides pref" >&2
  echo "$human_out3" >&2
  exit 1
fi

# ── prefs show --json should reflect the local override ──────────────────────
prefs_json="$(cd "$SANDBOX" && "$DA" workflow prefs show --json 2>/dev/null)"
pref_val="$(echo "$prefs_json" | python3 -c "import sys,json; print(json.load(sys.stdin)['execution']['max_parallel_workers'])")"
if [[ "$pref_val" != "3" ]]; then
  echo "FAIL: prefs show --json should report max_parallel_workers=3; got $pref_val" >&2
  exit 1
fi

echo "PASS: eligible limit + prefs round-trip — pref controls default, --limit overrides, footer label correct"
