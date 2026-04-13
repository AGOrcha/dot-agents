# Loop Orchestrator Layer

Status: Completed
Last updated: 2026-04-13
Depends on:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md`
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`

## Goal

Add a planner/orchestrator layer above the focused loop agent so work selection, safe parallel fanout, and fold-back happen through deterministic dot-agents artifacts instead of prompt improvisation.

## Decisions

- Build this as a mixed system: command surfaces + skills + existing delegation contracts + light hooks.
- Reuse canonical `PLAN.yaml` / `TASKS.yaml` and derive the dependency graph instead of hand-maintaining another graph file.
- Keep loop agents focused on one bounded slice.
- Keep high-risk shared-behavior changes behind proposal review.

## Current Slice

- [x] Write the orchestrator operating model and command/artifact direction in `docs/LOOP_ORCHESTRATION_SPEC.md`
- [x] Add `workflow next` as the first read-only task-selection primitive
- [x] Create a repo-local `orchestrator-session-start` skill that chains the existing workflow surfaces
- [x] Add `workflow plan graph` so the orchestrator can inspect cross-plan/task dependencies directly
- [x] Phase 3B - add `SLICES.yaml` support for safe parallel sub-task decomposition
- [x] Phase 3C - add fanout-from-slice support on top of existing delegation contracts
- [x] Phase 4 — Wire `workflow fanout --slice <id>` to resolve task and write-scope from SLICES.yaml
- [x] Phase 5 — Auto-route code-structure intents in `workflow graph query` to kg bridge; add tests and spec doc
- [x] Phase 6 — Implement `workflow fold-back create/list` with small vs proposal routing
- [x] Phase 7 — Reconcile completed delegations, merge-backs, and completed plans
- [x] Phase 8 — Add per-delegate prompt and prompt-file inputs to orchestrator fanout bundles

## Phase 4: Slice-based fanout

**Goal:** `workflow fanout --plan <id> --slice <slice-id>` resolves `--task` and `--write-scope` from SLICES.yaml automatically, reducing manual bookkeeping.

**File:** `commands/workflow.go`

**Changes to `NewWorkflowCmd()` (around line 428–433):**
1. Add flag: `fanoutCmd.Flags().String("slice", "", "Slice ID from plan SLICES.yaml; auto-fills --task and --write-scope from slice metadata")`
2. Remove the existing `_ = fanoutCmd.MarkFlagRequired("task")` line; replace with runtime mutual-exclusion check in `runWorkflowFanout`.

**Changes to `runWorkflowFanout()` (around line 3357):**
Add immediately after reading `taskID` and `writeScopeCSV` from flags:
```go
sliceID, _ := cmd.Flags().GetString("slice")
if sliceID != "" && taskID != "" {
    return fmt.Errorf("provide --slice or --task, not both")
}
if sliceID != "" {
    sf, err := loadCanonicalSlices(project.Path, planID)
    if err != nil {
        return fmt.Errorf("load slices for plan %s: %w", planID, err)
    }
    var found *CanonicalSlice
    for i := range sf.Slices {
        if sf.Slices[i].ID == sliceID {
            found = &sf.Slices[i]
            break
        }
    }
    if found == nil {
        return fmt.Errorf("slice %q not found in plan %s", sliceID, planID)
    }
    if found.Status == "completed" {
        return fmt.Errorf("slice %q is already completed", sliceID)
    }
    taskID = found.ParentTaskID
    if writeScopeCSV == "" {
        writeScope = found.WriteScope  // []string, skip CSV split
    }
}
if taskID == "" {
    return fmt.Errorf("provide --slice <slice-id> or --task <task-id>")
}
```
Note: the existing `writeScope` population from `writeScopeCSV` is a CSV split loop further down; when populating from slice, assign `writeScope` directly before that loop runs (or skip the loop when already populated).

**New tests in `commands/workflow_test.go`:**
- `TestFanoutFromSlice`: temp project dir with `PLAN.yaml` (plan `p1`, status active), `TASKS.yaml` (task `t1`, status pending, write_scope `["commands/"]`), `SLICES.yaml` (slice `s1`, parent_task_id `t1`, write_scope `["commands/"]`, status in_progress); run `workflow fanout --plan p1 --slice s1 --owner test`; assert delegation contract at `.agents/active/delegation/del-t1-*.yaml` has `parent_task_id: t1` and `write_scope: [commands/]`.
- `TestFanoutSliceAndTaskMutuallyExclusive`: pass both `--slice s1` and `--task t1`; assert error contains "not both".
- `TestFanoutSliceNotFound`: pass `--slice nonexistent`; assert error contains "not found".
- `TestFanoutSliceAlreadyCompleted`: slice with `status: completed`; assert error contains "already completed".

---

## Phase 5: KG-first graph query routing

**Status:** Implemented.

**Goal:** `workflow graph query --intent <code-structure-intent> <query>` auto-routes to the kg bridge via subprocess instead of returning the old guard error. Tests and spec live alongside the implementation.

**Code:** `commands/workflow.go`

- `runWorkflowGraphQuery` — if `isWorkflowGraphCodeBridgeIntent(intent)`, delegates to `runWorkflowGraphQueryViaKGBridge` before loading `graph-bridge.yaml` (code-structure queries do not require the workflow-local bridge to be enabled).
- `runWorkflowGraphQueryViaKGBridge` — resolves the `dot-agents` binary via `workflowDotAgentsExe` (tests swap this for a freshly built CLI), runs `kg bridge query --intent <intent> [<args...>]`, sets `cmd.Dir` to the project cwd, pipes stdout/stderr, and prepends `--json` to the child argv when the parent CLI has JSON output enabled (`Flags.JSON`).

**Tests:** `commands/workflow_test.go` — `TestWorkflowGraphQueryCodeStructureRoutesToKGBridge`, `TestWorkflowGraphQueryKGBridgeIntentsNotRouted`, plus `TestRunWorkflowGraphQueryAllowsWorkflowBridgeIntent` for the note-oriented path.

**Spec:** `docs/LOOP_ORCHESTRATION_SPEC.md` — subsection **KG-First Query Routing** under **KG / CRG Direction**.

---

## Phase 6: Fold-back reconciliation

**Goal:** `workflow fold-back create/list` routes low-risk loop observations into the correct durable artifact without requiring the orchestrator to manually edit TASKS.yaml or create proposal files.

**New subcommand structure in `commands/workflow.go`:**
```
workflow fold-back create --plan <id> [--task <id>] --observation "text" [--propose]
workflow fold-back list [--plan <id>]
```

**Fold-back artifact schema** written to `.agents/active/fold-back/{id}.yaml`:
```yaml
schema_version: 1
id: fold-{unix-timestamp}
plan_id: loop-orchestrator-layer
task_id: phase-4-fanout-from-slices   # empty string when --task not provided
observation: "the observation text"
classification: small                  # small|proposal
routed_to: "task_note:loop-orchestrator-layer/phase-4-fanout-from-slices"
                                       # or "proposal:obs-{timestamp}.md"
created_at: "2026-04-12T00:00:00Z"
```

**Routing rules:**
- Without `--propose` flag: `classification = "small"`; append observation text as a new bullet to the matching task's `Notes` field in TASKS.yaml (`saveCanonicalTasks`); create fold-back artifact with `routed_to = "task_note:{plan_id}/{task_id}"`. If `--task` not provided, append to the plan's top-level notes instead (update `plan.Summary` with a `\n- {observation}` suffix and call `saveCanonicalPlan`).
- With `--propose` flag: `classification = "proposal"`; write `~/.agents/proposals/obs-{unix-timestamp}.md` with YAML frontmatter (`title`, `observation`, `plan_id`, `task_id`, `created_at`) followed by the observation text as the body; create fold-back artifact with `routed_to = "proposal:obs-{timestamp}.md"`. Do NOT modify TASKS.yaml.

**`workflow fold-back list`** behavior:
- Read all `*.yaml` files under `.agents/active/fold-back/` in the current project.
- Render a table: ID | Plan | Task | Classification | Routed-to | Created-at.
- If `--plan <id>` provided, filter to that plan only.
- If no artifacts found, print "No fold-back observations recorded."

**Changes to `NewWorkflowCmd()` (around line 468):**
```go
foldBackCmd := &cobra.Command{Use: "fold-back", Short: "Route loop observations into durable plan artifacts or proposals"}
foldBackCreateCmd := &cobra.Command{Use: "create", Short: "Record and route a loop observation", RunE: runWorkflowFoldBackCreate}
foldBackCreateCmd.Flags().String("plan", "", "Canonical plan ID (required)")
foldBackCreateCmd.Flags().String("task", "", "Task ID to append note to (optional)")
foldBackCreateCmd.Flags().String("observation", "", "Observation text (required)")
foldBackCreateCmd.Flags().Bool("propose", false, "Route as proposal rather than inline task note")
_ = foldBackCreateCmd.MarkFlagRequired("plan")
_ = foldBackCreateCmd.MarkFlagRequired("observation")
foldBackListCmd := &cobra.Command{Use: "list", Short: "List recorded fold-back observations", RunE: runWorkflowFoldBackList}
foldBackListCmd.Flags().String("plan", "", "Filter by canonical plan ID")
foldBackCmd.AddCommand(foldBackCreateCmd, foldBackListCmd)
```
Add `foldBackCmd` to the final `cmd.AddCommand(...)` call at line 468.

**New functions in `commands/workflow.go`:**
- `runWorkflowFoldBackCreate(cmd *cobra.Command, _ []string) error`
- `runWorkflowFoldBackList(cmd *cobra.Command, _ []string) error`
- `writeFoldBackArtifact(projectPath string, artifact foldBackArtifact) error` (writes YAML to `.agents/active/fold-back/{id}.yaml`)
- `type foldBackArtifact struct { SchemaVersion int; ID string; PlanID string; TaskID string; Observation string; Classification string; RoutedTo string; CreatedAt string }`

**New tests in `commands/workflow_test.go`:**
- `TestFoldBackCreateSmall`: temp project with PLAN.yaml and TASKS.yaml (task `t1` with notes "existing"); run `fold-back create --plan p1 --task t1 --observation "new obs"`; assert TASKS.yaml task `t1` Notes field now contains "new obs"; assert `.agents/active/fold-back/fold-*.yaml` artifact exists with classification `small`.
- `TestFoldBackCreateNoTask`: `fold-back create --plan p1 --observation "plan-level obs"` (no --task); assert plan Summary updated; fold-back artifact exists.
- `TestFoldBackCreatePropose`: `fold-back create --plan p1 --task t1 --observation "big change" --propose`; assert TASKS.yaml task Notes NOT modified; assert `~/.agents/proposals/obs-*.md` created; fold-back artifact has classification `proposal`.
- `TestFoldBackList`: create two fold-back artifacts for different plans; run `fold-back list`; assert both appear; run `fold-back list --plan p1`; assert only p1 artifact appears.

---

## Notes

- `workflow next` should prefer canonical task state over checkpoint text.
- Phase 3B/3C is the current plan/docs reconciliation lane: `SLICES.yaml` is the canonical slice artifact, and `workflow fanout` remains the readiness gate for non-overlapping delegation.
- Write-scope conflict prevention already exists in `workflow fanout`; Phase 4 adds the missing slice-resolution layer.
- Hooks should validate stale or drifting orchestration state, not choose work.
- **Phase 8 follow-ups** (see `TASKS.yaml` phase-8 notes): optional bundle JSON-schema at save; optional CLI tie-in for `~/.agents/profiles/<profile>.md`; optional repo example files for spec parity; global `loop-worker` profile lives at `~/.agents/profiles/loop-worker.md` (operator-local). Skills `orchestrator-session-start`, `delegation-lifecycle`, and symlinked `iteration-close` align with bundle-first and delegated closeout.

---

## Phase 7: Completed delegation and plan closeout

**Goal:** completed delegations and merge-backs should not remain forever in active state, and plan completion should reconcile cleanly once a parent accepts the delegated result.

**Problem:** today `workflow merge-back` writes a merge-back artifact and marks the delegation contract `completed`, but there is no parent-driven closeout step that:

- consumes the merge-back as integrated
- archives processed active artifacts into history
- advances the canonical task to `completed` or `failed`
- closes the parent plan when the last task lands
- retires the canonical plan bundle from `.agents/workflow/plans/<id>/` once the plan is fully complete
- keeps `workflow orient` / `workflow status` from reporting already-processed merge-backs forever

**Direction:**

- add a closeout/reconcile command, for example:

```bash
dot-agents workflow delegation closeout --plan <id> --task <id> --decision accept
dot-agents workflow delegation closeout --plan <id> --task <id> --decision reject --note "needs follow-up"
```

- treat the closeout step as the parent-agent acknowledgment that the delegated work was integrated
- move processed artifacts out of `.agents/active/` into a durable history location
- standardize the archive path under the owning plan instead of a repo-wide catchall:
  `.agents/history/<plan-id>/<optional-subplan>/delegate-merge-back-archive/<yyyy-mm-dd>/<task-or-slice>/`
- treat any legacy flat archive such as `.agents/history/delegation-merge-back-archive/...` as transitional maintenance debt to be folded into the owning plan history tree
- update canonical task state from the closeout decision
- if all tasks are complete, update `PLAN.yaml` status and clear or rewrite `current_focus_task`
- distinguish `completed` from `archived`: `completed` stays in `.agents/workflow/plans/<id>/` until final verification, fold-back, and cleanup are done; `archived` means the canonical bundle has moved to `.agents/history/<plan-id>/plan-archive/<yyyy-mm-dd>/`
- when archiving, preserve `PLAN.yaml`, `TASKS.yaml`, optional `SLICES.yaml`, and the narrative plan doc; stamp the archived copy `status: archived`; remove the source bundle from `.agents/workflow/plans/<id>/`

**Acceptance shape:**

- completed delegation contracts no longer show up as live operational clutter
- merge-back counts reflect unintegrated work only
- completed plans do not retain stale active delegation state
- history preserves the contract and merge-back trail for later review
- closeout history is colocated with the relevant plan history so later review does not require cross-referencing a global delegation archive
- fully retired plans no longer stay in `.agents/workflow/plans/`; plan-owned history contains the final archived canonical bundle

---

## Phase 8: Per-delegate prompt, context, and worker bundles

**Goal:** the orchestrator/delegation flow should hand each sub-agent a reproducible worker bundle: stable worker profile, repo-local project overlay, and task-specific prompt/context/verification payload.

**Problem:** the current orchestration skill can select work and fan out a bounded task, but it does not model the handoff as a persisted artifact. That makes worker behavior, prompt shaping, verification expectations, and closeout steps too ad hoc to reuse across repos.

**Direction:**

Adopt a 3-layer model:

1. **Global worker profile** under `~/.agents/`
   - reusable `loop-worker` behavior: honor `write_scope`, trust canonical tasks, run focused tests first, record `workflow verify record`, `workflow checkpoint`, return `workflow merge-back`, and leave `workflow advance` / delegation closeout to the parent.
2. **Project overlay** in the repo
   - repo-local loop prompt / guidance file(s) that define plan locations, quality gates, regression matrix path, higher-layer validation queue path, and project-specific verification surfaces.
3. **Delegation bundle** in repo artifacts
   - sibling artifact to the delegation contract at `.agents/active/delegation-bundles/<delegation-id>.yaml`, backed by `schemas/workflow-delegation-bundle.schema.json`.

The bundle should persist:

- worker profile reference and project overlay file(s)
- selected plan/task/slice and selection reason
- inline prompt text and prompt files
- context files
- reusable testing metadata:
  - `feedback_goal`
  - `scenario_tags`
  - `regression_artifacts`
  - `higher_layer_validation_queue`
  - evidence `classification` expectations
  - `sandbox_mutations` policy
- closeout expectations for worker vs parent responsibilities

Candidate command shape:

```bash
dot-agents workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --owner <delegate-name> \
  --write-scope "commands/,internal/platform/" \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "Does fold-back create/list persist small and proposal routes cleanly?" \
  --scenario-tag canonical-plan-present \
  --scenario-tag workflow-fold-back-small \
  --regression-artifact .agents/workflow/testing-matrix.yaml \
  --validation-queue .agents/active/live-testing-queue.md \
  --prompt "Implement only the selected task and keep write-scope tight" \
  --prompt-file .agents/prompts/loop-worker.project.md \
  --context-file .agents/workflow/plans/loop-orchestrator-layer/TASKS.yaml \
  --context-file docs/LOOP_ORCHESTRATION_SPEC.md
```

**Rules:**

- the stable worker profile should not be redefined per repo; repos should customize through project overlays
- prompt/context inputs must be delegation-specific so different sub-agents can receive different bundles
- repeatable file flags are preferable to one giant comma-separated string
- the persisted bundle, not terminal memory, should be the source of truth for what the worker received
- negative-path coverage is required when the delegated change introduces new failure modes
- worker closeout must include `workflow verify record`, `workflow checkpoint`, and `workflow merge-back`
- parent closeout must include canonical `workflow advance` and delegation closeout/archive once the merge-back is accepted

**Acceptance shape:**

- a parent can pick a reusable worker profile and attach repo-local overlays
- a parent can supply prompt text inline or by file and attach multiple context files
- a delegated worker receives reproducible verification metadata, not just prose instructions
- two different delegated sub-agents can receive different prompt/context/testing bundles without colliding
- the resulting bundle is inspectable from repo artifacts and backed by a schema embedded into the binary
