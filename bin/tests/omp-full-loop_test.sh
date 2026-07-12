#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
DRIVER="$ROOT/bin/tests/omp-full-loop"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
REPO="$TMP/repo"
FAKES="$TMP/bin"
STATE="$TMP/state"
RUNS="$TMP/runs"
mkdir -p "$REPO/.agents/active/delegation" "$REPO/.agents/active/delegation-bundles" "$FAKES" "$STATE"

cat > "$FAKES/da" <<'DA'
#!/usr/bin/env bash
set -euo pipefail
state="${FAKE_STATE:?}"
scenario="${FAKE_SCENARIO:-normal}"
repo="${FULL_LOOP_REPO_ROOT:?}"
printf 'da %s\n' "$*" >> "$state/calls"
if [[ "$*" == *"workflow slots"* ]]; then
  printf '{"occupied":0,"awaiting_owner":0,"blocked":0,"pending":3,"terminal":0,"max_parallel":2,"available":2}\n'
elif [[ "$*" == *"workflow eligible"* ]]; then
  if [[ -f "$state/done" ]]; then
    printf '{"eligible_tasks":[],"max_batch":[],"total_eligible":0,"max_parallel":2}\n'
  else
    cat <<'JSON'
{"eligible_tasks":[
 {"plan_id":"plan-a","task_id":"task-a","app_type":"go-cli","write_scope":["a/"]},
 {"plan_id":"plan-b","task_id":"task-b","app_type":"web","write_scope":["b/"]},
 {"plan_id":"plan-c","task_id":"conflicting-task","app_type":"go-cli","write_scope":["a/file.go"]}
],"max_batch":["task-a","task-b"],"total_eligible":3,"max_parallel":2}
JSON
  fi
elif [[ "$*" == workflow\ fanout* ]]; then
  plan="" task=""
  while (($#)); do
    case "$1" in
      --plan) plan="$2"; shift 2 ;;
      --task) task="$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  [[ -n "$plan" && -n "$task" ]]
  if [[ "$scenario" == "fanout-failure" && "$task" == "task-b" ]]; then
    exit 9
  fi
  id="delegation-$task"
  printf 'id: %s\nparent_plan_id: %s\nparent_task_id: %s\n' "$id" "$plan" "$task" > "$repo/.agents/active/delegation/$task.yaml"
  printf 'id: %s\n' "$id" > "$repo/.agents/active/delegation-bundles/$id.yaml"
else
  echo "unexpected fake da call: $*" >&2
  exit 64
fi
DA
chmod +x "$FAKES/da"

cat > "$FAKES/omp-swarm" <<'OMP'
#!/usr/bin/env bash
set -euo pipefail
state="${FAKE_STATE:?}"
scenario="${FAKE_SCENARIO:-normal}"
yaml="${1:?missing swarm yaml}"
if [[ "$yaml" == *reconcile.swarm.yaml ]]; then
  python3 - "$WAVE_MANIFEST" "$scenario" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
scenario = sys.argv[2]
if scenario == "normal":
    assert len(m["tasks"]) == 2, m
    assert all(t["exit_code"] == 0 for t in m["tasks"]), m
    assert {t["result"] for t in m["tasks"]} == {"READY", "FOLD_BACK"}, m
elif scenario == "failure":
    assert len(m["tasks"]) == 2, m
    by_task = {t["task_id"]: t for t in m["tasks"]}
    assert by_task["task-a"]["exit_code"] == 7, m
    assert by_task["task-a"]["result"] == "UNKNOWN", m
    assert by_task["task-b"]["result"] == "FOLD_BACK", m
elif scenario == "recovery":
    assert len(m["tasks"]) == 1, m
    assert m["tasks"][0]["result"] == "UNKNOWN", m
    assert m["tasks"][0]["exit_code"] == 255, m
elif scenario == "fanout-failure":
    assert len(m["tasks"]) == 2, m
    by_task = {t["task_id"]: t for t in m["tasks"]}
    assert by_task["task-a"]["result"] == "READY", m
    assert by_task["task-b"]["result"] == "FOLD_BACK", m
    assert by_task["task-b"]["exit_code"] == 70, m
else:
    raise AssertionError(scenario)
PY
  if [[ "$scenario" != "recovery" ]]; then
    [[ -f "$WAVE_DIR/tasks/plan-a__task-a/exit-code" ]]
    [[ -f "$WAVE_DIR/tasks/plan-b__task-b/exit-code" ]]
  fi
  printf 'reconcile\n' >> "$state/omp-calls"
  : > "$WAVE_DIR/RECONCILED"
  : > "$state/done"
else
  printf 'inner %s\n' "$TASK" >> "$state/omp-calls"
  mkdir -p "$COORD"
  if [[ "$scenario" == "failure" && "$TASK" == "plan-a/task-a" ]]; then
    exit 7
  elif [[ "$TASK" == "plan-b/task-b" ]]; then
    printf 'FOLD-BACK\n' > "$COORD/GATE.md"
    printf '{"task_notes":[{"slug":"fixture-fold-back","observation":"fixture rejection"}]}\n' > "$COORD/refinement.json"
    sleep 0.05
  else
    printf 'READY\n' > "$COORD/READY.md"
    sleep 0.15
  fi
fi
OMP
chmod +x "$FAKES/omp-swarm"

# These sentinels prove GPT/Claude selection stays inside OMP; invoking a vendor CLI fails the test.
for vendor in claude codex copilot cursor; do
  cat > "$FAKES/$vendor" <<'VENDOR'
#!/usr/bin/env bash
echo "vendor CLI must not be invoked" >&2
exit 99
VENDOR
  chmod +x "$FAKES/$vendor"
done

PATH="$FAKES:$PATH" \
FAKE_STATE="$STATE" \
FULL_LOOP_REPO_ROOT="$REPO" \
FULL_LOOP_DA_BIN="$FAKES/da" \
OMP_SWARM_BIN="$FAKES/omp-swarm" \
FULL_LOOP_INNER_YAML="$ROOT/.agents/workflow/runtime/full-loop/profile-driven.swarm.yaml" \
FULL_LOOP_RECONCILE_YAML="$ROOT/.agents/workflow/runtime/full-loop/reconcile.swarm.yaml" \
FULL_LOOP_RUN_ROOT="$RUNS" \
FULL_LOOP_RUN_ID="fixture" \
"$DRIVER" --max-waves 3 > "$STATE/output"

[[ "$(grep -c '^da workflow fanout ' "$STATE/calls")" -eq 2 ]]
[[ "$(grep -c '^inner ' "$STATE/omp-calls")" -eq 2 ]]
[[ "$(grep -c '^reconcile$' "$STATE/omp-calls")" -eq 1 ]]
! grep -q 'conflicting-task' "$STATE/omp-calls"
[[ -f "$RUNS/fixture/wave-1/RECONCILED" ]]
[[ ! -d "$RUNS/fixture/wave-2" ]]
[[ "$(cat "$RUNS/latest")" == "$RUNS/fixture" ]]
grep -q 'FULL_LOOP_RUN:' "$STATE/output"


# An inner crash must still pass through reconciliation so fanout-held slots are not orphaned.
REPO_FAIL="$TMP/repo-failure"
STATE_FAIL="$TMP/state-failure"
RUNS_FAIL="$TMP/runs-failure"
mkdir -p "$REPO_FAIL/.agents/active/delegation" "$REPO_FAIL/.agents/active/delegation-bundles" "$STATE_FAIL"
PATH="$FAKES:$PATH" \
FAKE_STATE="$STATE_FAIL" \
FAKE_SCENARIO="failure" \
FULL_LOOP_REPO_ROOT="$REPO_FAIL" \
FULL_LOOP_DA_BIN="$FAKES/da" \
OMP_SWARM_BIN="$FAKES/omp-swarm" \
FULL_LOOP_INNER_YAML="$ROOT/.agents/workflow/runtime/full-loop/profile-driven.swarm.yaml" \
FULL_LOOP_RECONCILE_YAML="$ROOT/.agents/workflow/runtime/full-loop/reconcile.swarm.yaml" \
FULL_LOOP_RUN_ROOT="$RUNS_FAIL" \
FULL_LOOP_RUN_ID="failure" \
"$DRIVER" --max-waves 3 > "$STATE_FAIL/output"
[[ -f "$RUNS_FAIL/failure/wave-1/RECONCILED" ]]
python3 - "$RUNS_FAIL/failure/wave-1/manifest.json" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
assert any(t["exit_code"] == 7 and t["result"] == "UNKNOWN" for t in m["tasks"]), m
PY

# A stale lock plus an incomplete prior wave is recovered before any new selection.
REPO_RECOVER="$TMP/repo-recovery"
STATE_RECOVER="$TMP/state-recovery"
RUNS_RECOVER="$TMP/runs-recovery"
ORPHAN="$RUNS_RECOVER/orphan/wave-1"
COORD_RECOVER="$ORPHAN/tasks/plan-z__task-z"
mkdir -p "$REPO_RECOVER/.agents/active/delegation" "$REPO_RECOVER/.agents/active/delegation-bundles" \
  "$STATE_RECOVER" "$COORD_RECOVER" "$RUNS_RECOVER/.driver-lock"
printf '999999\n' > "$RUNS_RECOVER/.driver-lock/pid"
printf '{"plan_id":"plan-z","task_id":"task-z","app_type":"meta","write_scope":["z/"]}\n' > "$ORPHAN/selection.jsonl"
printf '999999\n' > "$COORD_RECOVER/pid"
PATH="$FAKES:$PATH" \
FAKE_STATE="$STATE_RECOVER" \
FAKE_SCENARIO="recovery" \
FULL_LOOP_REPO_ROOT="$REPO_RECOVER" \
FULL_LOOP_DA_BIN="$FAKES/da" \
OMP_SWARM_BIN="$FAKES/omp-swarm" \
FULL_LOOP_INNER_YAML="$ROOT/.agents/workflow/runtime/full-loop/profile-driven.swarm.yaml" \
FULL_LOOP_RECONCILE_YAML="$ROOT/.agents/workflow/runtime/full-loop/reconcile.swarm.yaml" \
FULL_LOOP_RUN_ROOT="$RUNS_RECOVER" \
FULL_LOOP_RUN_ID="after-recovery" \
"$DRIVER" --max-waves 1 > "$STATE_RECOVER/output"
[[ -f "$ORPHAN/RECONCILED" ]]
grep -q 'recovering incomplete wave' "$STATE_RECOVER/output"

# A later fanout refusal must not strand an earlier successful delegation.
REPO_FANOUT="$TMP/repo-fanout"
STATE_FANOUT="$TMP/state-fanout"
RUNS_FANOUT="$TMP/runs-fanout"
mkdir -p "$REPO_FANOUT/.agents/active/delegation" "$REPO_FANOUT/.agents/active/delegation-bundles" "$STATE_FANOUT"
PATH="$FAKES:$PATH" \
FAKE_STATE="$STATE_FANOUT" \
FAKE_SCENARIO="fanout-failure" \
FULL_LOOP_REPO_ROOT="$REPO_FANOUT" \
FULL_LOOP_DA_BIN="$FAKES/da" \
OMP_SWARM_BIN="$FAKES/omp-swarm" \
FULL_LOOP_INNER_YAML="$ROOT/.agents/workflow/runtime/full-loop/profile-driven.swarm.yaml" \
FULL_LOOP_RECONCILE_YAML="$ROOT/.agents/workflow/runtime/full-loop/reconcile.swarm.yaml" \
FULL_LOOP_RUN_ROOT="$RUNS_FANOUT" \
FULL_LOOP_RUN_ID="fanout-failure" \
"$DRIVER" --max-waves 3 > "$STATE_FANOUT/output"
[[ -f "$RUNS_FANOUT/fanout-failure/wave-1/RECONCILED" ]]
[[ "$(grep -c '^inner ' "$STATE_FANOUT/omp-calls")" -eq 1 ]]
echo "omp-full-loop tests: PASS"
