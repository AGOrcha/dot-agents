# `da workflow` — Exhaustive Engine Reference

> Scope: the `da workflow` command family — the repository-local workflow engine a swarm
> will drive. Source: `github.com/AGOrcha/dot-agents`, repo `~/proj-docs/dot-agents`,
> package `commands/workflow/**`. Installed binary **`da version 0.4.2`** (tag `v0.4.2`,
> commit `c1245e55`, 2026-06-29). Repo HEAD at read time: `917908d4`.
> Every claim is cited `file:line` against `commands/workflow/*` unless noted.

## 0. Binary-vs-source fidelity note (READ FIRST for swarm design)

- The workflow code already lived in the `commands/workflow/` **sub-package** at tag `v0.4.2`
  (verified: `git ls-tree v0.4.2 -- commands/workflow/` lists `plan_task.go`,
  `eligible_accounting.go`, etc.). The dep-gating predicate `depSatisfiesDownstream` and
  cross-plan resolver `incompleteCanonicalDependenciesCrossplan` are **present and identical**
  in `v0.4.2` (`git show v0.4.2:commands/workflow/eligible_accounting.go` line 72;
  `git show v0.4.2:commands/workflow/plan_task.go` lines 1737/1814). So the shipped 0.4.2
  binary's dependency algorithm matches the HEAD source documented here.
- The `da workflow eligible` "dep-gating unreliable" symptom is **NOT a missing algorithm**;
  it is a *state-fidelity* problem (§6.4). A swarm must treat `eligible`/`next` dep-gating as
  advisory and gate its own DAG. Details + citations in §6.4.
- **Confirmed 0.4.2-vs-HEAD flag divergence (swarm runs the 0.4.2 binary):** `da workflow fanout`
  in the shipped 0.4.2 binary does **NOT** expose `--with-tests` or `--skip-asserting-test-gate`
  (both absent from `da workflow fanout --help` and from `git show v0.4.2:commands/workflow/cmd.go`;
  present only in HEAD `cmd.go`). The 0.4.2 binary has `--skip-tdd-gate` but **not** the §0d
  asserting-test-scope gate. Everything else documented here (the plan/task/delegation/verify/
  sentinel/hook-outcome surface) matches 0.4.2. Treat §7.1's `--with-tests` /
  `--skip-asserting-test-gate` / asserting-test-scope-gate items as HEAD-only until the installed
  binary is upgraded.

---

## 1. Command tree (every subcommand, every flag)

`da workflow` is defined in `commands/workflow/cmd.go` (`newWorkflowCmd`, registration at
`cmd.go:44-77`). Long help: *"Captures the repository-local workflow state … canonical plans,
checkpoints, verification logs, preferences, fanout artifacts, and bridge queries."*

### 1.1 Global flags (apply to every subcommand)

| Flag | Short | Meaning |
|------|-------|---------|
| `--dry-run` | `-n` | Show what would be done without making changes |
| `--force` | `-f` | Overwrite existing configurations (per-command semantics vary) |
| `--json` | | Output as JSON (read via `deps.Flags.JSON()`) |
| `--verbose` | `-v` | Detailed output |
| `--yes` | `-y` | Auto-confirm prompts |

Note: several subcommands (`app-types`, `plan archive`, `contract create`, `hook-sentinel read`)
**omit** some global flags from their own flag set — e.g. `plan archive` drops `-f` in favor of a
local `--force` (`plan_task.go` archive cmd), and `hook-sentinel read` drops `--json` global (has its
own `--json`). Always confirm per subcommand.

### 1.2 Full tree

```
da workflow
├── status                      Show workflow state (JSON via --json)
├── orient                      Render session orient context
├── checkpoint                  Write a checkpoint
│     --message string
│     --verification-status string   (pass|fail|partial|unknown; default "unknown")
│     --verification-summary string
│     --log-to-iter int              write schema-validated iter-N stub to .agents/active/iteration-log/iter-N.yaml
│     --role string                  with --log-to-iter: merge only impl|verifier|review block
│     --verifier-type string         verifier slug when --role verifier (e.g. unit)
├── log                         Show recent checkpoint log entries   [--all]
├── plan                        List canonical plans
│   ├── show <plan-id>
│   ├── graph [plan-id]              derived plan/task graph
│   ├── create <plan-id>            --title(req) --summary --owner(default "dot-agents")
│   │                                --success-criteria --verification-strategy
│   ├── update <plan-id>            --status(draft|active|paused|completed|archived) --focus
│   │                                --title --summary --success-criteria --verification-strategy
│   ├── archive                     --plan(req, comma-sep) --force  [--no-commit] [-n]
│   ├── schedule <plan-id>          Kahn BFS topological wave schedule
│   ├── derive-scope <plan> <task>  --seed-symbol(rep) --seed-path(rep)   → scope-evidence sidecar
│   └── check-scope <plan> <task>   --from-git-diff | --changed-file(rep)
├── task
│   ├── add <plan-id>               --id(req) --title(req) --notes --owner(default "dot-agents")
│   │                                --write-scope --depends-on --blocks --app-type
│   │                                --verification-required (default TRUE)
│   └── update <plan-id>            --task(req) --notes --write-scope --title
│                                    (NOTE: cmd also wires --depends-on / --blocks; see §5.4)
├── tasks <plan-id>             Show tasks for a plan
├── slices <plan-id>            Show slices for a plan
├── eligible                    --plan(comma-sep) --limit int   Unblocked eligible tasks + conflicts
├── slots                       --plan(comma-sep)               Slot ledger (occupied/awaiting-owner/blocked)
├── next                        --plan(comma-sep)               Suggest next actionable task
├── complete                    --plan(req, comma-sep)          Probe scoped plan-completion
├── advance <plan-id>           --task(req) --status(req) [--commit-state]
├── health                      Workflow health snapshot
├── verify
│   ├── record                  --kind(req: test|lint|build|format|custom|review) --summary(req)
│   │                            --status(pass|fail|partial|unknown; req unless kind=review)
│   │                            --command --scope(file|package|repo|custom; default repo)
│   │                            --task --verifier-type
│   │                            (review only:) --phase1-decision --phase2-decision
│   │                            --overall-decision --escalation-reason --failed-gate(rep) --reviewer-notes
│   └── log                      [--all]
├── prefs                       Show resolved preferences
│   ├── show
│   ├── set-local <key> <value>
│   └── set-shared <key> <value>            (queued for review)
├── app-types                   [--verbose] [--format flag|task|plan|doc]
├── resolve-prompt              --kind(req: executor|verifier|reviewer|orchestrator) --slug(req)
├── graph
│   ├── query [q]               --intent(plan_context|decision_lookup|entity_context|workflow_memory|contradictions) --scope
│   └── health
├── fanout                      Delegate a task to a sub-agent (see §7.1 for all flags)
├── contract
│   ├── create                  --plan(req) --task(req) [--direct|--delegated|--mode] --owner --write-scope --force
│   └── list
├── merge-back                  --task(req) --summary(req) --verification-status(pass|fail|partial|unknown)
│                                --integration-notes [--commit-state]
├── fold-back
│   ├── create                  --plan(req) --observation(req) --task --slug --propose [--dry-run]
│   ├── update                  --plan(req) --slug(req) --observation(req) --task
│   └── list                    [--plan]
├── delegation
│   ├── closeout                --plan(req) --task(req) --decision(req: accept|reject) --note
│   └── gate                    --task(req) [--plan]
├── drift                       [--project] [--stale-days 7] [--proposal-days 30]     (read-only)
├── sweep                       [--apply] [--stale-days 7] [--proposal-days 30]
├── bundle
│   └── stages <bundle-path>    Expand bundle → impl → verifier(s) → review
├── commit                      [--dry-run] [--include(rep)]     Stage+commit workflow-state paths
├── close-task <plan-id>        --task(req) [--next-focus] [--no-commit] [--score-recompute current]
│                                [--repo-dir] [--transcript-dir(rep)]
├── start-task <plan-id>        --task(req) [--no-derive-scope] [--no-commit] [--dry-run]
│                                [--seed-symbol(rep)] [--seed-path(rep)]
├── hook-sentinel
│   ├── write <skill>           see §11 (many companion flags)
│   ├── read <skill>            --run-id | --latest [--json]
│   └── clear <skill>           --run-id(req)
├── hook-outcome
│   └── write                   see §12 (many flags)
└── archive-orphans             [-n] [--json]   Reconcile stale active merge-back/delegation artifacts
```

---

## 2. On-disk state layout

Base dirs (`state.go:975-981`, `iter_log_autoderive.go:87-89`; `stateAgentsDir == ".agents"`):

| Path | Content |
|------|---------|
| `.agents/workflow/plans/<plan-id>/PLAN.yaml` | `CanonicalPlan` (`plansBaseDir` = `.agents/workflow/plans`) |
| `.agents/workflow/plans/<plan-id>/TASKS.yaml` | `CanonicalTaskFile` |
| `.agents/workflow/plans/<plan-id>/SLICES.yaml` | `CanonicalSliceFile` (optional) |
| `.agents/workflow/plans/<plan-id>/<plan-id>.plan.md` | human plan narrative (moved on archive) |
| `.agents/history/<plan-id>/…` | archived plan tree (`historyBaseDir` = `.agents/history`) |
| `.agents/history/<plan-id>/delegate-merge-back-archive/<YYYY-MM-DD>/<task>/` | archived merge-back/delegation/closeout/verification (`delegation.go:2182`) |
| `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/` | archived hook sentinels (`hook_sentinel.go:343-345`) |
| `.agents/active/delegation/<task>.yaml` | active `DelegationContract` (`delegation.go:119-121`) |
| `.agents/active/delegation-bundles/<contract-id>.yaml` | fanout bundle (`delegation.go:1519-1521`) |
| `.agents/active/merge-back/<task>.md` | merge-back artifact (`delegation.go:123-125`, `263-275`) |
| `.agents/active/verification/<task>/<verifier_type>.result.yaml` | typed verification result |
| `.agents/active/verification/<task>/review-decision.yaml` | review decision doc |
| `.agents/active/fold-back/<id>.yaml` | fold-back artifact (`delegation.go:299-301`) |
| `.agents/active/hook-sentinels/<skill>-<run-id>.json` | active hook sentinel (`hook_sentinel.go:321-323`) |
| `.agents/active/iteration-log/iter-N.yaml` | canonical per-iteration record |
| `.agents/active/iteration-log/iter-N.hook-outcomes.yaml` | hook-outcome sidecar (`hook_outcome.go:213-215`) |
| `<project-context-dir>/verification-log.jsonl` | append-only verify log (`verification.go:38-40`) |

---

## 3. Plan / Task data model

### 3.1 `CanonicalPlan` — PLAN.yaml (`types.go:96-110`)

| YAML key | Go field | Notes |
|----------|----------|-------|
| `schema_version` | `SchemaVersion int` | stamped `1` at create (`plan_task.go` create) |
| `id` | `ID string` | |
| `title` | `Title` | required at create |
| `status` | `Status` | `draft`\|`active`\|`paused`\|`completed`\|`archived` (`isValidPlanStatus`) |
| `summary` | `Summary` | |
| `created_at`/`updated_at` | RFC3339, UTC | `updated_at` refreshed on every mutation |
| `owner` | `Owner` | default `"dot-agents"` (flag default) |
| `success_criteria` | `SuccessCriteria` | |
| `verification_strategy` | `VerificationStrategy` | |
| `current_focus_task` | `CurrentFocusTask` | **stores task TITLE, not id**, on advance→in_progress; see §5.5 |
| `default_app_type` | `DefaultAppType` (omitempty) | plan-level app_type default |

### 3.2 `CanonicalTaskFile` / `CanonicalTask` — TASKS.yaml (`types.go:112-131`)

`CanonicalTaskFile`: `schema_version int`, `plan_id string`, `tasks []CanonicalTask`.

`CanonicalTask` fields:

| YAML key | Go field | Semantics |
|----------|----------|-----------|
| `id` | `ID` | |
| `title` | `Title` | |
| `status` | `Status` | see §4 state machine |
| `depends_on` | `DependsOn []string` | **the only field that gates eligibility** (§6). Bare id → same-plan; `<plan>/<task>` → cross-plan |
| `blocks` | `Blocks []string` | **advisory only** — used solely in `plan graph` rendering (`plan_task.go:1021`), NEVER consulted for gating |
| `owner` | `Owner` | default `"dot-agents"` |
| `write_scope` | `WriteScope []string` | comma-sep patterns (files/dirs/globs) the task may touch; drives conflict detection + fanout scope |
| `verification_required` | `VerificationRequired bool` | flag default **true** (`cmd.go:426`); gates the fanout TDD pre-check |
| `notes` | `Notes` | free text; fold-back task-notes are appended here |
| `app_type` | `AppType` (omitempty) | verifier-dispatch key (`go-cli`, `go-http-service`, …); resolves the verifier sequence for fanout |

### 3.3 `CanonicalSlice` — SLICES.yaml (`types.go:133-151`)

Bounded sub-slices under a task: `id`, `parent_task_id`, `title`, `summary`, `status`,
`depends_on []string`, `write_scope []string`, `verification_focus`, `owner`.
`fanout --slice <id>` auto-fills task + write-scope from a slice (`delegation.go:1075-1104`).

### 3.4 `DelegationContract` — `.agents/active/delegation/<task>.yaml` (`delegation.go:85-102`)

| Field | Notes |
|-------|-------|
| `schema_version` | `1` (`contract_core.go:48`) |
| `id` | **`del-<taskID>-<unixEpoch>`** (`contract_core.go:49`); this id names the bundle file |
| `mode` | `delegated` (fanout) or `direct` (`contract create`); absent → normalized to `delegated` (`delegation.go:75-83`) |
| `parent_plan_id` / `parent_task_id` | |
| `title` / `summary` | |
| `write_scope []string` | |
| `success_criteria` | contract-create fills from task Notes (`contract.go:104`) |
| `verification_expectations` | |
| `may_mutate_workflow_state bool` | |
| `owner` | |
| `status` | `pending`\|`active`\|`completed`\|`failed`\|`cancelled` (`delegation.go:104-106`); created **`active`** (`contract_core.go:58`) |
| `pending_intent` | `CoordinationIntent`: ``\|`status_request`\|`review_request`\|`escalation_notice`\|`ack` (`delegation.go:29-43`) |
| `created_at`/`updated_at` | |

### 3.5 `delegationBundleYAML` — `.agents/active/delegation-bundles/<contract-id>.yaml` (`types.go:234-279`)

Top-level: `schema_version`, `delegation_id`, `plan_id`, `task_id`, `slice_id?`, `owner`.
- `worker`: `profile` (default `loop-worker`, `types.go:13`), `profile_version?`, `project_overlay_files[]`
- `selection`: `selected_by` (`"workflow fanout"`), `selected_at`, `reason?` (`delegation.go:1855-1863`)
- `scope`: `write_scope[]`, `constraints[]?`
- `prompt`: `inline[]?`, `prompt_files[]?`
- `context`: `required_files[]?`, `optional_files[]?`
- `verification`: `feedback_goal`, `scenario_tags[]?`, `regression_artifacts[]?`,
  `higher_layer_validation_queue?`, `focused_commands[]?`, `regression_commands[]?`,
  `app_type?`, `verifier_sequence[]?`, `evidence_policy?`
- `evidence_policy` (`types.go:189-194`): `require_negative_coverage?`, `sandbox_mutations?`,
  `verifier_chain_max?`, `lens_chain_max?`. Legacy key `primary_chain_max` is accepted as an
  alias for `verifier_chain_max` with a one-shot stderr deprecation warning (`types.go:202-232`).
- `closeout`: `worker_must[]`, `parent_must[]`. Fanout defaults (`delegation.go:1903-1904`):
  `worker_must: [workflow_verify_record, workflow_checkpoint, workflow_merge_back]`,
  `parent_must: [workflow_delegation_closeout]`.

### 3.6 `MergeBackSummary` — `.agents/active/merge-back/<task>.md` (`delegation.go:245-261`)

Stored as Markdown with a YAML frontmatter block + `## Summary` / `## Integration Notes`
sections (`saveMergeBack`, `delegation.go:263-275`). **Every frontmatter field must be present**
(schema_version=1); `files_changed` must be `[]` or a block list or closeout silently rejects
(lesson `merge-back-frontmatter-required`).
Fields: `schema_version`, `task_id`, `parent_plan_id`, `title`, `summary`, `files_changed[]`,
`verification_result {status, summary}` (`MergeBackVerification`), `integration_notes`,
`blockers_encountered[]?`, `created_at`.

### 3.7 `VerificationRecord` — verification-log.jsonl (`types.go:300-311`)

One JSON line per verify run: `schema_version`, `timestamp`, `kind`, `status`, `command`,
`scope`, `summary`, `artifacts[]`, `recorded_by`.

### 3.8 `VerificationResultDoc` — `<task>/<verifier_type>.result.yaml`

Canonical schema `static/verification-result.schema.json`. Required: `schema_version`(=1),
`task_id`, `parent_plan_id`, `verifier_type` (`^[a-z][a-z0-9_-]*$`), `status`
(`pass|fail|partial|unknown`), `summary`, `recorded_at`. Optional: `delegation_id`, `recorded_by`,
`commands[]`, `artifact_paths[]`. The verifier_type is the filename stem, e.g.
`merge-back.result.yaml` (const `VerifierTypeMergeBack = "merge-back"`, `verification_result_schema.go:17`).

### 3.9 `ReviewDecisionDoc` — `<task>/review-decision.yaml`

Canonical schema `static/verification-decision.schema.json`. Required: `schema_version`(=1),
`task_id`, `parent_plan_id`, `phase_1_decision`, `phase_2_decision`, `overall_decision`
(each `accept|reject|escalate`), `failed_gates[]`, `recorded_at`. Optional: `delegation_id`,
`escalation_reason` (**required when overall=escalate**), `reviewer_notes`, `recorded_by`.
Consolidation rule (documented in schema + `review_decision_schema.go:62-70`): **any reject →
reject; else any escalate → escalate; else accept** (pessimistic).

### 3.10 `foldBackArtifact` — `.agents/active/fold-back/<id>.yaml` (`types.go:153-163`)

`schema_version`, `id`, `plan_id`, `task_id`, `observation`, `classification`,
`routed_to`, `created_at`. `routed_to` forms: `task_note:<plan>/<task>`,
`plan_summary:<plan>`, `proposal:<file>` (`delegation.go:108-115`).

### 3.11 `workflowDelegationCloseoutRecord` — closeout.yaml (`types.go:165-174`)

`schema_version`, `plan_id`, `task_id`, `delegation_id`, `decision`, `note?`, `closed_at`.

---

## 4. Task status state machine

Vocabulary (`task_status.go:15-41`; design.md §2.1/§2.2/§3.1/§3.2):

| Status | Meaning | Slot (§2.8) |
|--------|---------|-------------|
| `pending` | created; deps unmet or not picked up | free |
| `in_progress` | worker actively implementing | **holds** |
| `awaiting_agent_review` | branch pushed, verifier green, lens reviewers running | **holds** (may bounce back) |
| `awaiting_owner_review` | lens accepted; human owns merge | free (unbounded latency) |
| `blocked` | external/cascade/unrecoverable-verify block; manual recovery | free |
| `completed` | terminal success (PR merged) | free |
| `cancelled` | terminal manual abandonment | free |

Plus parameterized `blocked-on:<ref>` prefix status (`eligible_accounting.go:29-37`) — frees slot,
bucketed separately from bare `blocked`.

Legal transitions (`validTaskStatusTransitions`, `task_status.go:94-134`; self-transitions are
idempotent no-ops):

```
pending             → in_progress | cancelled | blocked | completed(direct)
in_progress         → awaiting_agent_review | blocked | cancelled | completed(direct)
awaiting_agent_review → awaiting_owner_review | in_progress(reject) | blocked
awaiting_owner_review → completed(merged) | in_progress(changes) | blocked(closed→cascade)
blocked             → in_progress | cancelled
completed           → (terminal, no edges)
cancelled           → (terminal, no edges)
```

Transition ownership (`transitions.go:34-42`, design §6) — each edge has a *sole* dispatcher:
- `ownerVerifier`: `in_progress → awaiting_agent_review` (verifier gate).
- `ownerLensGate`: `awaiting_agent_review → {awaiting_owner_review | in_progress}`.
- `ownerPollDetector`: all `awaiting_owner_review`-side edges + force-rebase, driven by
  `event.pr.*` envelopes — **not** a `gh` shell-out (`transitions.go:203-248`).

`advance --status` validates against this machine via `applyTaskStatusTransition` →
`validateTaskStatusTransition` (`task_status.go:189-220`). An **unknown from-status is treated
permissively** (returns true) so legacy rows are not wedged (`task_status.go:143-152`). A rejected
transition leaves the task untouched. Note: `advance` accepts direct `pending/in_progress →
completed` shortcuts (legacy non-staged closeout) — the staged path routes through
`awaiting_owner_review`.

---

## 5. Plan/task mutation commands (`plan_task.go`)

### 5.1 `plan create <id>` (`runWorkflowPlanCreate`)
- Refuses an existing plan dir (`plan %q already exists`).
- Writes `PLAN.yaml` (`schema_version:1`, `status:"draft"`, all flag values, timestamps) and an
  empty `TASKS.yaml` (`schema_version:1`, `plan_id`, `tasks:[]`).
- `observed.FilesCreated = [PLAN.yaml, TASKS.yaml]`. **Not lock-guarded** (§8).

### 5.2 `plan update <id>` (`runWorkflowPlanUpdate`)
- Validates `--status` against `draft|active|paused|completed|archived`.
- Applies only non-empty, changed values (`applyPlanFieldUpdate`) to `Status/Title/Summary/
  SuccessCriteria/VerificationStrategy/CurrentFocusTask` (`--focus` → `current_focus_task`).
- Always refreshes `updated_at`. **Not lock-guarded.**

### 5.3 `plan archive` (`runWorkflowPlanArchive` / `archiveSinglePlan`)
- Loops `--plan` ids (comma-sep supplied by cmd layer); each failure recorded, loop continues,
  first error returned.
- **Guard**: plan must be `status==completed` unless `--force` (`plan %q has status %q (expected
  completed)`).
- Stamps `PLAN.yaml status=archived` + `updated_at`, then `mergeWorkflowPlanDir(planID, srcDir,
  dstDir)` merges `.agents/workflow/plans/<id>` → `.agents/history/<id>` and removes the source dir.
- Merge rules (`fs.go:176-211`): DMA artifacts (delegation.yaml, merge-back.md, closeout.yaml,
  `delegate-merge-back-archive/`) → always skip; `PLAN.yaml`/`TASKS.yaml`/`<id>.plan.md` → always
  overwrite; everything else → sha256+mtime compare (dest-newer → skip+warn).
- `--dry-run` (global `-n`): prints `[dry-run] stamp … status=archived`, previews the merge,
  prints `[dry-run] remove source dir …`, does NOT mutate or commit.
- `--no-commit`: skips the trailing `iterationCloseCommit`; otherwise commits the archive move.

### 5.4 `task add <plan>` / `task update <plan>`
- `add` (`runWorkflowTaskAdd`): appends a `CanonicalTask{status:"pending"}` with `--id/--title/
  --notes/--owner/--app-type/--verification-required` and CSV-split `--depends-on/--blocks/
  --write-scope`. Duplicate-id guard `task %q already exists`. **Lock-guarded** (`withTasksLock`),
  then unlocked `PLAN.yaml.updated_at` bump.
- `update` (`runWorkflowTaskUpdate` + `applyTaskFieldUpdates`): replaces `title/notes/write_scope/
  depends_on/blocks` (only non-empty, changed). Task-not-found guard. **Lock-guarded.** (Help text
  only advertises `--notes/--write-scope/--title`, but cmd also wires `--depends-on`/`--blocks`,
  `cmd.go:451`.)

### 5.5 `advance <plan> --task --status` (`runWorkflowAdvance`, `plan_task.go:2041-2089`)
1. Validate `--status` ∈ vocabulary (`isValidTaskStatus`).
2. `withTasksLock`: load TASKS.yaml → capture `observed.FromStatus` → `applyTaskStatusTransition`
   (edge validation) → `saveCanonicalTasks`.
3. Reload PLAN.yaml, refresh `updated_at`; set `current_focus_task` = the advanced task's **title**
   if new status is `in_progress`, else `effectivePlanFocusTask(tf.Tasks)` (§5.6); save PLAN.yaml.
4. **`runWorkflowAdvance` itself does NOT touch `--commit-state` and does NOT flip
   `plan.Status→completed`.** `--commit-state` is wired at the cmd layer: after a successful
   advance it calls `iterationCloseCommit(...)` (`cmd.go:586-598`).

### 5.6 `effectivePlanFocusTask` (`plan_task.go:2019-2038`)
Returns the last `in_progress` task's title; else the first `pending` task whose deps are all
complete (`incompleteCanonicalDependencies` empty); else `""`.

---

## 6. Eligibility, dependency gating, slots — the swarm's ordering surface

### 6.1 `eligible` (`runWorkflowEligible`, `plan_task.go:1456`) and `next` (`runWorkflowNext`, :1290)
Both call `selectAllEligibleTasks(projectPath, planFilter)` (`plan_task.go:1899`). `eligible`
returns the full annotated set + `max_batch` (greedy non-conflicting batch); `next` returns one
suggestion. `--limit`/`max_parallel_workers` pref caps output (`resolveEligibleEffectiveLimit`,
:1393). `--plan` accepts comma-sep ids; unknown id → error (`applyEligiblePlanFilter`, :1937).

### 6.2 `selectAllEligibleTasks` pipeline
- Enumerate canonical plan ids → filter → `loadActiveDelegationTaskSet` (a task with a
  `pending`/`active` delegation contract whose `ParentTaskID` matches is excluded) →
  `collectEligibleTasksForPlan` per **active** plan (`plan_task.go:1977-2014`).
- `collectEligibleTasksForPlan` includes a task iff: `plan.Status=="active"`, `task.Status ∈
  {pending, in_progress}`, task id ∉ active-delegations, and
  `incompleteCanonicalDependenciesCrossplan(...)` returns empty.

### 6.3 Dependency-satisfaction predicate (`eligible_accounting.go:72-74`)

```go
func depSatisfiesDownstream(s string) bool {
    return s == TaskStatusCompleted || s == TaskStatusAwaitingOwnerReview
}
```

So an upstream in **`completed` OR `awaiting_owner_review`** satisfies a downstream dep. `in_progress`
and `awaiting_agent_review` do **not** satisfy (impl/lens may still bounce). `pending`,
`blocked`, `blocked-on:*`, `cancelled` do not satisfy (design §3.4.6/§4).

Cross-plan resolution (`incompleteCanonicalDependenciesCrossplan` / `crossPlanDepIncomplete`,
`plan_task.go:1786-1834`):
- Bare dep id (no `/`) → looked up in the **local** plan's `statusByID` map.
- `<plan>/<task>` dep → `loadCanonicalTasks(refPlan)`, falling back to
  `.agents/history/<refPlan>/TASKS.yaml` (so a dep on an *archived* plan's completed task resolves).
- Unresolvable cross-plan plan/task → treated as **incomplete** (gates) + a warning appended.

### 6.4 ⚠️ Why dep-gating is "unreliable" in practice (swarm-critical)

The *algorithm* is sound; the *inputs* drift. Grounded failure modes:

1. **Status drift / no PR cross-check.** `eligible` reads `TASKS.yaml.status` verbatim; it does NOT
   cross-check whether the work already shipped via a merged PR (lesson
   `verify-task-status-vs-pr-history`: *"`da workflow eligible` reports tasks whose status is
   pending|in_progress … It does NOT cross-check whether the work already shipped via a merged
   PR"*). After parallel waves, statuses lie → already-shipped tasks resurface as eligible, and
   deps whose upstream shipped but was never reconciled (missed `delegation closeout`) still gate
   downstream (lesson `reconcile-task-status-on-pr-merge`; proposal `pr-merge-auto-reconcile.md`).
2. **Worktree-copy reads.** `loadCanonicalTasks` reads the **worktree copy** of TASKS.yaml, not a
   canonical ref. Under worktree isolation / a stale local checkout, `eligible` reads stale plans →
   re-dispatch storms. This is the entire motivation for the active `git-ref-work-backend` plan and
   its `read-from-master-shim` task, and lesson `stale-local-checkout-mass-drift` (*"a local
   checkout many commits behind origin … `da`/`eligible` then read stale plans"*).
3. **Silent intra-plan missing-dep block.** A bare `depends_on` id that does not exist in the local
   plan yields `statusByID[dep]==""` → `depSatisfiesDownstream("")==false` → **permanently gated,
   no warning** (only *cross-plan* misses warn). A typo'd same-plan dep id silently wedges the
   dependent.
4. **`blocks` never gates.** Only `depends_on` gates (§3.2). An upstream that declares
   `blocks:[x]` without `x` declaring `depends_on:[upstream]` produces NO ordering.

**Swarm rule:** do not treat `eligible`/`next` dep-gating as authoritative. Maintain your own DAG
from `depends_on` (qualify cross-plan deps as `<plan>/<task>`), reconcile task status (run
`delegation closeout` / `advance`) so state does not drift, and re-read canonical state fresh each
wave rather than trusting a cached worktree snapshot.

### 6.5 Slots / concurrency (`slots`, `eligible_accounting.go`)
- `countsAgainstParallelTasks(s)` → true only for `in_progress` and `awaiting_agent_review`
  (`:45-47`). `freesSlot(s)` → `awaiting_owner_review` or `blocked-on:*` (`:55-57`).
- `slots` renders occupied / awaiting-owner / blocked buckets vs `max_parallel_tasks`
  (`computeSlotLedger`, `:154-167`). Default budget = `NumCPU - reserve`, clamped (`:277-292`),
  supersedes the old fixed 7; overridable via prefs (`resolveMaxParallelTasks`, `:250-258`).
- Eligible-batch parallelism uses a **separate** `max_parallel_workers` pref (`:1393`), distinct
  from the slot budget.

### 6.6 Write-scope conflict detection (`plan_task.go`)
`computeWriteScopeConflicts` populates each suggestion's `conflicts_with`, builds a conflict graph,
and computes `max_batch` (greedy set of non-conflicting eligible tasks). `annotateEligibleTasks`
adds `has_evidence`/`evidence_confidence` from the scope-evidence sidecar
(`deriveScopeEvidencePath`), and `write_scope_declared` (false when empty). `eligible --json`
output shape: `{eligible_tasks[], max_batch[], conflict_graph, total_eligible, max_parallel,
draft_plans[]}`.

---

## 7. Delegation lifecycle (fanout → worker → merge-back → gate → closeout)

Status flow the swarm drives:

```
        fanout (or contract create)          worker does work        merge-back
task ──────────────────────────────► [contract active] ──────────► [merge-back.md +
(pending→in_progress)                                                <vt>.result.yaml,
                                                                     contract=completed]
                                                                            │
                     (optional) verify record --kind review                │
                     writes review-decision.yaml                           ▼
                                                            delegation gate (advisory readback)
                                                                            │
                                              accept ──────────► delegation closeout --decision accept
                                                                     → task = completed
                                              reject ──────────► delegation closeout --decision reject
                                                                     → task = blocked (+ note in Notes)
```

### 7.1 `fanout` (`runWorkflowFanout`, `delegation.go:1279-1383`)
All flags: `--plan(req)`, `--task(req)` | `--slice`, `--owner`, `--write-scope`,
`--delegate-profile`(default `loop-worker`), `--feedback-goal`, `--prompt(rep)`,
`--prompt-file(rep)`, `--context-file(rep)`, `--project-overlay(rep)`, `--scenario-tag(rep)`,
`--regression-artifact(rep)`, `--validation-queue`, `--selection-reason`, `--base-branch`,
`--verifier-sequence`, `--verifier-retry-max`, `--lens-retry-max`, `--require-negative-coverage`,
`--sandbox-mutations`, `--skip-evidence-check`, `--skip-tdd-gate`.
**HEAD-only (NOT in the 0.4.2 binary — see §0):** `--skip-asserting-test-gate` (`cmd.go:911`) and
`--with-tests` (`cmd.go:893`, auto-adds each `.go` write_scope entry's sibling `_test.go` path).

Sequence:
1. Resolve task + write-scope (slice-aware); refuse if the task already has a contract
   (`task %s already has an active delegation contract`).
2. `ensureTaskVerificationDir`; `checkPreVerifierTDDGate` (Go write_scope needs adjacent
   `*_test.go` unless `--skip-tdd-gate` or `verification_required=false`);
   `checkFanoutScopeEvidenceWarnings` (non-blocking); `checkFanoutWriteScopeConflicts` (refuses a
   fanout whose write_scope overlaps a pending/active delegation — **fails closed** on a corrupt
   contract, `delegation.go:1166-1195`); *(HEAD-only, not in 0.4.2)* `checkFanoutAssertingTestScope`
   (§0d EXPAND-warn / cross-package REFUSE, `delegation.go:2339-2424`).
3. `fanoutResolveBase` (§4 layered base-resolution; multi-dep conflict → non-zero exit).
4. `materializeFanoutContractAndBundle` → writes `DelegationContract` (mode=delegated,
   status=active) + the bundle at `.agents/active/delegation-bundles/<contract-id>.yaml`
   (removes orphan contract on bundle-build failure).
5. `advanceFanoutTaskStatusIfPending`: flips task `pending → in_progress` (non-fatal on save error,
   `delegation.go:1385-1396`).

### 7.2 `contract create` (`runWorkflowContractCreate`, `contract.go:37-131`)
Direct-mode counterpart to fanout: same on-disk contract shape and same closeout pipeline, for
orchestrator-owned work. Mode resolution (`resolveContractCreateMode`): default **`direct`**;
`--direct`/`--delegated`/`--mode` (mutually validated). Guards: plan/tasks must load; task status
must be `pending|in_progress` (`resolveContractTargetTask`); existing contract requires `--force`;
non-empty write scope required (falls back to TASKS.yaml write_scope). Does **not** advance task
status or write a bundle (unlike fanout). `contract list` groups by mode (direct first),
`--json` emits the contract array.

### 7.3 `merge-back` (`runWorkflowMergeBack`, `delegation.go:1455-1517`)
`--task(req)`, `--summary(req)`, `--verification-status`(pass|fail|partial|unknown),
`--integration-notes`, `--commit-state`.
- Requires an existing contract not already `completed`/`cancelled`.
- `filesChanged = git diff --name-only HEAD` (`delegation.go:1406-1418`).
- Writes `.agents/active/merge-back/<task>.md` (`buildMergeBackSummary`) **and** a typed
  `.agents/active/verification/<task>/merge-back.result.yaml` (`buildMergeBackVerificationDoc`,
  verifier_type=`merge-back`).
- Flips **contract.Status = "completed"** (non-fatal warn on save error). Does NOT advance the
  canonical task — the parent must review then closeout.
- `--commit-state` (cmd layer, `cmd.go:930-937`): after recording, `iterationCloseCommit`.

### 7.4 `delegation gate` (`runWorkflowDelegationGate`, `review_gate.go:147`)
Deterministic **read-only** parent-gate readback (`evaluateDelegationGate`, `review_gate.go`):
loads contract (+ optional `--plan` cross-check) and requires a merge-back to exist. Then:
- No `review-decision.yaml` → `outcome=reject`, `closeout_allowed=false`, reason *"review-decision.yaml
  missing; explicit accept evidence required before closeout"*.
- `overall_decision==accept` → `outcome=accept`, `closeout_allowed=true`.
- `==reject` → `outcome=reject`.
- `==escalate` → `outcome=escalate`, `planning_required=true`.
JSON shape `DelegationGateDecision`: `{schema_version, task_id, plan_id, delegation_id,
merge_back_present, review_decision_present, review_overall_decision, outcome, closeout_allowed,
planning_required, reason, escalation_reason}`. **Gate does not mutate anything** — it is advisory
evidence for the closeout decision.

### 7.5 `delegation closeout` (`runWorkflowDelegationCloseout`, `delegation.go:2266-2327`)
`--plan(req)`, `--task(req)`, `--decision(req: accept|reject)`, `--note`.
1. Requires an existing merge-back (`merge-back for task %s is required before closeout`).
2. `reconcileDelegationContractForCloseout`: plan-id must match; forces contract → `completed` if
   not already terminal (`delegation.go:2156-2178`).
3. Builds `closeout.yaml` record; `archiveCloseoutArtifacts` (`delegation.go:2180-2220`) copies
   `merge-back.md`, `delegation.yaml`, and (accept-only) the `verification/` dir into
   `.agents/history/<plan>/delegate-merge-back-archive/<YYYY-MM-DD>/<task>/`, writes `closeout.yaml`,
   then removes the active merge-back, active delegation, active verification dir (accept), and the
   bundle file.
4. `applyCloseoutDecisionToTasks` (`delegation.go:2222-2264`):
   - **accept → task.Status = "completed"**.
   - **reject → task.Status = "blocked"** and (if `--note`) appends
     `delegation closeout reject: <note>` to the task Notes.
   - Then reloads PLAN.yaml, refreshes `updated_at`, recomputes `current_focus_task`
     (`effectivePlanFocusTask`), and if **all tasks are terminal** (`completed`/`cancelled`) flips
     **`plan.Status = "completed"`** (`allCanonicalTasksTerminal`, `delegation.go:2141-2154`).
   - ⚠️ This task-status write goes through `saveCanonicalTasks` **without** `withTasksLock` (§8).
5. `--json` emits the closeout record.

### 7.6 `bundle stages <bundle-path>` (`bundle.go:81-110`)
Expands a bundle into ordered stages: `impl`, one `verifier:<type>` per non-empty
`verifier_sequence` entry, then `review` (`expandBundleStages`, `bundle.go:19-32`). Text output one
per line; `--json` emits `[]bundleStageEntry{stage, verifier_type?}`.

---

## 8. File-lock on TASKS.yaml — `AcquireFileLock` (concurrency)

Primitive: `internal/agentslock/lockfile.go:398` — `AcquireFileLock(path) (release func() error, err)`.
An advisory **inter-process** lock implemented as a `<path>.lock` mkdir-lock with stale reclaim and
a bounded acquisition timeout (`lockAcquireTimeout`, `lockfile.go:68`).

In `commands/workflow`, TASKS.yaml is locked only via `withTasksLock` (`plan_task.go:562-576`):
```go
func withTasksLock(projectPath, planID string, fn func() error) (err error) {
    path := tasksLockPath(projectPath, planID) // = plansBaseDir/<plan>/TASKS.yaml
    release, lockErr := agentslock.AcquireFileLock(path)
    if lockErr != nil {
        return fmt.Errorf("TASKS.yaml locked by another process, timed out waiting: %w", lockErr)
    }
    defer release() // wraps release error
    return fn()
}
```
The lock keys on the **protected file's own path** (`tasksLockPath`, `plan_task.go:550`).

**Lock-guarded TASKS.yaml writers:** `runWorkflowAdvance`, `runWorkflowTaskAdd`,
`runWorkflowTaskUpdate` (all in `plan_task.go`).

**⚠️ UNLOCKED writers (lost-update race, swarm-critical):**
- `plan create` writes TASKS.yaml unlocked.
- `delegation.go` writers — `advanceFanoutTaskStatusIfPending` (fanout), `applyCloseoutDecisionToTasks`
  (closeout) — call `saveCanonicalTasks` with **no lock**.
- `contract.go` and `eligible_accounting.go` read/write without the lock.
- The PLAN.yaml `updated_at`/`current_focus_task` writes after add/update/advance are outside the lock.

This is documented in the repo's own handoff/proposal notes: *"only `plan_task.go` takes
`agentslock.AcquireFileLock` (`:573`) … `delegation.go`/`contract.go`/`eligible_accounting.go` have
zero lock usage → concurrent `da workflow advance`/`task update` still silently lose updates there"*
(handoff `2026-07-11-dot-agents-state-from-payout-mega-session.md`;
proposal `read-task-state-from-master-source.md`). The strategic fix (git-ref/WorkStore backend) is
the active `git-ref-work-backend` plan.

**Swarm rule:** never run two mutating `da workflow` commands that touch the **same plan** (or a
fanout/closeout on that plan concurrently with an advance/task-update) in parallel — serialize
mutations per plan. `advance`/`task add`/`task update` are inter-process safe against each other;
`fanout`/`closeout`/`contract`/`plan create` are NOT.

---

## 9. Verification recording (`verify record`) & aggregation

`verify record` (`verification.go`) has two paths keyed on `--kind`:

### 9.1 Non-review kinds (`test|lint|build|format|custom`) — `runWorkflowVerifyRecord` (`verification.go:303-355`)
- Requires `--status` (`pass|fail|partial|unknown`) + `--summary`; `--scope` default `repo`.
- If `--task` set: writes a typed `.agents/active/verification/<task>/<verifier_type>.result.yaml`
  (`writeVerifyResultArtifact`, `:251-289`). `verifier_type` defaults to `--kind` when `--task` set;
  must match `^[a-z][a-z0-9_-]*$`. Requires an existing contract (reads `parent_plan_id`,
  `delegation_id`).
- Always appends a `VerificationRecord` line to `verification-log.jsonl`.

### 9.2 Review kind — `runWorkflowVerifyRecordReview` (`verification.go:140-210`)
- `--phase1-decision`/`--phase2-decision` (`accept|reject|escalate`), consolidated to
  `overall_decision` by the pessimistic rule (`resolveReviewOverallDecision`, `:85-102`;
  `deriveOverallReviewDecision`, `review_decision_schema.go:62-70`). If `--overall-decision` is set
  it must equal the derived value or the command errors. `escalate` requires `--escalation-reason`.
- `--task` resolves the delegation contract (or the single readable active contract when omitted).
- Writes `.agents/active/verification/<task>/review-decision.yaml` (`ReviewDecisionDoc`) and appends
  a `kind:"review"` verification-log record whose status maps
  `accept→pass / reject→fail / escalate→partial` (`overallDecisionToVerificationStatus`,
  `review_decision_schema.go:72`).

### 9.3 How merge-back "aggregates" verdicts
`merge-back` itself writes a single `merge-back.result.yaml` carrying the worker-reported
`--verification-status` (§7.3). Per-verifier evidence is the set of `<verifier_type>.result.yaml`
docs the worker wrote via `verify record --task ...`; the review lens is `review-decision.yaml`.
The **`delegation gate`** consolidates: it reads the merge-back presence + `review-decision.yaml`'s
`overall_decision` into an accept/reject/escalate readback (§7.4). There is no numeric roll-up —
consolidation is the pessimistic decision fold plus "merge-back + accept present" gating. `--lens`
is not a flag; lens verdicts flow through phase-1/phase-2 review decisions and `--failed-gate`.
(Per-lens retry budgets are set on the bundle via `fanout --lens-retry-max`.)

---

## 10. Composed client commands (start-task / close-task)

### 10.1 `start-task <plan> --task` (`start_task.go`)
Chain: `plan update --status active` → `plan update --focus <task>` → (unless `--no-derive-scope`)
`plan derive-scope <plan> <task> [--seed-symbol/--seed-path]` → (unless `--no-commit`)
`workflow commit`. `--dry-run`/`-n` short-circuits with **no** disk/journal writes and previews the
chain. JSON result: `{plan_id, task_id, activated_plan, focused_task, derived_scope,
workflow_commit, dry_run}`. **Fanout is intentionally NOT wired** — decide direct-vs-delegated
explicitly.

### 10.2 `close-task <plan> --task` (`close_task.go`)
Chain: `checkpoint --log-to-iter N` → `score iteration N` → **`advance --status completed`**
(direct completion, `close_task.go`) → `plan update --focus <next>` (next = `pickNextFocus` = first
eligible task in the same plan, or `--next-focus` override) → (unless `--no-commit`) `workflow
commit`. `--score-recompute` only accepts `current` in this build (others error). JSON result:
`{plan_id, task_id, iteration_n, sidecar_path, score_value, score_band, next_focus, workflow_commit}`.
⚠️ close-task advances the task directly to `completed` (not through the staged review gates) and does
not run the delegation merge-back/closeout pipeline — it is the direct-work end-of-iteration molecule.

---

## 11. Hook sentinels (`hook-sentinel`)

Sentinels are the contract between an enforced skill and its Stop/SubagentStop gate. Active file:
`.agents/active/hook-sentinels/<skill>-<run-id>.json` (`HookSentinelDoc`, `hook_sentinel.go:174-187`).
Schema `static/workflow-hook-sentinel.schema.json` (v1). Enforced skills:
`iteration-close`, `isp`, `loop-worker`, `orchestrator-session-start`, `delegation-lifecycle`.

### 11.1 `write <skill>` (`runHookSentinelWrite`)
Required: `--run-id` (filename-safe `^[A-Za-z0-9][A-Za-z0-9._-]*$`), `--plan`, `--task`,
`--agent-type` (`main|loop-worker`). Optional generic: `--write-scope(rep)`, `--expect(rep)`
(`expected_artifacts`), `--eligible-snapshot-loaded`, `--max-batch`.
Companion (`--operation`): `fanout_handoff` / `existing_bundle_handoff` require
`--delegation-path`, `--bundle-path`, `--write-scope`; `parent_closeout` requires `--decision`
(`accept|reject`), `--expected-archive-artifact(rep)`, `--expected-cleanup-path(rep)`.
`orchestrator-session-start` restricts operation to `{fanout_handoff, existing_bundle_handoff}`;
`delegation-lifecycle` to `{parent_closeout}` (schema `allOf`).
- The CLI **captures git HEAD itself** (`git_head_at_start`); any caller-supplied value is discarded
  (`buildHookSentinelDoc`, `hook_sentinel.go:549-579`; schema note).
- `started_at` = now (UTC RFC3339Nano). `lifecycle_point` v1 = only `skill_entry`.
- Atomic temp+rename write; **collision = error** (v1 has no overwrite; must `clear` first,
  `hook_sentinel.go:347-390`).

### 11.2 `read <skill>` (`runHookSentinelRead`)
`--run-id <id>` (exact) XOR `--latest` (most recent `started_at`, filename tie-break,
`readLatestHookSentinel`/`pickLatestHookSentinel`). `--json` emits the doc. Errors if no sentinel
for the skill.

### 11.3 `clear <skill> --run-id` (`clearHookSentinel`, `hook_sentinel.go:486-521`)
Archives the active record to
**`.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/<skill>-<run-id>.json`** where the date is
derived from the sentinel's own `started_at` (UTC), then removes it from active. **No record is
silently deleted** — `clear` is archive-then-remove; archive collision is an error (v1 does not
overwrite history).

---

## 12. Hook outcomes (`hook-outcome write`)

Appends one `HookOutcomeRecord` (`hook_outcome.go:123-139`) to
`.agents/active/iteration-log/iter-N.hook-outcomes.yaml` (`HookOutcomeSidecar`, `:141-145`), where N
= the highest existing `iter-N.yaml` (`resolveActiveIterationN`, `:225-254`). Schema
`static/workflow-hook-outcome.schema.json` (v1).

Required flags: `--sentinel-id` (`<skill>-<run-id>`), `--skill`, `--lifecycle-point`
(`pre_tool_use|stop|subagent_stop|subagent_start|pre_compact|post_tool_use|post_tool_use_failure`),
`--intervention-class` (`prevent_before_action|remediate_at_stop|continuity_advice|
observe_tool_result`), `--result` (`allow|advise|remediate`), `--rule-id`
(`^[a-z][a-z0-9-]*(?:\.[A-Za-z0-9]+)+$`), `--platform` (`claude|codex|copilot|cursor`). Optional:
`--ts` (default now), `--correlation-id` (default = sentinel-id), `--archived-sentinel-path`.

Behavior (`appendHookOutcome`, `:404-441`):
- **Idempotent** on the R2.3 key `(sentinel_id, rule_id, lifecycle_point, intervention_class)` — a
  duplicate is a no-op that preserves the existing record (ts not refreshed).
- **No active iteration → exit 0 silently** (status `no_active_iteration`, R2.2) with a stderr advisory.
- Bounded telemetry: transcript/tool-input/tool-output/stdout/stderr/command_args/failure_message are
  **schema-forbidden** (`propertyNames.not`, schema §hookOutcome).
- 8s write timeout (`hookOutcomeWriteTimeout`). Atomic temp+rename write.

---

## 13. Other subcommands (brief, swarm-relevant)

- **`status` / `orient`** (`state.go`, `types.go:62-80`): read-only session context — active/canonical
  plans, checkpoint, health, prefs, active delegations, pending merge-backs, warnings, recent
  sessions. `--json` for machine consumption. `orient` is the session-start snapshot the ISP gate's
  `eligible_snapshot_loaded` signal refers to.
- **`checkpoint`**: writes a `workflowCheckpoint`; `--log-to-iter N` writes a schema-validated
  iter-N stub (used by close-task); `--role impl|verifier|review` merges one block.
- **`complete --plan`**: probes scoped plan-completion state (are all tasks terminal?).
- **`health`**: `WorkflowHealthSnapshot` (`types.go:313-336`) — git dirty count, has-active-plan,
  pending proposals, canonical plan count, completed-plans-pending-archive, tooling status.
- **`prefs` / `set-local` / `set-shared`**: resolved `WorkflowPreferences`; `set-local` writes a
  user override, `set-shared` queues a proposal for review.
- **`app-types` / `resolve-prompt`** (`app_types.go`, `profile_prompt.go`): read the units-lock-backed
  effective config (same path as `da config explain`) to list `app_type → verifier_sequence`
  (`execution_profile` topology) and resolve a stage profile's composed prompt files. These feed
  fanout verifier dispatch (`resolveFanoutVerifierDispatch`, `delegation.go:1685-1705`).
- **`graph query/health`**: forwards bridge intents (`plan_context`, `contradictions`, …) to the KG
  bridge (`da kg bridge query`).
- **`fold-back create/update/list`** (`delegation.go:431-976`): records a loop observation and routes
  it. `--slug` = stable id for create-or-update (one tagged line per slug). Routing:
  - default → appends a bullet to the task's Notes (`task_note:<plan>/<task>`) when `--task` set,
    else the plan summary (`plan_summary:<plan>`);
  - `--propose` → writes a `~/.agents/proposals/*.md` file (`proposal:<file>`) consumed by `da review`
    (see the `da-run-review.md` sibling doc for the proposal-consumption side).
  `--dry-run`/`-n` previews routing + target paths with no writes/journal.
- **`drift` / `sweep`**: cross-repo workflow-drift detector (read-only) / fixer (`--apply`).
  Thresholds `--stale-days 7`, `--proposal-days 30`.
- **`commit`** (`commit_cmd.go`): stages the deterministic scoped set of `.agents/workflow/` +
  `.agents/history/` paths (from `git status --porcelain=v2 -z`) and commits with a generated
  message. `--include <path>` adds session-touched paths (e.g. iter-N.yaml). Never `-A`; idempotent;
  `--dry-run` prints the staging set. This is what `--commit-state`/close-task/start-task call
  (`iterationCloseCommit`).
- **`archive-orphans`**: reconciles stale `.agents/active/{merge-back,delegation}/<task>` artifacts
  after `plan archive` (dedupe/relocate/fold-back). `-n` preview, `--json` machine output.

---

## 14. Swarm-relevant hooks (non-interactive recipes + gotchas)

All commands accept the global `--json` / `-y`(auto-confirm) / `-n`(dry-run). Recommended
non-interactive invocations for an artifact-driven DAG swarm:

### 14.1 Read state (no mutation)
```bash
da --json workflow orient                       # session snapshot: plans, focus, delegations, warnings
da --json workflow eligible --plan P1,P2        # eligible_tasks[] + max_batch[] (ADVISORY — see §6.4)
da --json workflow next --plan P1               # single next suggestion
da --json workflow slots --plan P1              # slot ledger vs max_parallel_tasks
da --json workflow tasks P1                     # full task list w/ status
da --json workflow contract list                # in-flight contracts by mode
da --json workflow delegation gate --task T     # accept/reject/escalate readback (does NOT mutate)
da --json workflow bundle stages .agents/active/delegation-bundles/del-T-<epoch>.yaml
```

### 14.2 Pick a task (swarm should gate its own DAG)
- Use `eligible --json` for the candidate set + `max_batch`/`conflict_graph`, but **re-derive dep
  order from `depends_on` yourself** (qualify cross-plan as `<plan>/<task>`). Treat `eligible`'s
  ready/blocked partition as a hint, not truth (§6.4). Re-read fresh each wave (worktree staleness).
- Satisfaction predicate to mirror: an upstream is "done enough" iff its status is `completed` OR
  `awaiting_owner_review`.

### 14.3 Delegate a slice (bundle → worker)
```bash
da -y workflow fanout --plan P1 --task T \
   --owner worker-T --write-scope internal/foo/,commands/bar.go \
   --feedback-goal "…" --verifier-sequence unit,api            # → contract(active) + bundle; task→in_progress
# OR orchestrator-owned direct work:
da --json workflow contract create --plan P1 --task T --direct --write-scope commands/bar.go
```
Then read `bundle stages` to drive the worker's impl→verifier(s)→review order. Bundle default
closeout obligations: worker_must=[verify_record, checkpoint, merge_back]; parent_must=[closeout].

### 14.4 Record verification / review
```bash
da workflow verify record --kind test --status pass --task T --verifier-type unit \
   --command "go test ./..." --summary "all green"             # → <T>/unit.result.yaml + log line
da workflow verify record --kind review --task T \
   --phase1-decision accept --phase2-decision accept --summary "LGTM"   # → <T>/review-decision.yaml
```
Escalate needs `--escalation-reason`; reject may list `--failed-gate g1 --failed-gate g2`.

### 14.5 Merge-back → gate → closeout
```bash
da workflow merge-back --task T --summary "…" --verification-status pass    # contract→completed
da --json workflow delegation gate --task T                                 # confirm accept
da -y workflow delegation closeout --plan P1 --task T --decision accept      # task→completed (or blocked on reject)
```
- Order is enforced: closeout requires a merge-back to exist first; gate requires merge-back +
  review-decision.yaml.
- `accept` → task `completed` (+ plan flips `completed` when all tasks terminal). `reject` → task
  `blocked` + note appended. Both archive artifacts under
  `.agents/history/<plan>/delegate-merge-back-archive/<date>/<task>/`.

### 14.6 Advance / focus / commit state
```bash
da workflow advance P1 --task T --status in_progress                # focus→task title
da workflow advance P1 --task T --status completed --commit-state   # advance + workflow commit
da workflow start-task P1 --task T                                  # activate+focus+derive-scope+commit
da workflow close-task P1 --task T --json                           # checkpoint+score+advance completed+focus+commit
da workflow commit --json                                          # commit workflow-state only
```

### 14.7 Sentinel / hook-outcome lifecycle (skill-gate contract)
```bash
da workflow hook-sentinel write loop-worker --run-id r1 --plan P1 --task T \
   --agent-type loop-worker --write-scope internal/foo/ [--expect path]
da --json workflow hook-sentinel read loop-worker --latest
da workflow hook-outcome write --sentinel-id loop-worker-r1 --skill loop-worker \
   --lifecycle-point subagent_stop --intervention-class remediate_at_stop \
   --result remediate --rule-id loop-worker.R3.1 --platform claude
da workflow hook-sentinel clear loop-worker --run-id r1    # archive → .agents/history/<plan>/hook-sentinels/<date>/
```

### 14.8 Gotchas / invariants a swarm MUST respect
1. **Per-plan write serialization.** `fanout`, `delegation closeout`, `contract create`, and
   `plan create` write TASKS.yaml/PLAN.yaml **without the file lock**; only `advance`/`task add`/
   `task update` are lock-guarded. Never run two mutating commands touching the same plan
   concurrently — lost updates are silent (§8). Different plans in parallel are fine.
2. **`eligible` dep-gating is advisory, not authoritative** (§6.4): status drift (no PR cross-check),
   worktree-copy reads (stale checkout), silent intra-plan missing-dep block, and `blocks` never
   gating. Gate your own DAG; reconcile status via closeout; qualify cross-plan deps.
3. **Closeout ordering is enforced**: merge-back → (review-decision for gate) → closeout. Missing a
   merge-back or an incomplete-frontmatter merge-back → closeout error/silent-reject.
4. **`del-<task>-<epoch>` contract id names the bundle file** — capture it from `fanout` output
   (or `contract list --json`) to locate `.agents/active/delegation-bundles/<id>.yaml`.
5. **`advance` won't skip illegal edges** (validated against §4). To move a delegated task to
   `completed`, prefer the closeout path (which also archives + flips the plan) over a raw `advance`.
6. **`--commit-state` / start-task / close-task commit is a real git commit** of `.agents/**` state —
   honor `commit.disable`; in a swarm keep exactly one committer per plan to avoid interleaving.
7. **Sentinels never silently delete** (`clear` = archive-then-remove); a duplicate sentinel write is
   an error, and a duplicate hook-outcome append is an idempotent no-op.
8. **`fanout` refuses overlapping write scopes** (`checkFanoutWriteScopeConflicts`, fails closed on a
   corrupt contract) and enforces a Go TDD pre-check (`--skip-tdd-gate` to bypass). In HEAD it also
   runs the cross-package asserting-test REFUSE gate (`--skip-asserting-test-gate`) — **that gate and
   `--with-tests` are not in the 0.4.2 binary** (§0). Design the swarm's slices as disjoint write
   scopes, each with adjacent `*_test.go`, or pass `--skip-tdd-gate`/`--skip-evidence-check`
   (and, on HEAD, `--skip-asserting-test-gate`) deliberately.
