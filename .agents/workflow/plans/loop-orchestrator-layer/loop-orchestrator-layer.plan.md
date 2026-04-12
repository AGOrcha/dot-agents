# Loop Orchestrator Layer

Status: In Progress
Last updated: 2026-04-12
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
- [ ] Phase 5 — Auto-route code-structure intents in `workflow graph query` to kg bridge; add tests and spec doc
- [ ] Phase 6 — Implement `workflow fold-back create/list` with small vs proposal routing
- [ ] Phase 7 — Reconcile completed delegations, merge-backs, and completed plans
- [ ] Phase 8 — Add per-delegate prompt and prompt-file inputs to orchestrator fanout bundles

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

**Goal:** `workflow graph query --intent <code-structure-intent> <query>` auto-routes to the kg bridge via subprocess instead of returning an error. Tests and spec doc land alongside the code change.

**File:** `commands/workflow.go`, function `runWorkflowGraphQuery` (line 3017–3019)

**Current behavior** (lines 3017–3019):
```go
if isWorkflowGraphCodeBridgeIntent(intent) {
    return fmt.Errorf("workflow graph query does not handle code-structure intent %q; use 'dot-agents kg bridge query --intent %s' instead", intent, intent)
}
```

**New behavior:** replace the error with a subprocess routing call:
```go
if isWorkflowGraphCodeBridgeIntent(intent) {
    dotAgents, err := os.Executable()
    if err != nil {
        dotAgents = "dot-agents"
    }
    kgArgs := []string{"kg", "bridge", "query", "--intent", intent}
    if scope != "" {
        kgArgs = append(kgArgs, "--scope", scope)
    }
    kgArgs = append(kgArgs, args...)  // positional query words
    c := exec.Command(dotAgents, kgArgs...)
    c.Stdout = os.Stdout
    c.Stderr = os.Stderr
    return c.Run()
}
```

**New tests in `commands/workflow_test.go`:**
- `TestWorkflowGraphQueryCodeStructureRoutesToKGBridge`: intent is a code-structure intent (use one from `isWorkflowGraphCodeBridgeIntent`); assert the error message is NOT the old "use dot-agents kg bridge query" text (the error should be from the subprocess, not the guard). Use a helper that captures stderr or checks exec behavior. If a full integration test is too heavy, at minimum add a unit test that confirms `isWorkflowGraphCodeBridgeIntent` returns true for all expected code-structure intents and false for all KG intents.
- `TestWorkflowGraphQueryKGBridgeIntentsNotRouted`: for each valid KG intent (plan_context, decision_lookup, entity_context, workflow_memory, contradictions), confirm `isWorkflowGraphCodeBridgeIntent` returns false.

**File:** `docs/LOOP_ORCHESTRATION_SPEC.md`

Add a new section after the existing "Decision" section:

```markdown
## KG-First Query Routing

`workflow graph query` is the single query entry point for orchestrator agents. It routes by intent type:

| Intent | Routing | Backing system |
|--------|---------|----------------|
| plan_context | LocalGraphAdapter | ~/.agents knowledge notes |
| decision_lookup | LocalGraphAdapter | ~/.agents knowledge notes |
| entity_context | LocalGraphAdapter | ~/.agents knowledge notes |
| workflow_memory | LocalGraphAdapter | ~/.agents knowledge notes |
| contradictions | LocalGraphAdapter | ~/.agents knowledge notes |
| symbol_lookup | kg bridge subprocess | CRG SQLite/Postgres |
| impact_radius | kg bridge subprocess | CRG SQLite/Postgres |
| call_graph | kg bridge subprocess | CRG SQLite/Postgres |
| community_summary | kg bridge subprocess | CRG SQLite/Postgres |

Agents should not use `grep` or `glob` to answer code-structure questions that are in the second tier of the table above. Call `workflow graph query --intent <intent> <query>` and let the routing layer handle dispatch.
```

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

---

## Phase 7: Completed delegation and plan closeout

**Goal:** completed delegations and merge-backs should not remain forever in active state, and plan completion should reconcile cleanly once a parent accepts the delegated result.

**Problem:** today `workflow merge-back` writes a merge-back artifact and marks the delegation contract `completed`, but there is no parent-driven closeout step that:

- consumes the merge-back as integrated
- archives processed active artifacts into history
- advances the canonical task to `completed` or `failed`
- closes the parent plan when the last task lands
- keeps `workflow orient` / `workflow status` from reporting already-processed merge-backs forever

**Direction:**

- add a closeout/reconcile command, for example:

```bash
dot-agents workflow delegation closeout --plan <id> --task <id> --decision accept
dot-agents workflow delegation closeout --plan <id> --task <id> --decision reject --note "needs follow-up"
```

- treat the closeout step as the parent-agent acknowledgment that the delegated work was integrated
- move processed artifacts out of `.agents/active/` into a durable history location
- update canonical task state from the closeout decision
- if all tasks are complete, update `PLAN.yaml` status and clear or rewrite `current_focus_task`

**Acceptance shape:**

- completed delegation contracts no longer show up as live operational clutter
- merge-back counts reflect unintegrated work only
- completed plans do not retain stale active delegation state
- history preserves the contract and merge-back trail for later review

---

## Phase 8: Per-delegate prompt and file inputs

**Goal:** the orchestrator/delegation flow should be able to hand each sub-agent not just a task and write scope, but also a prompt payload assembled from inline text and/or files.

**Problem:** the current orchestration skill tells the parent how to select and fan out work, but it does not model how a prompt bundle is passed into the delegated sub-agent. That makes prompt shaping ad hoc and hard to reproduce.

**Direction:**

Extend the delegation contract or a sibling bundle artifact so each fanout can persist:

- inline prompt text
- one or more prompt files
- one or more context/resource files
- optional per-delegate override data tied to the specific delegation owner

Candidate command shape:

```bash
dot-agents workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --owner <delegate-name> \
  --write-scope "commands/,internal/platform/" \
  --prompt "Focus on command readback only" \
  --prompt-file .agents/prompts/command-readback.md \
  --context-file docs/LOOP_ORCHESTRATION_SPEC.md \
  --context-file .agents/workflow/plans/loop-orchestrator-layer/TASKS.yaml
```

**Rules:**

- prompt/file inputs must be delegation-specific so different sub-agents can receive different bundles
- repeatable file flags are preferable to one giant comma-separated string
- the contract should store what was handed to the sub-agent so the parent can reproduce or audit the handoff later
- skills should read from the persisted bundle, not reconstruct it from memory

**Acceptance shape:**

- a parent can supply prompt text inline or by file
- a parent can attach multiple context files
- two different delegated sub-agents can receive different prompt/file inputs without colliding
- the resulting bundle is inspectable from the repo artifacts
