# Loop Runtime Refactor

Status: Active
Last updated: 2026-04-14
Depends on:
- Analysis from session `31916bf2-980e-4311-b2e6-c3c31d872eba`
- `docs/LOOP_ORCHESTRATION_SPEC.md`
- `loop-orchestrator-layer` plan (completed — infrastructure it built is the foundation here)

## Goal

Three parallel tracks that make the orchestration loop faster, less ambiguous, and testable
as a real multi-agent system:

1. **Skill chain smoothing** — fix load-order bugs, strip duplication, kill dead branches, add
   reconciliation iteration type, and auto-escalate tool-bugs. Reduces per-iteration cognitive
   overhead without touching the CLI.

2. **Ralph runtime scripts** — split `ralph-cursor-loop.sh` into role-pure orchestrator,
   worker, closeout, and pipeline scripts. `ITERATIONS` becomes a worker-budget param, not
   an orchestrator re-run count.

3. **Subagent worker** — a cold-start-capable worker prompt + skill so the orchestrator can
   spawn workers via the Claude Code `Agent` tool (Pattern E) instead of shelling out to
   a Cursor agent process. Needed for metrics comparison.

## Notes

- `workflow next` matches `current_focus_task` against `task.Title`, not `task.ID`. Always set
  it to the exact title string or use `workflow plan update <id> --focus "<title>"`. Setting it
  to a task ID causes the plan to drop to priority 3 (loses to any plan with a correctly set
  focus title).

## Decisions

- **No CLI changes in this plan.** All three tracks touch only: skills, prompt overlays,
  shell scripts, and one new subagent skill file.
- **Use `/skill-architect` for all skill work.** Tasks touching `.agents/skills/` use
  `/skill-architect` with the appropriate mode:
  - `improve` — for refining existing skill files (1a, 1e, 4b)
  - `new` — for creating the loop-worker skill from scratch (4a)
  Direct file edits to skill files are not acceptable for these tasks.
- `agent-start` is a global user-scope skill — do not modify it. Add a project-level
  suppression note to `active.loop.md` so the loop context routes to `orchestrator-session-start`
  instead.
- `active.loop.md` was always designed as the worker project overlay (Layer 2 of the
  three-layer model in `~/.agents/profiles/loop-worker.md`). Orchestrator content crept in
  over time. Phase 2 refactors `active.loop.md` back to worker-only scope and extracts
  orchestrator content into a new `orchestrator.loop.md`. No new parallel file — the
  existing overlay gets its intended purpose restored.
- The `loop-worker` skill (Phase 4a) is a thin wrapper: it loads
  `~/.agents/profiles/loop-worker.md` (already has discipline + closeout sequence) plus
  the startup instructions. It does not duplicate the profile's content.
- Scripts land in `bin/tests/` (same location as `ralph-cursor-loop`). They are not installed
  globally; CI ignores them.

---

## Phase 1 — Skill Chain Fixes

**Write scope:** `.agents/skills/`, `.agents/active/active.loop.md`

### 1a. Fix `orchestrator-session-start` load order

**Problem:** `SKILL.md` loads `workflow.md` (step 1) then `gotchas.md` (step 2). The "Do Not
Turn The Orchestrator Into A Worker" rule fires after the fanout/direct decision is already made.

**Fix:** Reorder SKILL.md steps:
```
0. preflight.md
1. gotchas.md          ← moved before workflow.md
2. workflow.md
3. delegation-lifecycle (only if fanout was run)
```

File: `.agents/skills/orchestrator-session-start/SKILL.md`

### 1b. Strip closeout from `active.loop.md`, delegate to `/iteration-close`

**Problem:** Steps 19–30 in `active.loop.md` are a duplicate of the `iteration-close` skill.
The agent executes them inline and never invokes the skill.

**Fix:** Replace steps 19–30 with a single step:
```
19. Run /iteration-close
    CLI broken fallback: if the CLI binary won't build, mark
    persisted_via_workflow_commands: paused — <reason>, create a fold-back item
    for the blocker, and continue. Run deferred persist at the start of the next
    iteration before picking new work.
```

File: `.agents/active/active.loop.md`

### 1c. Add loop-context suppression for `agent-start`

**Problem:** `agent-start` (global skill) matches generic session-start phrasing and displaces
`orchestrator-session-start` in loop contexts, wasting tokens on generic checks.

**Fix:** Add a note at the top of `active.loop.md` under `## Project overlay metadata`:
```
- **Skill routing:** In this project, prefer `/orchestrator-session-start` over `/agent-start`.
  `agent-start` is for one-off tasks in repos without a dot-agents workflow setup.
```

Also update `orchestrator-session-start/SKILL.md` description to include an explicit trigger:
"Use in repos with `.agents/workflow/` and `active.loop.md` present."

File: `.agents/active/active.loop.md`, `.agents/skills/orchestrator-session-start/SKILL.md`

### 1d. Reduce session-start oracles and add reconciliation iteration type

**Problem:**
- `workflow status` is always stale (documented baseline) but still in the required startup chain.
- Pure reconciliation iterations have no valid feedback goal or CLI trace, forcing the agent to
  invent stretch goals that produce `informative-nonblocking` non-evidence.

**Fixes:**

Remove `workflow status` from step 4 of `active.loop.md`. Replace with:
```
4. If workflow orient output conflicts with canonical task state in step 7, log the mismatch
   under ## Loop Health — do not chase it; the canonical YAML wins.
```

Add iteration type `reconciliation` to the Implementation section:
```
Reconciliation iterations (state catch-up, no new code):
- feedback_goal: "state hygiene: confirm <X> is marked correctly"
- CLI trace: workflow tasks <plan> before/after is sufficient
- one_item_only: n/a (a single reconciliation pass may touch multiple checkboxes by definition)
- cli_produced_actionable_feedback: n/a
```

File: `.agents/active/active.loop.md`

### 1e. Add tool-bug auto-escalation to `iteration-close`

**Problem:** `[tool-bug]` items accumulate as baseline noise with no escalation path. The pgx
dependency has been documented for multiple iterations without being scheduled.

**Fix:** Add to `iteration-close/instructions/workflow.md` after the checkpoint step:
```
## Auto-escalate tool-bugs

If any [tool-bug] was logged this iteration:

  dot-agents workflow fold-back create \
    --plan <active-plan-id> \
    --observation "[tool-bug]: <detail from trace — command, error, reproduction>" \
    --propose

This routes the bug into the proposal queue for orchestrator scheduling rather than
leaving it as baseline noise. One fold-back per distinct tool-bug per iteration.
```

File: `.agents/skills/iteration-close/instructions/workflow.md`

---

## Phase 2 — Overlay Split

**Write scope:** `.agents/active/`

The `loop-worker.md` global profile already documents the three-layer model:
- Layer 1 (global): `~/.agents/profiles/loop-worker.md` — discipline, closeout sequence
- Layer 2 (project): repo `*.loop.md` — repo-specific CLI commands, plans, matrices
- Layer 3 (bundle): delegation YAML — per-task scope, prompts, context

`active.loop.md` was always intended as the worker's Layer 2 overlay. Orchestrator content
(wave selection, multi-oracle startup, evidence decision tree, scenario coverage tables) crept
in and does not belong there. This phase restores the intended boundary.

### 2a. Refactor `active.loop.md` to worker-only scope (~100 lines)

Remove from `active.loop.md`:
- Steps 1–12 session-start oracle chain (keep only: read loop-state Current Position +
  `workflow tasks <plan>` for the assigned task + `git status`)
- Wave selection logic (steps 11–12 and plan-wave-picker guidance)
- The 90-line evidence decision tree (steps 93–180 — replace with one paragraph)
- Scenario coverage and command coverage update instructions (move to orchestrator overlay)
- Step 49 scenario tag taxonomy (move to orchestrator overlay)

Keep in `active.loop.md`:
- Role declaration (worker, bundle is context)
- 3-step startup (bundle → workflow tasks → git status)
- Implementation rules (one item, write_scope, tests, one CLI trace)
- Closeout: `/iteration-close` single line (Phase 1b already handles this)
- Read-only CLI command list (workflow tasks, verify log, kg health)
- Write CLI commands (verify record, checkpoint, merge-back)
- Safety guardrails (hard rules — no refresh/install, no ~/.agents without approval)
- Loop-state write surface: iteration log entry + Next Iteration Playbook only
  (worker does NOT update ## Current Position — that belongs to the orchestrator)

### 2b. Create `orchestrator.loop.md`

Extract orchestrator-facing content from `active.loop.md` into a new overlay:
- Multi-oracle startup chain (orient + next + plan + tasks)
- Wave selection and plan-wave-picker guidance
- Evidence decision tree (simplified per Phase 1d)
- Scenario coverage and command coverage tables
- Loop-state ## Current Position and ## Loop Health update instructions

This file is passed via `--project-overlay .agents/active/orchestrator.loop.md` when
running the orchestrator agent (ralph-orchestrate.sh, orchestrator-session-start turns).

Workers continue to use `--project-overlay .agents/active/active.loop.md`.

---

## Phase 3 — Ralph Scripts

**Write scope:** `bin/tests/`

### 3a. `ralph-orchestrate.sh`

Fork of current `ralph-cursor-loop.sh`. Key changes:
- Remove `ITERATIONS` outer loop — orchestrator runs exactly once per invocation
- Snapshot: `orient + next` only (drop `status` from the 3-command snapshot)
- Multi-task discovery: after orient/next, call `workflow tasks <plan>` for the chosen plan;
  find all tasks with `status: pending` and no active delegation contract; call fanout for each
- Changed deliverables in prompt: "Do NOT implement. Output RALPH_BUNDLE: <path> lines (one
  per task). Write TASKS.yaml notes for each. Stop."
- Machine-readable output: each bundle path emitted as `RALPH_BUNDLE: <absolute_path>` on its
  own line so the pipeline can grep it

Env vars:
- `RALPH_MAX_PARALLEL_WORKERS` (default 3) — cap on concurrent bundles
- `RALPH_AUTO_FANOUT` (default 1) — orchestrator creates bundles for all unblocked tasks
- `RALPH_FANOUT_PLAN` / `RALPH_FANOUT_TASK` — manual override (still works)

### 3b. `ralph-cursor-loop.sh` (reworked to worker-only)

Breaking change: `--bundle <path>` is now required. Without it the script exits with an error
and a pointer to `ralph-orchestrate.sh`.

Key changes:
- Remove `run_fanout_preflight` function (orchestrator's job now)
- `RALPH_ITERATIONS` is the worker implementation budget (default 5)
- Worker prompt (~30 lines): read bundle → read worker overlay → implement write_scope →
  `/iteration-close`
- No `workflow orient/status/next` in the prompt
- Worker exits after merge-back is written (or budget exhausted)
- Each iteration logs to `$LOG_DIR/worker-iter-<N>.{ndjson|log}`

Env vars kept: `RALPH_ITERATIONS`, `RALPH_MODEL`, `RALPH_FORCE`, `RALPH_NO_LOG`,
`RALPH_LOG_DIR`, `RALPH_AGENT_OUTPUT_FORMAT`

### 3c. `ralph-closeout.sh`

New script. Scans for completed delegation contracts with merge-back artifacts; for each,
runs the orchestrator pass that accepts and archives.

```bash
# For each .agents/active/merge-back/*.md:
#   1. Extract task_id and plan_id
#   2. Run: dot-agents workflow delegation closeout --plan <id> --task <id> --decision accept
#   3. Log result

# OR: spawn a single Cursor/Claude agent pass with:
#   - list of merge-back artifacts as context
#   - instruction to run workflow advance + delegation closeout for each
#   - orchestrator-session-start as the skill reference
```

Env vars: `RALPH_CLOSEOUT_AUTO` (default 1 — auto-accept all pending merge-backs),
`RALPH_LOG_DIR`

### 3d. `ralph-pipeline.sh`

Wrapper: orchestrate → parallel workers → closeout.

```bash
#!/usr/bin/env bash
# bin/tests/ralph-pipeline.sh
# Full E2E: orchestrate → parallel workers → closeout

LOG_DIR=".ralph-loop-streams/run-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$LOG_DIR"

# Phase 1: Orchestrate (emits RALPH_BUNDLE: lines)
./bin/tests/ralph-orchestrate.sh 2>&1 | tee "$LOG_DIR/orchestrator.log"
mapfile -t BUNDLES < <(grep '^RALPH_BUNDLE:' "$LOG_DIR/orchestrator.log" | awk '{print $2}')

[[ ${#BUNDLES[@]} -eq 0 ]] && { echo "no unblocked tasks — done"; exit 0; }
echo "Spawning ${#BUNDLES[@]} worker(s)..." >&2

# Phase 2: Workers in parallel (isolated write scopes from bundle)
pids=()
for bundle in "${BUNDLES[@]}"; do
  task_id=$(grep '^task_id:' "$bundle" | awk '{print $2}')
  ./bin/tests/ralph-cursor-loop.sh --bundle "$bundle" \
    2>&1 | tee "$LOG_DIR/worker-${task_id}.log" &
  pids+=($!)
done
for pid in "${pids[@]}"; do wait "$pid"; done

# Phase 3: Closeout
./bin/tests/ralph-closeout.sh --log-dir "$LOG_DIR" 2>&1 | tee "$LOG_DIR/closeout.log"
echo "Pipeline done. Logs: $LOG_DIR" >&2
```

---

## Phase 4 — Subagent Worker

**Write scope:** `.agents/skills/`

### 4a. `loop-worker` skill (`.agents/skills/loop-worker/`)

A new skill designed to be invoked either:
- By a human: `/loop-worker` to run as a worker in the current session
- By the orchestrator: via `Agent(prompt=worker_prompt + bundle_path)` (Pattern E)

`SKILL.md` description: "Bounded implementation worker for a delegated task. Reads a bundle,
implements write_scope, runs /iteration-close. Designed for cold-start invocation with only
the bundle path as context."

`instructions/startup.md` — 3-step startup (bundle → workflow tasks → git status)
`instructions/workflow.md` — implementation + closeout (worker-scoped version of iteration-close workflow)
`instructions/gotchas.md` — worker-specific: "do not run workflow orient/next", "stay in write_scope", "merge-back is your exit, not advance"

### 4b. Orchestrator invocation pattern in `orchestrator-session-start`

Add to `orchestrator-session-start/instructions/workflow.md` step 4 (fanout decision):
```
### Pattern E: Native subagent (Claude Code Agent tool)

After workflow fanout creates the bundle, the orchestrator can spawn a worker as a native
Claude Code subagent instead of shelling out to ralph-cursor-loop.sh:

Agent(
  description="Implement <task_id> in <plan_id>",
  prompt="""
Delegation bundle: <absolute_bundle_path>
Worker skill: .agents/skills/loop-worker/

Read the bundle. It contains write_scope, task_id, plan_id, feedback_goal, and context_files.
Load the worker skill at the path above.
Implement the single task within write_scope only.
Run /iteration-close when done.
""",
  mode="auto"
)

Use Pattern E for:
- Tasks ≤ 5 files in write_scope (cold start cost is justified for clean isolation)
- When you want guaranteed role separation (orchestrator cannot accidentally continue implementing)

Use ralph-cursor-loop.sh (script worker) for:
- Tasks requiring multiple agent sessions (long work, > 30 min)
- Headless/batch runs without an interactive Claude Code session
```

### 4c. Metrics capture

To compare Pattern E subagents vs script workers, capture per-run:
- Context tokens consumed (from `--output-format json` → `usage.input_tokens`)
- Iteration count to reach merge-back
- Merge-back verification status (pass/partial/fail)
- Files changed vs write_scope size ratio (scope discipline)
- `persisted_via_workflow_commands` rate (was iteration-close run?)

Log these to `.ralph-loop-streams/run-<timestamp>/metrics.json` from both the pipeline script
and the Pattern E orchestrator session.

---

## Acceptance Criteria

- Phase 1: `orchestrator-session-start` loads gotchas before workflow; `active.loop.md` has
  no closeout steps beyond "run /iteration-close"; reconciliation iteration type is documented;
  `iteration-close` auto-folds tool-bugs; `agent-start` suppression note present.
- Phase 2: `active.loop.md` is ~100 lines, worker-only (no wave selection, no oracle chain,
  no scenario coverage tables). `orchestrator.loop.md` exists with the extracted orchestrator
  content. Ralph fanout calls pass the correct overlay per role.
- Phase 3: Four scripts exist in `bin/tests/`. `ralph-cursor-loop.sh --bundle <path>` works;
  without `--bundle` it errors with a helpful message. `ralph-pipeline.sh` runs E2E without
  manual intervention when at least one unblocked task exists.
- Phase 4: `loop-worker` skill exists. Orchestrator instructions document Pattern E invocation.
  A cold-start test (worker spawned with only bundle + skill path, no other context) succeeds
  without running workflow orient.
