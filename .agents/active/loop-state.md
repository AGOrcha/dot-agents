# Loop State

Last updated: 2026-04-11
Iteration: 7

## Current Position

Driving specs:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — post-MVP workflow automation waves
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — KG subsystem with code-structure layer

Active wave summary (from `.agents/active/*.plan.md`):
- **active-artifact-cleanup**: Completed (2026-04-11) — all items done
- **crg-kg-integration**: Phases A-D complete. E/F active next (Postgres, Go MCP); G (skill integration) deferred
- **platform-dir-unification**: Blocked — Phases 4+5 need resource-intent-centralization implementation rollout
- **refresh-skill-relink**: Blocked on resource-intent-centralization
- **skill-import-streamline**: Blocked on resource-intent-centralization
- **resource-intent-centralization**: Phase 1 complete and Phase 2 started (2026-04-11) — declarative `ResourceIntent` model landed in `internal/platform`; planner/executor layer is next.

**Actionable implementation work remains.** `resource-intent-centralization` Phase 2 remains next (planner/executor layer after the ResourceIntent shape). `crg-kg-integration` Phases E/F are also active but lower priority.

As of iteration 7: 27/41 commands tested (no new command coverage this iteration), 12 scenario families covered. Resource-intent-centralization Phase 2 is now in progress: the shared `ResourceIntent` model is defined and regression-tested, but no planner/executor wiring has landed yet. Bootstrap, closeout, and partial analysis stacks are exercised. Mutation/reconciliation stacks still uncovered.

Note: Many older plans (kg-phase-1 through 5, wave-3 through 5) show "Completed" in their status header but still have unchecked `- [ ]` items. The status header is authoritative — unchecked boxes on completed plans are stale plan hygiene, not real work.

Analysis prep priority:
- The next loop-worthy improvement is not more summary prose; it is better evidence capture for the later analysis phase.
- Future iterations should leave behind structured trace metadata, scenario coverage, and explicit linkage between commands, commits, retries, and follow-on actions.

## Iteration Log

### Iteration 7 — 2026-04-11 09:54
- wave: resource-intent-centralization
- item: Phase 2 — Define an internal `ResourceIntent` shape for projection outputs
- scenario_tags: [clean-repo, repo-health-stack]
- files_changed: 2
- lines_added: 345
- lines_removed: 0
- tests_added: 5
- tests_total_pass: true
- retries: 0
- commit: 4920aeb
- scope_note: on-target
- summary: Added a declarative `ResourceIntent` / `ResourceSourceRef` model with typed ownership, shape, transport, replace/prune policies, validation, and focused tests in `internal/platform`

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: no
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 6 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 1 — Extract shared command spine into internal/projectsync
- scenario_tags: [clean-repo, managed-file-restore-stack, repo-health-stack]
- files_changed: 7
- lines_added: 194
- lines_removed: 80
- tests_added: 5 (internal/projectsync/projectsync_test.go)
- tests_total_pass: true
- retries: 0
- commit: 0053909
- scope_note: on-target (mapResourceRelToDest and restoreFromResourcesCounted deferred to Phase 3 — tightly coupled to import.go internals)
- summary: Created internal/projectsync with CopyFile, EnsureGitignoreEntry, CreateProjectDirs, WriteRefreshMarker, RefreshMarkerContent; removed duplicates from add.go, refresh.go, import.go, install.go, init.go; 5 new tests pass

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: yes (repo-health-stack post-refactor; workflow log as new untested command)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes (7 files)
- no_destructive_commands: yes

### Iteration 5 — 2026-04-11
- wave: none (no actionable implementation wave — evidence-capture-only iteration)
- item: Exercise uncovered write-command-path and kg-postprocess-complete scenarios
- scenario_tags: [write-command-path, kg-postprocess-complete, clean-repo, no-kg-home-configured]
- files_changed: 0 (loop-state.md only)
- lines_added: 0
- lines_removed: 0
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: (loop-state update only)
- scope_note: on-target
- summary: Exercised kg warm, kg warm stats, kg link add/list/remove, kg postprocess, kg flows (after postprocess), workflow checkpoint, workflow health (before+after checkpoint), workflow orient, workflow drift, workflow plan, status, doctor, kg health, kg query, kg lint. Captured write-path trace, postprocess-complete trace, and several ok-empty/ok-warning patterns.

Self-assessment:
- read_loop_state: yes
- one_item_only: yes (single focus: evidence capture across uncovered commands)
- committed_after_tests: yes
- ran_cli_command: yes (14 distinct commands)
- exercised_new_scenario: yes (write-command-path, kg-postprocess-complete, no-kg-home-configured)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 4 — 2026-04-11
- wave: active-artifact-cleanup
- item: Normalize stale plan state + record cleanup outcome in history + add lesson
- files_changed: 6
- lines_added: 65
- lines_removed: 3
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: 7aad9c1
- scope_note: on-target
- summary: Added Status/Depends-on headers to 3 blocked plans; wrote impl-results.2.md; extended archive-completed-active-plans lesson; marked active-artifact-cleanup complete

Self-assessment:
- read_loop_state: yes
- one_item_only: no (grouped normalize + lesson + history as one logical item — the plan listed them as separate items but they're a single coherent action)
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 3 — 2026-04-11 18:00
- wave: active-artifact-cleanup
- item: Archive completed plan files out of `.agents/active/` when the task is done or already has matching history coverage
- files_changed: 12
- lines_added: 0
- lines_removed: 11
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: 87bce37
- scope_note: on-target
- summary: Moved 12 completed plan files from .agents/active/ to matching history/ folders; active set reduced from 18 to 6 plans

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: no (12 files — all plan renames, no code changes; scope is correct)
- no_destructive_commands: yes

### Iteration 2 — 2026-04-11
- wave: crg-kg-integration
- item: Phase C — CRG advanced query bridge (impact, flows, communities, postprocess)
- files_changed: 3
- lines_added: ~350
- lines_removed: ~10
- tests_added: 0 (verified via live CLI)
- tests_total_pass: true
- retries: 1 (CRGImpactResult naming collision with existing ImpactResult in store.go)
- commit: (not recorded — backfilled)
- scope_note: on-target
- summary: Added runPyQuery helper, 4 CRGBridge methods, 4 kg subcommands (impact, flows, communities, postprocess)

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 1 — 2026-04-11
- wave: crg-kg-integration
- item: Phase B — CRG subprocess bridge and basic code-graph CLI commands
- files_changed: 6
- lines_added: ~500
- lines_removed: ~5
- tests_added: ~10 (internal/graphstore/crg_test.go)
- tests_total_pass: true
- retries: 0
- commit: (not recorded — backfilled)
- scope_note: expanded: also committed prior-session skill-architect transforms that were uncommitted
- summary: CRGBridge subprocess bridge to Python CRG, kg build/update/code-status/changes subcommands

Self-assessment:
- read_loop_state: yes
- one_item_only: no (also committed prior-session leftovers)
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 0 — 2026-04-11 (seed)
- wave: n/a (manual sessions)
- item: n/a
- files_changed: ~30
- lines_added: ~2000
- lines_removed: ~200
- tests_added: ~37 (internal/graphstore/sqlite_test.go)
- tests_total_pass: true
- retries: n/a
- commit: (multiple, not tracked)
- scope_note: n/a (seed from Codex sessions 019d7a6d and 019d7a9d)
- summary: GraphStore interface + SQLite backend, managed resource cleanup, AGENTS.md, skill transforms, AgentsRC schema, resource-intent-centralization plan

## What's Next

**Actionable implementation waves remain.** The remaining active plans:
- 1 completed cleanup plan (active-artifact-cleanup — archive when convenient)
- 3 plans blocked on `resource-intent-centralization` rollout (`platform-dir-unification`, `refresh-skill-relink`, `skill-import-streamline`)
- `resource-intent-centralization` Phase 2 is in progress; the `ResourceIntent` model is done and the planner/executor layer is next
- `crg-kg-integration` Phases E/F remain active; Phase G deferred

Recommended implementation order:
- `resource-intent-centralization` Phase 2: add the planner/executor layer that consumes `internal/platform.ResourceIntent`
- Then Phase 3 (centralize shared repo targets) and Phase 4 (thin platform adapters) to unblock the blocked plans
- `crg-kg-integration` Phase E/F are lower priority until resource-intent lands

Evidence capture is now broad enough to support command-landscape analysis, but still uneven across state families. Read-only workflow coverage is strong; repo-local write paths have some traces; the biggest remaining gaps are canonical workflow write paths, delegation flows, initialized KG lifecycle, bridge/config states, CRG build/update end-to-end runs, and mutation/reconciliation stacks.

Analysis follow-on once implementation resumes:
- Prefer iterations that exercise uncovered commands or new workflow states over repeating the same happy-path checks.
- Capture at least some expected empty-state, warning, retry-recovered, and blocked traces; a later analysis pass will be weak if it only sees `[ok]` outcomes.
- Link each interesting trace to the triggering plan item, commit, and any retry/error entry so later review can reconstruct cause and effect quickly.

## Analysis Readiness

Questions the later analysis phase should be able to answer:
- Which commands are reliable only on happy paths vs across realistic repo states?
- Which workflow states produce the most retries, ambiguity, or operator intervention?
- Which outputs are correct but still create UX friction or weak operator guidance?
- Which agent loops stay tightly scoped vs drift or require cleanup work afterward?

Signals already captured:
- Per-iteration summary, scope, retries, commit, and basic self-assessment
- Exact CLI invocations with short output snapshots and structured metadata (scenario, expectation, follow-on, classification)
- Command-level coverage tracking (27 of ~41 commands now tested)
- Scenario coverage grouped by workflow, KG, CRG, bridge/config, delegation, integration, and outcome-quality families
- The `ResourceIntent` contract itself is now codified in Go with validation tests, reducing ambiguity before planner/executor implementation starts
- Write-command-path traces: kg warm, kg link CRUD, workflow checkpoint
- Empty-state traces: kg health/query/lint (no KG_HOME), workflow tasks/plan (no PLAN.yaml), kg flows (no igraph)
- Warning-state traces: kg link orphan, workflow status next-action parsing, kg flows misleading help text
- Before/after traces: workflow health warn→healthy after checkpoint write
- Small integration traces already exist, especially status/doctor/workflow-health and checkpoint→health improvement; these now anchor the first bootstrap and closeout stacks

Signals still missing or too weak:
- Canonical workflow state transitions: `workflow advance`, `workflow verify`, and plan/task flows with real `PLAN.yaml` + `TASKS.yaml` (workflow log now covered)
- Delegation lifecycle: `workflow fanout` and `workflow merge-back`, including conflict/error paths
- Cross-project remediation: `workflow sweep` dry-run/apply and drift cases that detect real stale state rather than empty/no-op results
- Initialized KG lifecycle: `kg setup`, `kg ingest`, `kg queue`, `kg query`, `kg lint`, `kg maintain`, and `kg sync`
- CRG end-to-end build/update traces and subprocess failure paths, not just read/query style commands
- Bridge/config states for both `workflow graph` and `kg bridge`
- `add`/`install`/`refresh` commands not exercised directly — indirect confidence from clean test suite post-refactor, but no live integration trace of the managed-file-restore-stack pattern

Minimum capture rules for remaining work:
- Every iteration should declare one or more scenario tags from the family buckets below; when possible, include both one command-coverage gain and one state-transition gain
- Every CLI trace should record whether the result was expected, unexpected, or informative-but-nonblocking
- Every retry or detour should leave an Error Log entry even if the final outcome is green
- Prefer paired state coverage when useful: uninitialized vs initialized, disabled vs enabled, dry-run vs apply, empty vs populated, raw vs postprocess-complete

## Scenario Coverage

Coverage is grouped by state family so later analysis can distinguish "which command ran" from "what situation it exercised."

### Workflow Project State

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `clean-repo` | yes | 7 | Reconfirmed after the `ResourceIntent` model commit; `status`, `workflow health`, and `doctor` all ran from a clean repo state |
| `dirty-repo` | yes | 3 | `workflow status` reported `dirty files: 1` |
| `legacy-plan-only` | no | - | no iteration has isolated next-action behavior driven only by `.agents/active/*.plan.md` unchecked items |
| `no-canonical-plan` | yes | 5 | `workflow plan`, `workflow tasks`, and `workflow drift` all returned expected empty/no-plan states |
| `canonical-plan-present` | no | - | requires a real `PLAN.yaml` + `TASKS.yaml` plan to exist |
| `current-focus-task-present` | no | - | requires active canonical plan state with `current_focus_task` set |
| `blocked-plan-set` | yes | 5 | `workflow orient` rendered blocked and deferred active plans correctly |
| `blocked-task-visible` | no | - | requires canonical tasks with `blocked` status surfaced by `workflow tasks` or `plan show` |

### Workflow Write Paths

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `checkpoint-written` | yes | 5 | `workflow checkpoint` created a checkpoint and improved `workflow health` output |
| `workflow-log-visible` | yes | 6 | `workflow log` showed checkpoint from prior iteration; next_action UX issue confirmed |
| `workflow-advance-success` | no | - | requires canonical plan/task state to mutate |
| `verification-log-recorded` | no | - | `workflow verify record/log` not exercised yet |
| `shared-pref-proposal-pending` | no | - | requires approval-gated write path outside repo |
| `review-approve-reject-loop` | no | - | depends on queued shared preference proposals |

### Delegation Lifecycle

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `fanout-success` | no | - | `workflow fanout` not exercised yet |
| `fanout-write-scope-conflict` | no | - | no overlapping delegation contract has been created/tested |
| `merge-back-success` | no | - | `workflow merge-back` not exercised yet |
| `merge-back-without-contract` | no | - | missing-contract error path not exercised yet |

### Cross-Project Workflow Ops

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `multi-project-drift-empty` | yes | 5 | `workflow drift` ran, but only in the no-plan / no-remediation-needed path |
| `multi-project-drift-detected` | no | - | need a stale checkpoint or missing workflow dir in a managed project |
| `sweep-dry-run` | no | - | `workflow sweep` not exercised yet |
| `sweep-apply-confirmed` | no | - | apply path is both untested and more invasive |

### KG Lifecycle

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `no-kg-home-configured` | yes | 5 | `kg health`, `kg query`, and `kg lint` fail clearly with setup guidance |
| `kg-setup-complete` | no | - | requires `kg setup`, which writes outside the repo and needs approval |
| `kg-empty-but-healthy` | no | - | would be the post-setup, pre-ingest baseline |
| `kg-ingest-queue-drain` | no | - | `kg queue` + `kg ingest --all` path untested |
| `kg-query-success` | no | - | blocked until KG is initialized with real notes |
| `kg-lint-success` | no | - | blocked until KG is initialized |

### KG Maintenance And Storage Integrity

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `write-command-path` | yes | 5 | repo-local write-style traces exist for `kg warm`, `kg link`, and `workflow checkpoint` |
| `warm-layer-empty` | yes | 5 | `kg warm` / `kg warm stats` succeeded with zero notes and a created SQLite DB |
| `warm-layer-populated` | no | - | requires initialized KG notes to sync into SQLite |
| `orphan-link` | yes | 5 | `kg link add` accepted a non-existent note id; referential integrity gap documented |
| `kg-lint-repair-cycle` | no | - | `kg maintain reweave` plus re-lint path untested |
| `stale-note-marking` | no | - | `kg maintain mark-stale` not exercised |
| `compact-archives-superseded` | no | - | `kg maintain compact` not exercised |
| `kg-sync-pull-lint` | no | - | `kg sync` path not exercised |

### CRG And Code-Graph States

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `crg-read-surface` | yes | 2 | `kg code-status`, `kg changes`, `kg impact`, and `kg communities` exercised read/query behavior |
| `crg-build-complete` | no | - | `kg build` not exercised yet |
| `crg-update-from-diff` | no | - | `kg update` not exercised yet |
| `kg-pre-postprocess` | yes | 2 | `kg flows` was empty before `kg postprocess` |
| `kg-postprocess-complete` | yes | 5 | `kg postprocess` rebuilt communities and FTS; flows stayed 0 without igraph |
| `postprocess-complete-with-real-flows` | no | - | requires igraph-capable environment and/or richer graph data |
| `crg-subprocess-failure` | no | - | no forced missing-binary or subprocess stderr path captured yet |

### Bridge And Config States

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `workflow-graph-disabled` | no | - | `workflow graph health/query` not exercised yet |
| `workflow-graph-enabled` | no | - | requires `.agents/workflow/graph-bridge.yaml` configured and enabled |
| `bridge-intent-disallowed` | no | - | disallowed-intent guard path untested |
| `kg-bridge-mapping-reviewed` | no | - | `kg bridge health/mapping/query` not exercised yet |

### Cross-Subsystem Integration Checks

These are larger chained checks that cross subsystem boundaries. Grouping them by stack type makes it easier to ask later whether the system is strongest at bootstrap, reconciliation, analysis, or closeout work.

#### Bootstrap Stacks

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `repo-health-stack` | yes | 7 | Re-run after the `ResourceIntent` model commit; `status` → `workflow health` → `doctor` stayed coherent end-to-end on the current branch |
| `project-add-health-stack` | no | - | would cover `add` -> `status` -> `doctor` -> `workflow status` for managed-project bootstrap |
| `kg-bootstrap-stack` | no | - | grounded in past session history: `kg setup` -> `kg health` -> `kg queue` |
| `managed-project-sync-stack` | no | - | would cover `add` or `import` -> `sync` -> `status` -> `doctor` |

#### Mutation And Reconciliation Stacks

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `projection-refresh-integrity-stack` | no | - | projected-file mutation or refresh-style operation followed by `status` / `doctor` / managed-file inspection |
| `managed-file-restore-stack` | no | - | inspired by the AGENTS overwrite incident: detect overwrite -> restore -> `status` -> `doctor` -> `workflow health` |
| `prefs-review-health-stack` | no | - | would cover `workflow prefs set-shared` -> `review show/approve/reject` -> `workflow health` |
| `drift-remediation-stack` | no | - | would validate `workflow drift` -> `workflow sweep` -> follow-up `status` / `workflow health` |

#### Analysis And Readback Stacks

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `kg-ingest-validation-stack` | no | - | grounded in past session history: `kg ingest` -> `kg health` -> `kg query` or `kg queue` |
| `kg-crg-postprocess-stack` | no | - | `kg build` or `kg update` -> `kg postprocess` -> `kg code-status` / `kg flows` |
| `workflow-kg-bridge-stack` | no | - | would connect `workflow graph` commands with `kg bridge` health/query/mapping in one scenario |
| `kg-to-workflow-memory-stack` | no | - | would connect KG evidence creation (`kg ingest` or `kg warm`) with `workflow checkpoint` / `workflow health` |

#### Closeout And Evidence Stacks

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `checkpoint-health-stack` | yes | 6 | `workflow checkpoint` → `workflow health` → `workflow log` now all covered in a single closeout chain |
| `kg-write-workflow-checkpoint-stack` | yes | 5 | `kg warm`, `kg link` CRUD, and `workflow checkpoint` were all exercised in one evidence-gathering pass |
| `verification-checkpoint-stack` | no | - | would cover `workflow verify record` -> `workflow checkpoint` -> `workflow log` -> `workflow health` |
| `loop-iteration-closeout-stack` | no | - | would tie plan/tasks, verification, checkpoint, and health/log readback into one end-of-iteration chain |

### Outcome-Quality States

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `ok-empty-expected` | yes | 5 | multiple no-op/empty traces recorded and classified explicitly |
| `ok-warning-ux-friction` | yes | 5 | warnings captured for orphan links, misleading flows guidance, and next-action parsing |
| `retry-recovered` | yes | 2 | naming collision fixed before final commit |
| `retry-recovered-with-error-log` | no | - | retry happened, but no matching `## Error Log` entry exists yet |
| `pre-existing-tool-bug-confirmed` | yes | 5 | `kg flows` help text is wrong after postprocess when igraph is absent |
| `blocked-environment` | no | - | no scenario explicitly tagged as blocked by missing dependency or approval gate |

## Skip List

Plans to skip (blocked, requires architectural work, completed, or out of scope for loop):
- `refresh-skill-relink` — blocked on resource-intent-centralization
- `skill-import-streamline` — blocked on resource-intent-centralization
- `platform-dir-unification` — blocked on resource-intent-centralization
- `crg-kg-integration` Phase G — deferred until Phases E/F land and are exercised

(Completed plans archived to .agents/history/ on 2026-04-11:)
- kg-phase-1 through kg-phase-6, wave-3 through wave-7, workflow-automation-product-spec-review, agentsrc-local-schema, resource-sync-architecture-analysis

## Blockers

- Shared `.agents/skills/*` projection bug: repo-local skill directories are not converted to managed symlinks after import. Documented in `skill-import-streamline.plan.md`. Architectural direction is now captured in `docs/rfcs/resource-intent-centralization-rfc.md`; implementation remains outstanding.
- `plan-wave-picker` SKILL.md at `~/.agents/skills/dot-agents/plan-wave-picker/SKILL.md` has invalid frontmatter (missing `---` delimiters). Codex warns on load.

## CLI Traces

### Iteration 7 — 2026-04-11

Trace: repo-health-stack-after-resource-intent-model (integration: bootstrap stack)
Chain: `status` → `workflow health` → `doctor`
```
$ go run ./cmd/dot-agents status
dot-agents, ResumeAgent, payout — all projects showing healthy manifest/platform summaries; pre-existing user-config warnings and unfetched git source still surfaced
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: healthy, branch: feature/PA-cursor-projectsync-phase1-extract-293f, dirty files: 0, has active plan: true, canonical plans: 0, has checkpoint: true
$ go run ./cmd/dot-agents doctor
Installation: ✓, Platforms: ✓ (4/5 installed), User Config: ! 4 broken Claude skill links (pre-existing), Projects: ✓ (3), Link Health: ✓, dot-agents manifest: ! git source not yet fetched
```
Scenario: [clean-repo, repo-health-stack]
Expectation: expected — the new internal model is not wired into command behavior yet, so repo health should remain unchanged
Follow-on: none
Classification: [ok-warning]

### Iteration 6 — 2026-04-11

Trace: repo-health-stack-post-refactor (integration: bootstrap stack)
Chain: `status` → `workflow health` → `doctor`
```
$ go run ./cmd/dot-agents status
dot-agents, ResumeAgent, payout — all showing ✓ platforms+manifest (same as iteration 5)
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: healthy, branch: feature/PA-cursor-projectsync-phase1-extract-293f, dirty: 0, has checkpoint: true
$ go run ./cmd/dot-agents doctor
Installation: ✓, Platforms: ✓ (4/5), User Config: ! 4 broken links (pre-existing), Projects: ✓ (3)
```
Scenario: [clean-repo, repo-health-stack]
Expectation: expected — refactor should not affect command behavior
Follow-on: none
Classification: [ok]

Trace: workflow-log-checkpoint-readable (integration: closeout-and-evidence stack)
Chain: `workflow checkpoint` (prior iter) → `workflow health` → `workflow log`
```
$ go run ./cmd/dot-agents workflow log
Workflow Log — 2026-04-11T06:36:53Z, branch: feature/workflow-auto-operator, sha: 4ed7421
next_action: Status: Completed (2026-04-11)
```
Scenario: [checkpoint-written, workflow-log-visible]
Expectation: expected — log shows the checkpoint written in iteration 5
Follow-on: documented — next_action still shows literal status header (already documented); workflow-log-visible scenario now covered
Classification: [ok]

### Iteration 5 — 2026-04-11

Trace: kg-warm-no-kg-home
```
$ go run ./cmd/dot-agents kg warm
✓ Warm sync complete: 0 notes indexed, 0 skipped
```
Scenario: [write-command-path, no-kg-home-configured]
Expectation: informative-nonblocking — KG_HOME (~/.knowledge-graph) not initialized, so no notes to sync
Follow-on: documented
Classification: [ok-empty]

Trace: kg-warm-stats-no-kg-home
```
$ go run ./cmd/dot-agents kg warm stats
Warm Layer Stats — Notes indexed: 0, Symbol links: 0, Code nodes: 0, Code edges: 0
  Last warm sync: 2026-04-11T06:34:52Z, DB path: /Users/nikashp/knowledge-graph/ops/graphstore.db
```
Scenario: [write-command-path, no-kg-home-configured]
Expectation: expected — warm DB exists even without KG_HOME notes
Follow-on: none
Classification: [ok-empty]

Trace: kg-link-add-nonexistent-note
```
$ go run ./cmd/dot-agents kg link add nonexistent-note "commands::runKGWarm"
✓ Link created (id=1): nonexistent-note -[mentions]-> commands::runKGWarm
```
Scenario: [write-command-path, orphan-link]
Expectation: unexpected — no referential integrity check against note existence
Follow-on: documented — potential data quality issue; `kg link add` should validate note-id exists
Classification: [ok-warning]

Trace: kg-link-list
```
$ go run ./cmd/dot-agents kg link list nonexistent-note
  [1] nonexistent-note -[mentions]-> commands::runKGWarm
```
Scenario: [write-command-path]
Expectation: expected
Follow-on: none (test link removed with `kg link remove 1`)
Classification: [ok]

Trace: kg-postprocess-complete
```
$ go run ./cmd/dot-agents kg postprocess
Running post-processing on /Users/nikashp/Documents/dot-agents ...
INFO: FTS index rebuilt: 923 rows indexed
INFO: igraph not available, using file-based community detection
Post-processing: 50 communities, 923 FTS entries
```
Scenario: [kg-postprocess-complete]
Expectation: expected — igraph unavailable note is informational
Follow-on: none
Classification: [ok]

Trace: kg-flows-after-postprocess
```
$ go run ./cmd/dot-agents kg flows
Execution Flows — Found 0 execution flow(s)
No flows detected. Run 'dot-agents kg postprocess' to detect flows.
```
Scenario: [kg-postprocess-complete]
Expectation: unexpected — flows still 0 even after postprocess; help text still says to run postprocess (misleading)
Follow-on: documented — igraph is required for flow detection; without igraph the help text is incorrect. Should say "install igraph for flow detection"
Classification: [ok-warning]

Trace: workflow-checkpoint-write
```
$ go run ./cmd/dot-agents workflow checkpoint
✓ Checkpoint written
  ~/.agents/context/dot-agents/checkpoint.yaml
```
Scenario: [write-command-path, clean-repo]
Expectation: expected
Follow-on: none
Classification: [ok]

Trace: workflow-health-before-checkpoint
```
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: warn
Warnings: - no checkpoint recorded
```
Scenario: [write-command-path]
Expectation: expected — no checkpoint existed before
Follow-on: none
Classification: [ok]

Trace: workflow-health-after-checkpoint
```
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: healthy
  has active plan: true, canonical plans: 0, has checkpoint: true, pending proposals: 0
```
Scenario: [write-command-path]
Expectation: expected — checkpoint resolves the warn state
Follow-on: none
Classification: [ok]

Trace: workflow-orient-blocked-plan-set
```
$ go run ./cmd/dot-agents workflow orient
# Project — branch: feature/workflow-auto-operator, sha: 4ed7421, dirty files: 1
# Canonical Plans — none
# Active Plans — (includes all 6 active/*.plan.md files with status/depends-on headers visible)
```
Scenario: [blocked-plan-set]
Expectation: expected — orient renders all active plans verbatim including blocked ones
Follow-on: none
Classification: [ok]

Trace: workflow-drift-no-workflow-dir
```
$ go run ./cmd/dot-agents workflow drift
Workflow Drift Report — 3 projects checked, all warn
  ResumeAgent: no checkpoint found, no .agents/workflow/ directory
  dot-agents: no .agents/workflow/ directory
  payout: no checkpoint found, no .agents/workflow/ directory
Summary: healthy: 0, warnings: 3, unreachable: 0
```
Scenario: [no-canonical-plan, blocked-plan-set]
Expectation: expected — no projects have initialized .agents/workflow/ directories
Follow-on: documented
Classification: [ok-empty]

Trace: workflow-status-after-checkpoint
```
$ go run ./cmd/dot-agents workflow status
Next Action: Status: Completed (2026-04-11)
```
Scenario: [write-command-path]
Expectation: unexpected — "Next Action" renders the status-header text of the first active plan literally
Follow-on: documented — checkpoint serializes first active plan's `Status:` field as next_action verbatim; this is misleading UX
Classification: [ok-warning]

Trace: workflow-tasks-no-yaml-plan
```
$ go run ./cmd/dot-agents workflow tasks crg-kg-integration
Error: plan "crg-kg-integration" not found: open .../PLAN.yaml: no such file or directory
```
Scenario: [no-canonical-plan]
Expectation: expected — `workflow tasks` requires PLAN.yaml canonical plans, not .plan.md files
Follow-on: none
Classification: [ok-empty]

Trace: status-full
```
$ go run ./cmd/dot-agents status
dot-agents, ResumeAgent, payout — all projects showing ✓ on platforms+manifest
User Config: ! 4 broken links (.claude/skills/debug-test-skill*)
dot-agents manifest: ! git source not yet fetched
```
Scenario: [clean-repo]
Expectation: expected — broken links are pre-existing, git source requires explicit install
Follow-on: none
Classification: [ok-warning]

Trace: doctor-broken-links
```
$ go run ./cmd/dot-agents doctor
User Config: ! 4 broken link(s) — .claude/skills/debug-test-skill variants
dot-agents: ! git source not yet fetched
```
Scenario: [clean-repo]
Expectation: expected — pre-existing environment issue, not a regression
Follow-on: none
Classification: [ok-warning]

Trace: kg-health-no-kg-home
```
$ go run ./cmd/dot-agents kg health
Error: knowledge graph not initialized at /Users/nikashp/knowledge-graph — run 'dot-agents kg setup' first
```
Scenario: [no-kg-home-configured]
Expectation: expected — KG_HOME not initialized
Follow-on: none
Classification: [ok-empty]

Trace: kg-query-no-kg-home
```
$ go run ./cmd/dot-agents kg query "GraphStore interface implementation"
Error: knowledge graph not initialized — run 'dot-agents kg setup' first
```
Scenario: [no-kg-home-configured]
Expectation: expected
Follow-on: none
Classification: [ok-empty]

Trace: kg-lint-no-kg-home
```
$ go run ./cmd/dot-agents kg lint
Error: knowledge graph not initialized — run 'dot-agents kg setup' first
```
Scenario: [no-kg-home-configured]
Expectation: expected
Follow-on: none
Classification: [ok-empty]

### Iteration 4 — 2026-04-11
```
$ go run ./cmd/dot-agents workflow status
Workflow Status — branch: feature/PA-cursor-kg-build-update-commands-1b58, sha: 7aad9c1, dirty: 0, active plans: 6, lessons: 10
```
Classification: [ok]

### Iteration 3 — 2026-04-11
```
$ go run ./cmd/dot-agents workflow plan
No canonical plans found.
```
Classification: [ok] — expected, no PLAN.yaml files have been created yet

```
$ go run ./cmd/dot-agents workflow status
Workflow Status — active plans: 6, dirty files: 1
```
Classification: [ok]

### Iteration 2 — 2026-04-11
```
$ go run ./cmd/dot-agents kg impact commands/kg.go --limit 10
Impact Radius — 112 changed nodes, 10 impacted within 2 hops, 2 impacted files, 122 total
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg communities --min-size 5
Code Communities — Found 41 communities (top: commands-workflow size=139, commands-graph size=112)
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg flows
Execution Flows — Found 0 (no flows without postprocess run)
```
Classification: [ok] — flows require `kg postprocess` first, which is expected.

### Iteration 1 — 2026-04-11
```
$ go run ./cmd/dot-agents kg code-status
Code Graph Status — Nodes: 923, Edges: 6281, Files: 50, Languages: go ruby, Last updated: 2026-04-11T00:49:52
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg changes --brief
Change Impact — Analyzed 15 changed file(s): 5 changed functions, 0 flows, 5 test gaps, risk 0.30
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg changes
Change Impact — structured output with changed symbols, test gaps, review priorities
```
Classification: [ok]

## Command Coverage

| Command | Tested | Last Iteration | Status |
|---|---|---|---|
| `status` | yes | 7 | ok-warning |
| `doctor` | yes | 7 | ok-warning |
| `workflow status` | yes | 5 | ok-warning |
| `workflow orient` | yes | 5 | ok |
| `workflow checkpoint` | yes | 5 | ok |
| `workflow log` | yes | 6 | ok |
| `workflow plan` | yes | 5 | ok-empty |
| `workflow tasks` | yes | 5 | ok-empty |
| `workflow advance` | no | - | - |
| `workflow health` | yes | 7 | ok |
| `workflow verify` | no | - | - |
| `workflow prefs` | no | - | - |
| `workflow graph` | no | - | - |
| `workflow fanout` | no | - | - |
| `workflow merge-back` | no | - | - |
| `workflow drift` | yes | 5 | ok-empty |
| `workflow sweep` | no | - | - |
| `kg setup` | no | - | - |
| `kg health` | yes | 5 | ok-empty |
| `kg ingest` | no | - | - |
| `kg queue` | no | - | - |
| `kg query` | yes | 5 | ok-empty |
| `kg lint` | yes | 5 | ok-empty |
| `kg maintain` | no | - | - |
| `kg bridge` | no | - | - |
| `kg sync` | no | - | - |
| `kg warm` | yes | 5 | ok-empty |
| `kg warm stats` | yes | 5 | ok-empty |
| `kg link add` | yes | 5 | ok-warning |
| `kg link list` | yes | 5 | ok |
| `kg link remove` | yes | 5 | ok |
| `kg build` | no | - | - |
| `kg update` | no | - | - |
| `kg code-status` | yes | 1 | ok |
| `kg changes` | yes | 1 | ok |
| `kg changes --brief` | yes | 1 | ok |
| `kg impact` | yes | 2 | ok |
| `kg communities` | yes | 2 | ok |
| `kg flows` | yes | 5 | ok-warning |
| `kg postprocess` | yes | 5 | ok |

## Error Log

(No errors recorded yet)

<!--
Format:
### Iteration N
- type: test-failure | compile-error | cli-error
- detail: <what failed>
- resolution: <what fixed it>
- retries: N
-->

## CLI Observations

- `kg changes` returns qualified names with absolute file paths (e.g. `/Users/.../kg.go::runKGWarm`) — CRG uses absolute paths internally. This is slightly noisy in output; could be made relative to repo root in a future iteration.
- `--brief` flag sends human-readable text from CRG, not JSON. The bridge handles both modes correctly now.
- `kg build` and `kg update` stream output directly; no structured return — good for interactive use.
- `kg impact` output includes File-level nodes in "Changed nodes" section — these add noise; filtered out in the render pass (only non-File nodes shown). Good UX decision.
- `kg flows` help text says "Run 'dot-agents kg postprocess' to detect flows" but the real requirement is `igraph` being installed. After postprocess without igraph, flows are still 0 and the message is misleading. Should say "Install igraph or run in an environment with igraph available."
- `runPyQuery` pattern works cleanly for calling CRG Python tool functions without needing a full MCP server — useful pattern for adding more CRG capabilities later.
- `kg warm` silently succeeds with 0 notes when KG_HOME doesn't exist — reasonable behavior but could print a hint: "KG_HOME not initialized, run 'dot-agents kg setup' to create note directories."
- `kg link add` accepts any string as note-id without checking if the note exists in the warm DB or filesystem. Creates orphaned links. A future iteration should add a soft validation warning.
- `workflow status` Next Action field shows the literal `Status: <text>` header from the first active plan — this is the plan's status line, not a meaningful next action. The field needs a smarter extraction strategy.
- `workflow drift` flags all 3 registered projects as warn because none have `.agents/workflow/` directories. This is by design (PLAN.yaml workflow not used), but operators would see this as noisy on every run. Consider filtering or suppressing for projects without canonical workflow initialization.
- `workflow checkpoint` and `workflow health` work well as a write→verify pair: checkpoint clears the "no checkpoint" warning in health. Good UX feedback loop.
