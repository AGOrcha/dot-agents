# Workflow: Orchestrator Session Start

Use this skill when the session needs an architect/product-owner pass before handing work to a focused loop worker.

## Command chain

0. **Run pre-flight checks first.**
   Load -> `instructions/preflight.md`
   Check pending proposals, active delegation bundles, and worker loop context before running any workflow commands. If an active bundle already exists for the task `workflow next` will select, skip steps 1–4 and go directly to `delegation-lifecycle`.

1. Run workflow readback first.

```bash
da workflow orient
da workflow status
da workflow next
```

! da workflow orient
! da workflow status
! da workflow next

Interpretation:

- `workflow orient` is the broad session snapshot
- `workflow status` is checkpoint-backed runtime readback
- `workflow next` is the canonical task selector and should win over stale checkpoint text

2. Inspect the chosen task directly.

```bash
da workflow plan
da workflow tasks <plan-id>
```

! da workflow plan
! da workflow tasks <plan-id>

If the repo has multiple active plans, keep the selected task grounded in canonical dependencies before reading markdown notes.

3. Prefer graph-backed context before broad repo scans.

For workflow memory or decisions:

```bash
da workflow graph query --intent plan_context "<plan-or-task>"
da workflow graph query --intent workflow_memory "<plan-or-task>"
```

For code-structure shaped questions, prefer KG/CRG commands:

```bash
da kg changes
da kg impact <file-or-symbol>
da kg communities
da kg flows
```

Use `rg` only after the graph surfaces are absent, stale, or insufficient for the exact question.

4. Decide between direct execution and delegated fanout.

Use this decision tree — do not skip to the fanout command before checking the top branch:

```
Does a bundle already exist for the selected task?
├── YES → Go to delegation-lifecycle with the existing bundle. Do not re-fanout.
└── NO  → Can the task be bounded by write_scope AND no active delegation overlaps it?
          ├── YES → workflow fanout  (see command below)
          └── NO  → Direct execution in-session (small changes, single-owner writes)
```

If you are unsure whether to fanout: default to **direct** for changes touching ≤ 5 files with no cross-owner scope; default to **fanout** for anything larger or multi-session.

Fanout creates the delegation contract **and** a persisted bundle (same `delegation_id` as the contract's `id`). Prefer flags that capture profile, overlays, prompts, and verification metadata up front:

```bash
da workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --write-scope "<scope-prefix-1>/,<scope-prefix-2>/" \
  --owner "<worker-name>" \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "<concrete question evidence must answer>" \
  --scenario-tag "<tag>" \
  --regression-artifact .agents/workflow/testing-matrix.yaml \
  --validation-queue .agents/active/live-testing-queue.md \
  --prompt "<inline instruction>" \
  --prompt-file .agents/prompts/loop-worker.project.md \
  --context-file <path/to/spec-or-context-doc> \
  --selection-reason "<why this task now>"
```

Omit flags you do not need; repeatable flags (`--prompt`, `--context-file`, etc.) may appear multiple times.

**Bundle-first handoff:** After fanout, the success box prints `Bundle: .agents/active/delegation-bundles/<delegation_id>.yaml`. Treat that file as the **source of truth** for what the worker must do — do not reconstruct the handoff from chat memory. The worker (or the parent briefing the worker) reads the bundle first, then any referenced overlay, `--prompt-file`, and `--context-file` paths inside the repo.

**Before handing off — write context the worker needs into TASKS.yaml notes:**
Any constraints, risks, or session-context that does not fit the bundle YAML fields (e.g., known pre-existing breakages, dependency ordering, KG findings from step 3, or why this task was chosen now) should be written into the `notes` field of the matching task in `.agents/workflow/plans/<plan-id>/TASKS.yaml`. The worker reads `workflow tasks <plan-id>` at session start; notes there are guaranteed to be seen. Do not rely on chat memory or inline `--prompt` for anything load-bearing.

**Chain `delegation-lifecycle` for the worker turn (required):** This orchestrator pass **stops at** fanout + TASKS.yaml notes update. Implementation + profile-driven execution is **not** step 4 above — it is the next skill:

1. Load **`delegation-lifecycle`** → **`instructions/bundle-to-execution.md`**.
2. Start or brief the **worker** (subagent, new agent session, or headless `agent -p`) with:
   - absolute path to `.agents/active/delegation-bundles/<delegation_id>.yaml`
   - **`~/.agents/profiles/loop-worker.md`** and **`.agents/active/active.loop.md`** (already referenced on the bundle when you used `--delegate-profile loop-worker` and `--project-overlay` / `--prompt-file`)
3. Worker follows **bundle-to-execution** (read bundle → implement inside `write_scope` → verify → checkpoint → merge-back). Parent stays out of large edits until merge-back review.

### Pattern E: Native subagent (Agent tool)

After `workflow fanout` creates the bundle, spawn the worker as a native Claude Code subagent:

```
Agent(
  description="Implement <task_id> in <plan_id>",
  subagent_type="loop-worker",
  prompt="Delegation bundle: <absolute_bundle_path>",
  mode="auto"
)
```

The `loop-worker` sub-agent loads `AGENT.md` as its system prompt — no inlined worker instructions needed. The `prompt` field carries only the bundle path.

**Use Pattern E when:**
- Task write_scope is ≤ 5 files (cold-start cost justified for role isolation)
- You want guaranteed role separation — the subagent literally cannot continue orchestrating
- You are in an interactive Claude Code session with the Agent tool available

**Use a headless loop script (e.g. `ralph-cursor-loop.sh`) when:**
- Tasks require many implementation steps or long runtime
- Running headless/batch without an interactive Claude Code session

If **no fanout** (direct execution in-repo): you may stay in one session for small changes, but still avoid collapsing "choose work" and "implement everything" without bounds—see `instructions/gotchas.md`.

**Closeout (after worker merge-back):** Worker: `workflow verify record`, `workflow checkpoint`, `workflow merge-back`. Parent: `workflow delegation closeout --plan <id> --task <id> --decision accept|reject` after reviewing merge-back. Accepted delegation closeout completes the delegated task; do not also call `workflow advance` for delegated work. Advancement remains for direct, non-delegated work.

5. Fold observations back after the work finishes.

- small repo-local items should update plan notes, tests, matrix rows, or lessons
- cross-cutting or shared-behavior changes should become proposals under `~/.agents/proposals/`
- use `iteration-close` skill after the implementation slice is complete (worker path; aligns with `delegation-lifecycle` closeout)
