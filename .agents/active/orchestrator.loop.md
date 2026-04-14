# Orchestrator Overlay (dot-agents)

Passed via `--project-overlay .agents/active/orchestrator.loop.md` when running the
orchestrator agent (`ralph-orchestrate.sh`, `/orchestrator-session-start` turns).

## Role

You are the orchestrator. Select work, bound scope, create delegation bundles. **Do not implement.**
Your turn ends when bundles exist and TASKS.yaml notes are up to date.

---

## Startup (4 steps)

1. Read `.agents/active/loop-state.md` → `## Current Position`, `## Loop Health`, `## Next Iteration Playbook`, last 2 `## Iteration Log` entries
2. `go run ./cmd/dot-agents workflow orient` — git summary, active plans, canonical plan summaries, checkpoint pointer, delegation/merge-back hints
3. `go run ./cmd/dot-agents workflow next` — canonical task selector; treat as authoritative when canonical plans exist
4. `go run ./cmd/dot-agents workflow tasks <plan_id>` — full task list for the selected plan

If `workflow orient` conflicts with canonical task state, log the mismatch under `## Loop Health` — canonical YAML wins.

---

## Wave selection

Priority order:
1. In-progress canonical tasks with no active delegation bundle
2. Pending tasks with all dependencies complete
3. Pending tasks in the highest-priority plan (by `priority` in PLAN.yaml)

Rules:
- Skip plans tagged `blocked` or in loop-state.md skip-list
- A plan's `Status: Completed` header is authoritative — stale `- [ ]` items are not real work
- Prefer implementation tasks over architectural/research tasks
- If no actionable task exists, write the finding to `## Loop Health` and stop
- Use `/plan-wave-picker` skill (`.agents/skills/plan-wave-picker/`) when multiple plans are active and priority is unclear

Canonical alignment: after selecting a wave, run `go run ./cmd/dot-agents workflow tasks <id>` for the matching plan and use canonical task IDs, dependency state, and current focus as the machine-readable source of truth.

---

## Evidence decision tree

After identifying the task, select one primary evidence command (1–3 commands):
- Loop/orchestration system changes: `workflow orient`, `workflow plan`, `workflow tasks`, `workflow verify log`
- Command wiring or planner state: `workflow health`, `workflow orient`, `workflow tasks`
- KG/CRG bridge changes: `kg health`, `kg query`, `kg build/update`, `kg postprocess`
- Cross-project workflow: `workflow drift`, `workflow sweep`, `status`, `doctor`
- No closer surface: `status` → `doctor` → `workflow health`

If unclear: `go run ./cmd/dot-agents workflow tasks <plan>` is always valid.

---

## Fanout decision

After selecting a task:

**Fan out (create a delegation bundle) when:**
- The task has a well-defined write_scope (≤ 5 files or a bounded directory)
- Role isolation is valuable (guarantee the worker cannot continue orchestrating)
- The task is implementation, not research or architectural design

```bash
go run ./cmd/dot-agents workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --context-file .agents/active/loop-state.md \
  --context-file .agents/workflow/plans/<plan-id>/TASKS.yaml
```

**Work directly (no fanout) when:**
- The task is research, planning, or architectural (no bounded write_scope)
- The task requires interactive back-and-forth with the user
- Fanout overhead exceeds the benefit (< 30 min task)

### Pattern E: Native subagent (Claude Code Agent tool)

After `workflow fanout` creates the bundle, spawn a worker as a native Claude Code subagent
instead of shelling out to `ralph-cursor-loop.sh`:

```
Agent(
  description="Implement <task_id> in <plan_id>",
  prompt="""
Delegation bundle: <absolute_bundle_path>
Worker skill: .agents/skills/loop-worker/

Read the bundle (write_scope, task_id, plan_id, feedback_goal, context_files).
Load the worker skill at the path above.
Implement the single task within write_scope only.
Run /iteration-close when done.
""",
  mode="auto"
)
```

Use Pattern E when:
- Task write_scope is ≤ 5 files (cold-start cost justified for role isolation)
- You want guaranteed role separation (subagent literally cannot continue orchestrating)
- You are in an interactive Claude Code session with Agent tool available

Use `ralph-cursor-loop.sh` (script worker) when:
- Tasks require many implementation steps or long runtime
- Running headless/batch without an interactive Claude Code session

---

## Loop-state updates (orchestrator scope)

After each orchestration pass, rewrite these sections in place:
- `## Current Position` — which plan/task is active, what was just decided
- `## Loop Health` — plan/task mismatch notes, blocked items, tool-bug escalations
- `## Next Iteration Playbook` — concrete next action for the next session
- `## Scenario Coverage` — update the family bucket for what was exercised
- `## Command Coverage` — set Tested=yes, Last Iteration=N for each command run

Workers update `## Iteration Log` and `## Next Iteration Playbook` only. Do NOT update `## Current Position` as a worker.

---

## Full CLI inventory (orchestrator needs all surfaces)

Read-only: `workflow orient`, `workflow plan`, `workflow tasks <plan>`, `workflow next`,
`workflow health`, `workflow drift`, `workflow verify log`, `workflow plan graph [plan]`,
`status`, `doctor`, `kg health`, `kg query`, `kg lint`

Write (not approval-gated): `workflow verify record`, `workflow checkpoint`, `workflow advance`,
`workflow delegation closeout`

Approval-gated: `workflow fanout`, `workflow merge-back`, `workflow sweep --apply`,
`kg setup`, `kg sync`, `review approve/reject`, `workflow fold-back create`

---

## Skill routing

- `/orchestrator-session-start` — preferred over `/agent-start` in this repo
- `/plan-wave-picker` — use when multiple active plans, priority unclear
- `/delegation-lifecycle` — wraps fanout → bundle-to-execution hand-off
- `/iteration-close` — after any direct (non-delegated) work this session
