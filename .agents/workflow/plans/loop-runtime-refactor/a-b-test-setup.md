# Setup
```sh
TS="$(date +%Y%m%d-%H%M%S)"
BASE="$(git rev-parse --abbrev-ref HEAD)"

git worktree add "../dot-agents-pattern-e-$TS" HEAD
git worktree add "../dot-agents-script-$TS" HEAD

git -C "../dot-agents-pattern-e-$TS" switch -c "ab/pattern-e-$TS"
git -C "../dot-agents-script-$TS" switch -c "ab/script-$TS"
```

For a fair comparison, use the same task in both worktrees. For the current loop-runtime state, phase-5d-iter-log-schema is the best current candidate:

- plan: loop-runtime-refactor
- task: phase-5d-iter-log-schema
- write_scope: schemas/workflow-iter-log.schema.json,commands/workflow.go,commands/workflow_test.go,.agents/active/iteration-log/

## Script Worker Run

```sh
cd "../dot-agents-script-$TS"

RUN_DIR="$PWD/.ralph-loop-streams/script-ab-$TS"
mkdir -p "$RUN_DIR"

/usr/bin/time -p env \
AGENT_BIN=agent \
DOT_AGENTS='go run ./cmd/dot-agents' \
RALPH_MODEL='<your Cursor agent model id>' \
RALPH_AGENT_OUTPUT_FORMAT=stream-json \
RALPH_STREAM_PARTIAL=1 \
RALPH_FORCE=1 \
RALPH_ITERATIONS=5 \
RALPH_MAX_PARALLEL_WORKERS=1 \
RALPH_AUTO_FANOUT=1 \
RALPH_CLOSEOUT_AUTO=1 \
RALPH_FANOUT_PLAN=loop-runtime-refactor \
RALPH_FANOUT_TASK=phase-5d-iter-log-schema \
RALPH_FANOUT_WRITE_SCOPE='schemas/workflow-iter-log.schema.json,commands/workflow.go,commands/workflow_test.go,.agents/active/iteration-log/' \
RALPH_LOG_DIR="$RUN_DIR" \
./bin/tests/ralph-pipeline \
2>&1 | tee "$RUN_DIR/console.log"
```

## Pattern E Run

Open ../dot-agents-pattern-e-$TS in Cursor and use this prompt in Composer 2:

Use the loop methodology only for `loop-runtime-refactor`.

Target exactly:
- plan_id: loop-runtime-refactor
- task_id: phase-5d-iter-log-schema
- write_scope: schemas/workflow-iter-log.schema.json, commands/workflow.go, commands/workflow_test.go, .agents/active/iteration-log/

Required flow:
1. Run:
    - go run ./cmd/dot-agents workflow orient
    - go run ./cmd/dot-agents workflow next
    - go run ./cmd/dot-agents workflow tasks loop-runtime-refactor
2. Fan out the task with:
    - --plan loop-runtime-refactor
    - --task phase-5d-iter-log-schema
    - --delegate-profile loop-worker
    - --project-overlay .agents/active/active.loop.md
    - --context-file .agents/active/loop-state.md
    - --context-file .agents/workflow/plans/loop-runtime-refactor/TASKS.yaml
3. After fanout, spawn the worker as:
    Agent(
    description="Implement phase-5d-iter-log-schema in loop-runtime-refactor",
    subagent_type="loop-worker",
    prompt="Delegation bundle: <absolute_bundle_path>",
    mode="auto"
    )
4. The worker must stay inside write_scope, run tests, and stop at merge-back.
5. Do not auto-advance the canonical task blindly. `workflow advance` currently has a false-success bug; verify TASKS.yaml on disk before claiming status changed.
6. At the end, report:
    - bundle path
    - merge-back path
    - commit sha
    - tests run
    - worker iteration count
    - token/tool/duration usage if Cursor exposes it

Then write the Pattern E metrics file manually in that worktree:
```sh
cd "../dot-agents-pattern-e-$TS"

RUN_DIR="$PWD/.ralph-loop-streams/pattern-e-ab-$TS"
mkdir -p "$RUN_DIR"

cat > "$RUN_DIR/metrics.json" <<'EOF'
{
"run_id": "ab-REPLACE_TS",
"timestamp": "REPLACE_ISO8601",
"worker_mode": "subagent",
"bundles_created": 1,
"workers_spawned": 1,
"worker_iterations": { "phase-5d-iter-log-schema": REPLACE_ITERATIONS },
"merge_back_status": { "phase-5d-iter-log-schema": "present" },
"persisted_via_workflow_commands": { "phase-5d-iter-log-schema": "yes" },
"token_detail": {
    "phase-5d-iter-log-schema": {
    "total_tokens": REPLACE_OR_NULL,
    "tool_uses": REPLACE_OR_NULL,
    "duration_ms": REPLACE_OR_NULL
    }
},
"context_tokens_approx": REPLACE_OR_NULL,
"task_result": "REPLACE_SUMMARY",
"commit": "REPLACE_SHA"
}
EOF
```

Run In Parallel
Start the script command in one terminal, then immediately start the Pattern E Composer run in the other worktree. That reproduces the previous “parallel A/B” style without collisions.

Compare

```sh
jq . "../dot-agents-script-$TS/.ralph-loop-streams/script-ab-$TS/metrics.json"
jq . "../dot-agents-pattern-e-$TS/.ralph-loop-streams/pattern-e-ab-$TS/metrics.json"
```