# Workflow: Orchestrator Session Start

Use this skill when the session needs an architect/product-owner pass before handing work to a focused loop worker.

## Command chain

0. **Run pre-flight checks first.**
   Load -> `instructions/preflight.md`
   Check pending proposals, active delegation bundles, and worker loop context **before** running any workflow commands. If an active bundle already exists for the task `workflow next` will select, skip steps 1–4 and go directly to `delegation-lifecycle` with the existing bundle path.

1. Run workflow readback.

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

   Canonical plans live at `.agents/workflow/plans/<plan-id>/` (`PLAN.yaml` + `TASKS.yaml` + `<plan-id>.plan.md`). The `.agents/active/` directory is reserved for transient runtime artifacts (delegation bundles, merge-back, fold-back, verification, iteration logs) — it does NOT hold plans.

2. Inspect the chosen task directly.

   ```bash
   da workflow plan
   da workflow plan show <plan-id>
   da workflow tasks <plan-id>
   ```

   ! da workflow plan
   ! da workflow tasks <plan-id>

   If the repo has multiple active plans, keep the selected task grounded in canonical dependencies before reading markdown notes. The plan markdown is descriptive only — `PLAN.yaml` / `TASKS.yaml` are authoritative.

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

4. Pre-fanout sanity checks (run for every candidate task before deciding fanout vs. direct).

   These prevent the most common wasted-spawn patterns:

   a. **Cross-check task status vs shipped PRs.** `workflow eligible` reports `status: pending|in_progress` from TASKS.yaml, which drifts behind merged PRs after parallel-worker batches:
      ```bash
      gh pr list --state merged --search "<task-id>" --limit 5
      git log --oneline --all | grep -i "<task-id>" | head -5
      ```
      If the work shipped, run `da workflow delegation closeout --plan <plan> --task <task> --decision accept` instead of fanout (this advances status AND archives the contract in one step — do not also call `workflow advance`).

   b. **Validate write_scope against HEAD.** Confirm every file in the proposed `--write-scope` exists on the current tree. Then enumerate symbol callers via the code graph + `grep -rln '<symbol>\b'` to catch test-file or cross-package callers the static plan notes missed. Add missed-caller files to write_scope upfront rather than forcing the worker into a fold-back.

   c. **Confirm no overlapping active delegation.** `ls .agents/active/delegation-bundles/` — if one already exists for this task, do not re-fanout. Hand off to `delegation-lifecycle` with that bundle path.

5. Decide between direct execution and delegated fanout.

   ```
   Does a bundle already exist for the selected task?
   ├── YES → Go to delegation-lifecycle with the existing bundle. Do not re-fanout.
   └── NO  → Can the task be bounded by write_scope AND no active delegation overlaps it?
             ├── YES → workflow fanout  (see command below)
             └── NO  → Direct execution in-session (small changes, single-owner writes)
   ```

   If you are unsure: default to **direct** for changes touching ≤ 5 files with no cross-owner scope; default to **fanout** for anything larger or multi-session.

   Fanout creates the delegation contract **and** a persisted bundle (same `delegation_id` as the contract's `id`). Prefer flags that capture profile, overlays, prompts, verifier sequence, and verification metadata up front:

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
     --verifier-sequence "<id1>,<id2>"   # optional override; defaults from app_type_verifier_map
     --verifier-retry-max 3              # optional cap on verifier auto-fix iterations
     --selection-reason "<why this task now>"
   ```

   Omit flags you do not need; repeatable flags (`--prompt`, `--context-file`, `--scenario-tag`, etc.) may appear multiple times.

   For direct (non-delegated) orchestrator work that still needs a contract for the audit trail, use `da workflow contract create --plan <id> --task <id> --direct --write-scope <...>` instead.

   **Bundle-first handoff:** After fanout, the success box prints `Bundle: .agents/active/delegation-bundles/<delegation_id>.yaml`. Treat that file as the **source of truth** for what the worker must do — do not reconstruct the handoff from chat memory. The worker reads the bundle first, then any referenced overlay, `--prompt-file`, and `--context-file` paths inside the repo.

   **Before handing off — write context the worker needs into TASKS.yaml notes:**
   Any constraints, risks, or session-context that does not fit the bundle YAML fields (e.g. known pre-existing breakages, dependency ordering, KG findings from step 3, or why this task was chosen now) should be written into the `notes` field of the matching task in `.agents/workflow/plans/<plan-id>/TASKS.yaml`. The worker reads `workflow tasks <plan-id>` at session start; notes there are guaranteed to be seen. Do not rely on chat memory or inline `--prompt` for anything load-bearing.

   **Chain `delegation-lifecycle` for the worker turn (required):** This orchestrator pass **stops at** fanout + TASKS.yaml notes update. Implementation + profile-driven execution is **not** step 5 above — it is the next skill:

   1. Load **`delegation-lifecycle`** → **`instructions/bundle-to-execution.md`**.
   2. Start or brief the **worker** (subagent, new agent session, or headless batch run) with:
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

The `loop-worker` subagent loads `AGENT.md` as its system prompt — no inlined worker instructions needed. The `prompt` field carries only the bundle path.

**Use Pattern E when:**
- Task write_scope is bounded (cold-start cost justified for role isolation)
- You want guaranteed role separation — the subagent literally cannot continue orchestrating
- You are in an interactive session with the Agent tool available

**Use a headless loop script when:**
- Tasks require many implementation steps or long runtime
- Running headless/batch without an interactive session

**Use `general-purpose` (NOT `loop-worker`) when:**
- The work has no canonical task / bundle (cross-cutting hygiene, ad-hoc cleanups)
- The scope is "every file flagged for X" rather than an enforceable `write_scope`
- A `loop-worker` will REFUSE such work by design — that refusal is correct

If **no fanout** (direct execution in-repo): you may stay in one session for small changes, but still avoid collapsing "choose work" and "implement everything" without bounds — see `instructions/gotchas.md`.

### Verifier and reviewer chain (between worker and parent closeout)

If the project registers verifier_profiles in `.agentsrc.json` (e.g. `pr-ci` for PR/CI/SAST watching), the chain is:

1. **Impl worker** finishes at `merge-back` — does NOT poll CI or auto-fix mechanical issues itself.
2. **Verifier profile** (dispatched per `verifier_profiles` + `app_type_verifier_map`) polls CI + SAST/coverage gates to terminal state, auto-fixes mechanical issues (coverage gap → focused test, cog complexity → extract helper, dup literal → const, stale allowlist → prune), and writes `.agents/active/verification/<task-id>/<profile>.result.yaml`.
3. **Lens reviewers** (architecture-standards / acceptance-invariants / adversarial) audit pre-merge on demand for non-trivial slices — load on the same task-local evidence the verifier produced.
4. **Parent** runs `workflow delegation gate` (optional evidence-summary) then `workflow delegation closeout --decision accept|reject|escalate` — which auto-advances task status (do NOT also call `workflow advance` for delegated work).

If no verifier_profile is registered, the impl worker owns the readiness loop itself (fallback mode). See `delegation-lifecycle` for the full handoff.

**Closeout summary (after worker merge-back):**
Worker: `workflow verify record`, `workflow checkpoint`, `workflow merge-back --commit-state`.
Parent: `workflow delegation closeout --plan <id> --task <id> --decision accept|reject|escalate` after reviewing merge-back. Accepted closeout completes the delegated task; do not also call `workflow advance` for delegated work. `workflow advance` remains for direct, non-delegated work.

6. Fold observations back after the work finishes.

- Small repo-local items should update plan notes, tests, matrix rows, or lessons under `.agents/lessons/<name>/LESSON.md`.
- Cross-cutting or shared-behavior changes should become proposals under `~/.agents/proposals/` (formal YAML) for global scope, or `.agents/proposals/<id>.md` (markdown) for project-local scope.
- Use the `iteration-close` skill after the implementation slice is complete (worker path; aligns with `delegation-lifecycle` closeout).
