# Loop State

Last updated: 2026-04-11
Iteration: 27

## Current Position

Driving specs:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — post-MVP workflow automation waves
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — KG subsystem with code-structure layer

Active waves:
- `resource-intent-centralization`: Phase 4 COMPLETE. Phase 3 in_progress — shared-target plan covers skills, `.claude/agents/*` dir mirrors, Codex `.codex/agents/*.toml` renders, OpenCode `.opencode/agent/*.md` and Copilot `.github/agents/*.agent.md` file symlinks. Phase 5 — refresh/install/add use `RunSharedTargetProjection`; Phase 6 — dedupe/conflict aggregation tests, import conflict preserve-both, and **full `runRefresh` regression** for imported skill dir → managed symlink (`TestRefreshReplacesImportedRepoSkillDirWithManagedSymlink`); executor-only allowlist + status/explain registry bullets remain.
- `skill-import-streamline`: **Completed** — manifest preservation, `install --generate` merge, `skills promote` with copy-move convergence, `TestPromoteSkillIn_PreservesManifestUnknownFields` regression; canonical plan/tasks marked completed.
- `crg-kg-integration`: Phases A-D complete. Phase E (Postgres backend) and Phase F (Go MCP server) remain active.

Blocked waves (reassessed):
- `platform-dir-unification`: Phase 4 (bash parity) is still deferrable — no urgency, bash path is orthogonal.
- `refresh-skill-relink`: effectively done (all root-cause items are resolved by resource-intent-centralization). Remaining item is a regression test that requires running `refresh`, which is guardrail-blocked. Can update status to reflect this.

State summary:
- `skills promote <name>` command added: creates managed symlink in `agentsHome/skills/<project>/<name>` pointing to repo-local skill, registers in `.agentsrc.json`, calls `ExecuteSharedSkillMirrorPlan` for platform mirrors. 5 unit tests cover success, idempotency, and 3 error paths.
- `ExecuteSharedSkillMirrorPlan` now has its first caller from the command layer (`skills promote`).
- The intended workflow surfaces are now genuinely usable for this repo: `workflow orient`, `workflow status`, `workflow health`, `workflow plan`, and `workflow tasks crg-kg-integration` all return meaningful data.
- Canonical plan inventory is no longer empty: `workflow plan` currently lists 6 canonical plans, and `workflow tasks crg-kg-integration` shows Phase E in progress with dependent Phase F/G pending.
- `workflow status` next action is currently stale checkpoint data (`Status: Completed (2026-04-11)`), so wave selection must rely on `workflow orient` + `workflow plan/tasks` + loop-state until checkpoint persistence is refreshed.
- `skills promote` now does copy-move convergence: canonical path (`~/.agents/skills/<project>/<name>/`) is a real directory; repo-local (`.agents/skills/<name>/`) becomes a managed symlink. Prevents circular symlinks during platform mirror refresh.
- 6 tests cover success (convergence), idempotency, and 4 error paths (not found, no project name, mispointing symlink, canonical real-dir clash).
- `skill-import-streamline` wave closed: regression test locks promote path preservation of `ExtraFields` + multi-source `sources` through `Save()` after skill registration.
- `DryRunSharedTargetPlanLines` surfaces the same merged `ResourcePlan` as `CollectAndExecuteSharedTargetPlan` for `refresh --dry-run` and `install --dry-run` (no filesystem writes).
- `BuildSharedAgentMirrorIntents` + extended allowlist (`.claude/agents/`) centralizes project-scoped canonical `agents/<project>/<name>/AGENT.md` mirrors into `.claude/agents/<name>`; Cursor emits the same targets so Claude+Cursor duplicate intents merge once. Three new unit tests cover dedupe, imported-dir replacement, and Claude+Cursor execute path.
- Phase 3 non-dir agent outputs: Codex `.codex/agents/*.toml` (render), OpenCode `.opencode/agent/*.md` and Copilot `.github/agents/*.agent.md` (file symlinks) now go through `CollectAndExecuteSharedTargetPlan` via `BuildSharedCodexAgentTomlIntents` / `BuildSharedAgentFileSymlinkIntents`; executor handles `RenderSingle`+`write` and `DirectFile`+`symlink`. Adapters thinned; `pruneCodexRepoAgentTomls` preserves stale-toml cleanup.
- Phase 5 started: `status --audit` and `doctor --verbose` audit tail call `DryRunSharedTargetPlanLines` via `InstalledEnabledPlatforms` (same merge rules as `refresh --dry-run`). `explain links` documents the registry diagnostic path. `refresh` now computes installed+enabled platforms once per run.
- Phase 5 library slice: `BuildSharedTargetPlan` is the single intent aggregation + `BuildResourcePlan` merge path for both dry-run lines and `CollectAndExecuteSharedTargetPlan` execute (no duplicate collection/merge between dry-run and apply).
- `remove` runs `RemoveSharedTargetPlan` (same merged plan as install/refresh) for all **installed** platforms before per-platform `RemoveLinks`; unit tests cover skill symlink + Codex TOML teardown.
- `RunSharedTargetProjection(project, repoPath, platforms, dryRun)` wraps `DryRunSharedTargetPlanLines` vs `CollectAndExecuteSharedTargetPlan` so refresh, install (`createInstallPlatformLinks`), and `add` share one command-layer entry point for shared targets (tests: dry-run parity + apply returns nil lines).
- Phase 6 (partial): `internal/platform/resource_plan_test.go` adds `stubPlatform` and tests that `BuildSharedTargetPlan` / `DryRunSharedTargetPlanLines` surface dedupe, `conflicting intents`, and platform `SharedTargetIntents` failures on the aggregation path (not only direct `BuildResourcePlan`).
- Phase 6 (partial): **import conflict** — canonical hook `importOutput` carries `Origin` (platform id); when on-disk content differs, `processImportOutput` writes an origin-prefixed alternate under `hooks/<scope>/…` and an advisory YAML under `AGENTS_HOME/review-notes/import-conflicts/` (RFC §6–§7); replace-with-confirm remains when `Origin` is empty. Covered by unit + integration tests (`TestProcessImportOutput_preservesHookConflict`, dry-run branch).
- Phase 6 (partial): **refresh/import regression** — `TestRefreshReplacesImportedRepoSkillDirWithManagedSymlink` drives full `runRefresh` (import-from-refresh → `RunSharedTargetProjection` → Claude `CreateLinks`) and asserts repo `.agents/skills/review/` becomes a symlink to `~/.agents/skills/proj/review` when canonical skill exists and Claude is the sole installed+enabled platform (temp `HOME` + `.claude` stub).
- Next slice: Phase 6 remaining bullets (executor-only replacement allowlist tests, status/explain registry tests); optional adapter thinning; or `crg-kg-integration` Phase E incremental work.

## Loop Health

Review target: iterations 18-20 and paired commits.

Current findings:
- `single-commit-closeout`: on-target — iteration 17 targets one commit (tests + loop-state + plan YAML + canonical advances).
- `coverage-reconciliation`: on-target — new scenario tag `promote-preserves-extra-manifest`; Command Coverage updated for `workflow advance` and `workflow tasks`.
- `playbook-hygiene`: on-target — playbook rewritten for iteration 18.
- `evidence-signal`: primary proof is new `TestPromoteSkillIn_PreservesManifestUnknownFields` plus full `go test ./...`; CLI: `workflow tasks skill-import-streamline` after advancing tasks.
- `canonical-yaml-drift`: improved — `install-generate-merge` was pending while merge code/tests existed; advanced to completed alongside `add-regression-tests`.
- `workflow-dogfooding`: needs improvement — orient still lists some plans as paused until PLAN.yaml refresh propagates; use `workflow tasks` for task truth.
- `canonical-plan-reality`: improved — skill-import-streamline canonical plan marked completed in PLAN.yaml.
- `checkpoint-freshness`: unchanged — `workflow status` next action remains stale checkpoint text; treat as baseline.

Operating rules for iteration 18+:
- Prefer one final commit per iteration that includes code plus loop-state/plan updates.
- Use one primary evidence chain plus at most one secondary probe; reconcile coverage tables before closeout.
- Rewrite summary sections in place; do not append duplicate playbook blocks.
- Start each iteration with `workflow orient`, `workflow status`, and `workflow plan`; if the chosen wave has a canonical plan, run `workflow tasks <id>` before selecting the exact checklist item.
- Treat checkpoint-backed `workflow status` as runtime readback and canonical `workflow tasks` as machine-readable plan truth; if they disagree, log it rather than guessing.
- Prefer sandboxed `workflow checkpoint` / `workflow verify record` for closeout dogfooding when real `~/.agents` writes are not explicitly approved.
- After completing a canonical plan's tasks, run `workflow advance` for each task and align PLAN.yaml status with markdown plan headers.

## Workflow Command Baseline

Current repo baseline from live readback:
- `workflow orient`: usable. Lists repo metadata, 6 canonical plans, 6 active markdown plans, checkpoint pointer, lessons, and health.
- `workflow status`: usable, but next action is stale checkpoint text: `Status: Completed (2026-04-11)`.
- `workflow health`: healthy; dirty files `0`, active plan `true`, canonical plans `6`, checkpoint `true`, proposals `0`.
- `workflow plan`: usable; canonical inventory currently contains `active-artifact-cleanup`, `crg-kg-integration`, `platform-dir-unification`, `refresh-skill-relink`, `resource-intent-centralization`, and `skill-import-streamline`.
- `workflow tasks crg-kg-integration`: usable; `phase-e-postgres` is `in_progress`, `phase-f-go-mcp` pending, `phase-g-skill-integration` pending.
- `workflow verify log`: usable empty-state; returns `No verification records found.`

Dogfood implications:
- Session start should use workflow commands first, then markdown plans for richer rationale and checklist detail.
- Canonical plan/task presence is now a normal condition for this repo, not an aspirational future state.
- Persist surfaces (`workflow checkpoint`, `workflow verify`) are still underused in the loop and should be exercised in a temp sandbox unless real writes are explicitly approved.

## Iteration Log

### Iteration 27 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 6 — refresh/import regression test: imported `.agents/skills/<name>/` directory → managed symlink via full `runRefresh`
- scenario_tags: [clean-repo, canonical-plan-present, imported-dir-to-managed-symlink]
- feedback_goal: Does `runRefresh` after import-from-refresh still leave `repo/.agents/skills/review` as a symlink to the canonical `~/.agents/skills/proj/review` skill dir (full pipeline ordering)?
- files_changed: 3
- lines_added: 134
- lines_removed: 10
- tests_added: 1
- tests_total_pass: true
- retries: 0
- commit: ae29a92
- scope_note: "on-target"
- summary: Added `TestRefreshReplacesImportedRepoSkillDirWithManagedSymlink`; marked Phase 6 refresh/import regression checkbox in markdown plan

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: no (integration test asserts successful symlink outcome; allowlist/refusal paths remain in `internal/platform` tests)
- tests_used_sandbox: n/a
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: partial (YAML `phase-6-verification` still pending until Phase 5 task deps reconciled; markdown Phase 6 refresh/import item done)
- persisted_via_workflow_commands: no
- ran_cli_command: yes (`workflow tasks resource-intent-centralization`)
- exercised_new_scenario: yes (`imported-dir-to-managed-symlink`)
- cli_produced_actionable_feedback: informative-nonblocking (canonical tasks unchanged; confirms machine layer still shows phase-6 pending)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 26 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 6 — import conflict coverage: origin-prefixed alternate hook paths + `review-notes/import-conflicts/` advisory YAML
- scenario_tags: [clean-repo, canonical-plan-present, import-conflict-preserve-both]
- feedback_goal: After adding `importOutput.Origin` and preserve-both conflict handling, do unit tests prove the alternate bundle path and review note are written without hitting interactive replace?
- files_changed: 4
- lines_added: 431
- lines_removed: 9
- tests_added: 7
- tests_total_pass: true
- retries: 0
- commit: (same commit as this change; see `git log -1`)
- scope_note: "on-target"
- summary: Hook canonical imports attach platform Origin; content mismatch writes `hooks/…/<origin-logical>/HOOK.yaml` (or `.json` fallback) plus RFC-shaped review note; dry-run skips writes; replace path unchanged when Origin empty; `import --help` CLI sanity

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (success preserve path, dry-run no write, unsupported dest rel implicit via first-free tests)
- tests_used_sandbox: n/a
- used_workflow_orient_status: yes (session start)
- aligned_with_canonical_tasks: partial (phase-6-verification still pending in YAML; markdown Phase 6 item 2 done)
- persisted_via_workflow_commands: no
- ran_cli_command: yes (`import --help`)
- exercised_new_scenario: yes (import-conflict-preserve-both)
- cli_produced_actionable_feedback: informative-nonblocking (help confirms import still wired; behavior proven in tests)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 25 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 6 — focused tests: `BuildSharedTargetPlan` aggregation (dedupe, conflict, platform error, dry-run error propagation) via `stubPlatform`
- scenario_tags: [clean-repo, canonical-plan-present, shared-target-plan-aggregation, refresh-install-dry-run]
- feedback_goal: After locking the aggregation path in tests, does `install --dry-run dot-agents` still show the same six merged `shared target:` skill rows as the established refresh dry-run semantics?
- files_changed: 3
- lines_added: 157
- lines_removed: 12
- tests_added: 4
- tests_total_pass: true
- retries: 0
- commit: 74bf836
- scope_note: "on-target"
- summary: Added `stubPlatform` and four tests covering `BuildSharedTargetPlan` dedupe/conflict/errors and `DryRunSharedTargetPlanLines` on plan failure; marked Phase 6 first checkbox in markdown plan; exercised `install --dry-run` for shared-row parity

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes
- tests_used_sandbox: no
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: partial (YAML still shows phase-5/6 pending; markdown Phase 6 item 1 done)
- persisted_via_workflow_commands: no
- ran_cli_command: yes
- exercised_new_scenario: yes (shared-target-plan-aggregation; refresh-install-dry-run parity re-check via install)
- cli_produced_actionable_feedback: yes (install dry-run shows six shared-target lines with duplicate merges)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 24 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 5 — `RunSharedTargetProjection` unifies shared-target dry-run + execute for `refresh`, `install`, and `add`
- scenario_tags: [clean-repo, canonical-plan-present, dry-run-shared-target-preview]
- feedback_goal: After wiring `RunSharedTargetProjection`, does `refresh --dry-run dot-agents` still show the same merged shared-target skill rows (duplicate merges)?
- files_changed: 7
- lines_added: 140
- lines_removed: 39
- tests_added: 2
- tests_total_pass: true
- retries: 0
- scope_note: "on-target"
- summary: Added `platform.RunSharedTargetProjection`; refactored refresh/install/add to use it; tests assert dry-run parity with `DryRunSharedTargetPlanLines` and nil lines on apply

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (parity + apply shape; plan conflict/delegate paths unchanged and covered elsewhere)
- tests_used_sandbox: no
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: partial (markdown Phase 5 refresh/install items done; YAML `phase-5-unify-commands` still pending full scope)
- persisted_via_workflow_commands: no
- ran_cli_command: yes
- exercised_new_scenario: no (reconfirms dry-run-shared-target-preview — regression signal for refactor)
- cli_produced_actionable_feedback: yes (six skill rows unchanged)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 23 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 5 — `remove` calls `RemoveSharedTargetPlan` before per-platform `RemoveLinks`; `ResourcePlan.RemoveSharedTargets` + tests
- scenario_tags: [clean-repo, canonical-plan-present, remove-shared-target-plan]
- feedback_goal: After `CollectAndExecuteSharedTargetPlan`, does `RemoveSharedTargetPlan` remove the same shared symlink / rendered targets deterministically (tests)?
- files_changed: 5
- lines_added: 178
- lines_removed: 7
- tests_added: 2
- tests_total_pass: true
- retries: 0
- scope_note: "on-target"
- summary: Added `RemoveSharedTargetPlan` / `RemoveSharedTargets` for merged shared intents; `remove` command runs it for installed platforms before adapter `RemoveLinks`; tests for skill symlink + Codex TOML removal

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (execute→remove success paths; RemoveIfSymlinkUnder no-op for non-managed links implicit in links pkg)
- tests_used_sandbox: no (unit tests only; live `remove` destructive — not run)
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: N/A (phase-5-unify-commands still pending)
- persisted_via_workflow_commands: no
- ran_cli_command: yes
- exercised_new_scenario: yes (remove-shared-target-plan — evidence via tests not live CLI)
- cli_produced_actionable_feedback: informative-nonblocking (orient confirms session; remove behavior proven in `go test`)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 22 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 5 — centralize shared projection plan build (`BuildSharedTargetPlan` for dry-run + execute)
- scenario_tags: [clean-repo, canonical-plan-present, dry-run-shared-target-preview]
- feedback_goal: After refactoring to `BuildSharedTargetPlan`, does `refresh --dry-run` still emit the same merged shared-target lines (duplicate merges, skill rows)?
- files_changed: 4
- lines_added: 103
- lines_removed: 32
- tests_added: 1
- tests_total_pass: true
- retries: 0
- scope_note: "on-target"
- summary: Added `BuildSharedTargetPlan` + `collectSharedTargetIntents`; `CollectAndExecuteSharedTargetPlan` and `DryRunSharedTargetPlanLines` share one plan build; `formatSharedTargetPlanForDryRun` extracted; tests for empty platforms and dry-run/build parity

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (empty-platform plan + empty dry-run parity; existing conflict/dedupe tests unchanged)
- tests_used_sandbox: n/a
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: N/A (phase-5-unify-commands still pending)
- persisted_via_workflow_commands: no
- ran_cli_command: yes
- exercised_new_scenario: no (reconfirms dry-run-shared-target-preview after refactor — meaningful as regression signal)
- cli_produced_actionable_feedback: yes (six skill rows + merge counts unchanged)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 21 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 5 — `status`/`explain` read shared-target registry (`DryRunSharedTargetPlanLines`); `InstalledEnabledPlatforms` helper; refresh uses shared selector
- scenario_tags: [clean-repo, canonical-plan-present, status-audit-shared-registry, planner-diagnostic-visible]
- feedback_goal: Does `status --audit` list the same merged `shared target:` lines as `refresh --dry-run` for each managed project?
- files_changed: 8
- lines_added: 158
- lines_removed: 25
- tests_added: 2
- tests_total_pass: true
- retries: 0
- commit: fb68a05
- scope_note: "on-target"
- summary: Added platform.InstalledEnabledPlatforms; status/doctor audit prints Shared target registry; explain links documents path; refresh hoists installed-enabled platform list

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (all-disabled empty slice + invariant test)
- tests_used_sandbox: n/a
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: N/A (phase-5-unify-commands still pending; phase-3-migrate-shared-targets in_progress)
- persisted_via_workflow_commands: no
- ran_cli_command: yes
- exercised_new_scenario: yes (status-audit-shared-registry)
- cli_produced_actionable_feedback: yes (audit lines match dry-run merge semantics)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 20 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 3 — centralize non-dir agent projections (Codex TOML render, OpenCode/Copilot AGENT.md file symlinks) in shared target plan
- scenario_tags: [clean-repo, canonical-plan-present, dry-run-shared-target-preview, agents-non-dir-outputs-centralized]
- feedback_goal: After centralization, does `refresh --dry-run` still emit merged skill shared-target lines (and remain healthy) for the default enabled platforms?
- files_changed: 8
- lines_added: 326
- lines_removed: 47
- tests_added: 3
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Extended resource executor for file symlinks and codex-agent-toml render; Codex/OpenCode/Copilot SharedTargetIntents + thinned CreateLinks; prune stale Codex tomls; integration tests call CollectAndExecuteSharedTargetPlan; negative test for non-allowlisted file replace.

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes
- tests_used_sandbox: n/a
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: yes (phase-3-migrate-shared-targets in_progress)
- persisted_via_workflow_commands: no (read-only CLI only)
- ran_cli_command: yes
- exercised_new_scenario: yes (agents-non-dir-outputs-centralized; dry-run shows skills-only — OpenCode not enabled in live refresh)
- cli_produced_actionable_feedback: yes (dry-run path unchanged for skills; no new agent rows in this repo without OpenCode in enabled set)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 19 — 2026-04-11
- wave: resource-intent-centralization
- item: Phase 3 — centralize project-scoped `agents/` dir mirrors (`.claude/agents/<name>`) in shared target plan; thin Claude/Cursor `createAgentsLinks` for repo paths
- scenario_tags: [clean-repo, agents-repo-symlink-centralized, canonical-plan-present, dry-run-vs-apply]
- feedback_goal: Does `refresh --dry-run` surface `.claude/agents` shared-target rows when canonical project agents exist, and do unit tests prove Claude+Cursor agent intents dedupe to a single symlink?
- files_changed: 5
- lines_added: 257
- lines_removed: 24
- tests_added: 3
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Added `BuildSharedAgentMirrorIntents`, allowlisted `.claude/agents/` for imported-dir replacement, merged agent intents into Claude `SharedTargetIntents` and Cursor `SharedTargetIntents`, no-op repo `createAgentsLinks`; tests for dedupe, replacement, Claude+Cursor execute.

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: yes (agents-repo-symlink-centralized; live dry-run had no agent rows — empty canonical agents for `dot-agents`)
- cli_produced_actionable_feedback: yes (confirms skills dry-run unchanged; agent rows absent = expected empty state here)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 18 — 2026-04-11 12:00
- wave: resource-intent-centralization
- item: Phase 3 — dry-run visibility for centralized shared-target plan (`refresh` / `install` aligned with `CollectAndExecuteSharedTargetPlan`)
- scenario_tags: [clean-repo, dry-run-vs-apply, planner-diagnostic-visible, canonical-plan-present]
- feedback_goal: Does `refresh --dry-run` print merged `shared target:` lines (duplicate merge counts) before per-platform dry-run rows, and does `explain links` still describe centralized shared targets?
- files_changed: 4
- lines_added: 109
- lines_removed: 10
- tests_added: 2
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Added `DryRunSharedTargetPlanLines` and wired `refresh`/`install` dry-run paths to print merged symlink plan; two unit tests (none + cross-platform dedupe).

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: yes (dry-run-shared-target-preview paired with prior apply-only behavior)
- cli_produced_actionable_feedback: yes (live `refresh --dry-run dot-agents` showed merged rows + `.claude` vs `.agents` targets)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 17 — 2026-04-11
- wave: skill-import-streamline
- item: Add regression tests for manifest round-trip through promote (`add-regression-tests`); align canonical task `install-generate-merge` with implemented merge
- scenario_tags: [clean-repo, promote-preserves-extra-manifest, canonical-plan-reconciled]
- feedback_goal: Does `TestPromoteSkillIn_PreservesManifestUnknownFields` fail if promote drops `ExtraFields` or multi-source `sources`, and does `go test ./...` stay green?
- files_changed: 5
- lines_added: 152
- lines_removed: 37
- tests_added: 1
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Added promote-path regression test preserving legacy `refresh` + custom extra fields and git+local sources; advanced canonical tasks `add-regression-tests` and `install-generate-merge` to completed; PLAN.yaml + markdown plan marked completed for skill-import-streamline.

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (new test asserts preservation; existing promote tests still cover errors and convergence)
- tests_used_sandbox: n/a
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: yes
- persisted_via_workflow_commands: yes (workflow advance updated TASKS.yaml in repo)
- ran_cli_command: yes
- exercised_new_scenario: yes (promote-preserves-extra-manifest)
- cli_produced_actionable_feedback: yes (workflow tasks shows add-regression-tests completed)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 16 — 2026-04-11
- wave: skill-import-streamline
- item: Fix shared `.agents/skills/*` convergence so repo-local source directories can become managed mirrors after promotion without conflicting platform relink behavior
- scenario_tags: [canonical-plan-stale, skills-promote-convergence, skills-list-readback]
- feedback_goal: After promote, does `skills list <project>` show the promoted skill, confirming the agentsHome scan works end-to-end with canonical as real dir + repo-local as managed symlink?
- files_changed: 5
- lines_added: 97
- lines_removed: 43
- tests_added: 1
- tests_total_pass: true
- retries: 2 (io/fs import order + SKILL.md check ordering before symlink validation)
- commit: b457809
- scope_note: "on-target"
- summary: Rewrote promoteSkillIn to copy-move: canonical is real dir, repo-local becomes managed symlink. Added mispointing-symlink error path test. Canonical tasks advanced for items 1-4.

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (convergence success + idempotency + 4 error paths including new symlink-mispoints)
- used_workflow_orient_status: yes
- aligned_with_canonical_tasks: yes (advanced items 1-4 to completed; workflow tasks confirms)
- persisted_via_workflow_commands: sandboxed (workflow advance used to update canonical task state)
- ran_cli_command: yes
- exercised_new_scenario: yes (skills-list-readback: `skills list dot-agents` returns 3 skills)
- cli_produced_actionable_feedback: yes (agentsHome scan returns correct results end-to-end)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 15 — 2026-04-11
- wave: skill-import-streamline
- item: Add a project-scope "skills import/promote" command path
- scenario_tags: [clean-repo, skills-promote-new-command]
- feedback_goal: Does `skills promote` create the managed symlink in agentsHome/skills/<project>/, update .agentsrc.json Skills, and return errors for non-existent skill / missing project name / real-dir conflict?
- files_changed: 2
- lines_added: 154
- lines_removed: 0
- tests_added: 5
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Added `skills promote` subcommand with promoteSkillIn; creates managed symlink + updates manifest + calls ExecuteSharedSkillMirrorPlan; 5 tests cover success, idempotency, 3 error paths

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (success + idempotency + skill-not-found + no-project-name + non-symlink-conflict)
- ran_cli_command: yes
- exercised_new_scenario: yes (skills-promote-new-command; skills --help shows new subcommand)
- cli_produced_actionable_feedback: yes (skills --help confirms promote wired correctly)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 14 — 2026-04-11
- wave: skill-import-streamline
- item: Make `install --generate` merge with existing `.agentsrc.json` (preserve sources, project, ExtraFields)
- scenario_tags: [clean-repo, install-generate-source-merge]
- feedback_goal: After merge, does generated manifest state preserve pre-existing git/manual sources and extra keys instead of replacing `sources` wholesale?
- files_changed: 5
- lines_added: 269
- lines_removed: 62
- tests_added: 6
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Added MergeGenerateAgentsRC + merge in runInstallGenerate when manifest exists; dry-run shows merged project and source count; six unit tests

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- tests_positive_and_negative: yes (nil-arg and dedupe edge cases)
- ran_cli_command: yes
- exercised_new_scenario: yes (install-generate-source-merge via tests; live install blocked)
- cli_produced_actionable_feedback: informative-nonblocking (workflow plan lists canonical plans; merge behavior proven in tests)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 13 — 2026-04-11 15:00
- wave: skill-import-streamline
- item: Add manifest round-trip preservation for unknown fields (legacy refresh block, custom keys)
- scenario_tags: [clean-repo, sweep-dry-run, manifest-roundtrip-fixed]
- feedback_goal: Does AgentsRC.Save() now preserve unknown JSON fields like the legacy refresh block through a load → save cycle?
- files_changed: 2
- lines_added: 178
- lines_removed: 0
- tests_added: 2
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Added custom MarshalJSON/UnmarshalJSON to AgentsRC for unknown-field preservation; 2 new tests (round-trip + no-duplication)

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: yes (workflow sweep --dry-run — first time; sweep-dry-run scenario covered)
- cli_produced_actionable_feedback: yes (sweep proposes 5 actions across 3 projects; confirms drift report was accurate)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 12 — 2026-04-11 14:30
- wave: resource-intent-centralization
- item: Phase 4 — thin codex, opencode, and copilot createSkillsLinks to return nil (shared targets now command-layer only)
- scenario_tags: [clean-repo, platform-adapter-thinned, kg-uninitialized]
- feedback_goal: After thinning all 4 platform adapters, do all tests still pass, and does kg health cleanly surface the uninitialized state?
- files_changed: 3
- lines_added: 3
- lines_removed: 3
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Thinned codex/opencode/copilot createSkillsLinks to no-ops; ExecuteSharedSkillMirrorPlan now has zero callers from adapters; Phase 4 complete

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: yes (kg health — kg-uninitialized scenario, first time exercised)
- cli_produced_actionable_feedback: yes (confirmed kg-uninitialized reports cleanly; workflow health stable after full adapter thinning)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 11 — 2026-04-11 13:30
- wave: resource-intent-centralization
- item: Phase 4 — thin claude.createSkillsLinks to user-home only; update stage1 tests to use command-layer pattern
- scenario_tags: [clean-repo, platform-adapter-thinned, multi-project-drift-empty-state]
- feedback_goal: After thinning claude adapter, do stage1 integration tests still pass using CollectAndExecuteSharedTargetPlan + CreateLinks pattern?
- files_changed: 2
- lines_added: 13
- lines_removed: 6
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Thinned claude.createSkillsLinks (now user-home only); updated TestClaudeCreateLinksDualSkillOutputs and TestClaudeCreateLinksReplacesImportedRepoSkillDirWithManagedSymlink to use command-layer pattern

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: yes (workflow drift — multi-project-drift-empty-state, first time exercised)
- cli_produced_actionable_feedback: yes (drift shows all 3 projects lack .agents/workflow/)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 10 — 2026-04-11 12:30
- wave: resource-intent-centralization
- item: Phase 3 — wire command layer (install.go, refresh.go, add.go) to call CollectAndExecuteSharedTargetPlan before the platform loop
- scenario_tags: [clean-repo, command-layer-shared-plan-wired]
- feedback_goal: Does workflow health stay healthy after command-layer wiring, confirming the new pre-platform shared plan doesn't break the build path?
- files_changed: 3
- lines_added: 33
- lines_removed: 2
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "on-target"
- summary: Wired CollectAndExecuteSharedTargetPlan into install, refresh, and add command flows before the per-platform CreateLinks loop

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: no (workflow health and workflow status are covered paths; new evidence is they stay clean after wiring — low-signal but necessary)
- cli_produced_actionable_feedback: yes (confirmed backward compatibility of command-layer addition)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 9 — 2026-04-11 11:30
- wave: resource-intent-centralization
- item: Phase 3 — add SharedTargetIntents to Platform interface and CollectAndExecuteSharedTargetPlan command-layer aggregation function
- scenario_tags: [clean-repo, planner-diagnostic-visible, interface-machinery-added]
- feedback_goal: Does explain links still surface planner correctly, and does workflow health stay clean after the interface expansion?
- files_changed: 8
- lines_added: 87
- lines_removed: 0
- tests_added: 1
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: "split: Phase 3 command-layer wiring deferred to next iteration; this iteration adds machinery only"
- summary: Added SharedTargetIntents to Platform interface (5 platform impls), added CollectAndExecuteSharedTargetPlan aggregation helper, added cross-platform dedupe test

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: no (explain links and workflow health are previously covered; new evidence is that they stay clean after interface expansion — low signal)
- cli_produced_actionable_feedback: yes (confirmed backward compatibility of interface addition)
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 8 — 2026-04-11 10:19
- wave: resource-intent-centralization
- item: Phase 2 — add a planner/executor layer that aggregates intents before any filesystem writes
- scenario_tags: [clean-repo, planner-diagnostic-visible]
- feedback_goal: Does `dot-agents explain` now surface centralized shared-skill planning clearly enough to make the new planner visible without a write command?
- files_changed: 9
- lines_added: 424
- lines_removed: 24
- tests_added: 4
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: on-target
- summary: Added a minimal `ResourcePlan` builder/executor for shared skill mirrors, routed repo-local shared skill projections through it for Claude/Codex/OpenCode/Copilot, and documented the new ownership model in `dot-agents explain`

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: yes
- cli_produced_actionable_feedback: yes
- linked_traces_to_outcomes: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 7 — 2026-04-11 09:54
- wave: resource-intent-centralization
- item: Phase 2 — Define an internal `ResourceIntent` shape for projection outputs
- scenario_tags: [clean-repo, repo-health-stack]
- feedback_goal: Confirm the new internal model did not regress baseline repo-health surfaces
- files_changed: 2
- lines_added: 345
- lines_removed: 0
- tests_added: 5
- tests_total_pass: true
- retries: 0
- commit: b457809
- scope_note: on-target
- summary: Added a declarative `ResourceIntent` / `ResourceSourceRef` model with typed ownership, shape, transport, replace/prune policies, validation, and focused tests in `internal/platform`

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- exercised_new_scenario: no
- cli_produced_actionable_feedback: no
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
- commit: b457809
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
- commit: b457809
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
- commit: b457809
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
- commit: b457809
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
- commit: b457809
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
- commit: b457809
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
- commit: b457809
- scope_note: n/a (seed from Codex sessions 019d7a6d and 019d7a9d)
- summary: GraphStore interface + SQLite backend, managed resource cleanup, AGENTS.md, skill transforms, AgentsRC schema, resource-intent-centralization plan

## Next Iteration Playbook

Preferred single item for iteration 28:
- Phase **6** next unchecked bullet — non-empty directory replacement is executor-only and allowlisted (`removeImportedDirIfAllowlisted` / refusal paths), or status/explain registry tests; **or** `crg-kg-integration` Phase E Postgres slice.

Loop closeout rules (iteration 21+):
- Keep the iteration atomic: code plus loop-state/plan updates in one final commit.
- Run one primary evidence chain plus at most one secondary probe.
- Reconcile coverage tables before ending the iteration.
- Use the product workflow surfaces on purpose: `workflow orient` + `workflow status` + `workflow plan`, then `workflow tasks <id>` when the selected wave has a canonical plan.

Candidate paths (priority order):
1. **resource-intent-centralization Phase 6 (remaining)**: refresh/import regression, non-empty dir replacement allowlist, status/explain registry tests.
2. **crg-kg-integration Phase E**: Postgres backend slice matching the plan's next focus.

Primary feedback goal for iteration 28 (example):
- Does a new test lock executor-only refusal for a non-allowlisted path, or does `status --audit` stay aligned with `DryRunSharedTargetPlanLines` after a registry tweak?

Command-feedback priorities:
- Session start: `workflow orient` -> `workflow status` -> `workflow plan`; add `workflow tasks resource-intent-centralization` when picking that wave.
- Primary evidence: targeted package tests plus `refresh --dry-run` / `workflow health` when touching shared-target paths.
- Archive completed `skill-import-streamline` markdown plan to history when convenient (lesson archive-completed-active-plans).

Known baseline CLI noise:
- `status` / `doctor` warn about 4 broken Claude skill links in user config.
- `doctor` warns that the `dot-agents` git source is not yet fetched.
- `workflow sweep --dry-run` shows 5 actions across 3 projects; these are valid drift findings, not noise.
- `workflow status` next action is stale checkpoint text today; treat it as a freshness bug / baseline warning, not as the source of truth for wave selection.

Note:
- `ExecuteSharedSkillMirrorPlan` is exercised from `skills promote`; skill-import-streamline regression tests now include promote + manifest `ExtraFields` preservation.

## Analysis Readiness

Questions the later analysis phase should be able to answer:
- Which commands are reliable only on happy paths vs across realistic repo states?
- Which workflow states produce the most retries, ambiguity, or operator intervention?
- Which outputs are correct but still create UX friction or weak operator guidance?
- Which agent loops stay tightly scoped vs drift or require cleanup work afterward?

Signals already captured:
- Per-iteration summary, scope, retries, commit, and basic self-assessment
- Exact CLI invocations with short output snapshots and structured metadata (scenario, expectation, follow-on, classification)
- A clearer distinction between high-signal feedback traces and low-signal baseline health rechecks
- Command-level coverage tracking (28 of ~41 commands now tested)
- Scenario coverage grouped by workflow, KG, CRG, bridge/config, delegation, integration, and outcome-quality families
- Workflow dogfooding baseline is now explicit: `workflow orient/status/health/plan/tasks/verify log` are all known-good read surfaces, with one known freshness issue in `workflow status`
- The `ResourceIntent` contract and the first `ResourcePlan` builder/executor slice are now codified in Go with validation, dedupe/conflict, and imported-dir convergence tests
- Write-command-path traces: kg warm, kg link CRUD, workflow checkpoint
- Empty-state traces: kg health/query/lint (no KG_HOME), workflow tasks/plan (no PLAN.yaml), kg flows (no igraph)
- Warning-state traces: kg link orphan, workflow status next-action parsing, kg flows misleading help text
- Before/after traces: workflow health warn→healthy after checkpoint write
- Small integration traces already exist, especially status/doctor/workflow-health and checkpoint→health improvement; these now anchor the first bootstrap and closeout stacks
- `dot-agents explain` now exposes the centralized shared-skill ownership model, giving a safe read-only diagnostic surface for planner-aware iterations
- `status --audit` shared registry (iteration 21) gives per-project merged-plan readback without `refresh --dry-run`, using the same builder as command-layer shared targets

Signals still missing or too weak:
- A live **apply** trace that proves the shared plan runs once at the command layer during a real `refresh`/`install` (guardrail limits direct `refresh` in loop; dry-run now proves merged plan visibility; iteration 25 adds `install --dry-run` parity vs refresh for shared rows)
- Evidence that the post-skills planner shape can absorb canonical `agents/` projections without another ownership-model fork — **mostly addressed in repo:** dir mirrors, Codex TOML, OpenCode/Copilot file symlinks centralized; **status/explain registry slice done (iter 21)**; **command-layer shared projection unified (iter 24: `RunSharedTargetProjection` + remove plan)**; **iter 27** adds automated `runRefresh` regression for imported skill dir → symlink (still not a manual live `refresh` apply trace under guardrails); remaining gap: live apply traces where allowed and Phase 6 executor-only + status/explain verification bullets
- Canonical workflow state transitions: `workflow advance`, `workflow verify`, sandboxed `workflow checkpoint`, and plan/task flows that update real `PLAN.yaml` + `TASKS.yaml` state (workflow log now covered; tasks readback works)
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
| `clean-repo` | yes | 12 | Reconfirmed after Phase 4 complete; all 4 adapters thinned, tests pass, workflow health stable |
| `dirty-repo` | yes | 3 | `workflow status` reported `dirty files: 1` |
| `legacy-plan-only` | no | - | no iteration has isolated next-action behavior driven only by `.agents/active/*.plan.md` unchecked items |
| `no-canonical-plan` | yes | 5 | historical empty-state trace from early loop setup; keep as contrast against the now-populated canonical plan inventory |
| `canonical-plan-present` | yes | 14 | `workflow plan` now lists 6 canonical plans; live workflow readback during dogfooding review confirmed the inventory is stable |
| `current-focus-task-present` | no | - | requires active canonical plan state with `current_focus_task` set |
| `blocked-plan-set` | yes | 5 | `workflow orient` rendered blocked and deferred active plans correctly |
| `blocked-task-visible` | no | - | requires canonical tasks with `blocked` status surfaced by `workflow tasks` or `plan show` |

### Workflow Write Paths

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `checkpoint-written` | yes | 5 | `workflow checkpoint` created a checkpoint and improved `workflow health` output |
| `workflow-log-visible` | yes | 6 | `workflow log` showed checkpoint from prior iteration; next_action UX issue confirmed |
| `workflow-advance-success` | yes | 17 | `workflow advance` moved `add-regression-tests` and `install-generate-merge` to completed in TASKS.yaml |
| `dry-run-shared-target-preview` | yes | 24 | Iteration 24: `refresh --dry-run dot-agents` after `RunSharedTargetProjection` — still six skill rows with duplicate merges; regression signal for unified entry |
| `shared-target-plan-aggregation` | yes | 25 | `stubPlatform` tests: `BuildSharedTargetPlan` dedupe + conflict + wrapped collection error; `DryRunSharedTargetPlanLines` propagates plan error |
| `remove-shared-target-plan` | yes | 23 | `RemoveSharedTargetPlan` after `CollectAndExecuteSharedTargetPlan` in tests removes `.agents/skills/*` symlink and `.codex/agents/*.toml`; live `remove` not traced (destructive) |
| `status-audit-shared-registry` | yes | 21 | `status --audit` prints Shared target registry via same `DryRunSharedTargetPlanLines` + `InstalledEnabledPlatforms` as refresh dry-run; cross-checked merge counts vs iteration 20 dry-run semantics |
| `agents-repo-symlink-centralized` | yes | 19 | `BuildSharedAgentMirrorIntents` + Claude/Cursor dedupe in tests; allowlist includes `.claude/agents/` for imported-dir replacement |
| `agents-non-dir-outputs-centralized` | yes | 20 | `BuildSharedCodexAgentTomlIntents` + `BuildSharedAgentFileSymlinkIntents`; executor `RenderSingle`/`DirectFile`; tests for execute + allowlist negative |
| `verification-log-recorded` | no | - | `workflow verify record/log` not exercised yet |
| `shared-pref-proposal-pending` | no | - | requires approval-gated write path outside repo |
| `review-approve-reject-loop` | no | - | depends on queued shared preference proposals |
| `skills-promote-new-command` | yes | 15 | `skills promote` wired and tested; creates managed symlink + updates .agentsrc.json + calls ExecuteSharedSkillMirrorPlan; CLI help confirms subcommand present |
| `promote-preserves-extra-manifest` | yes | 17 | `TestPromoteSkillIn_PreservesManifestUnknownFields`: promote path keeps `refresh`/`myteam` ExtraFields and multi-source `sources` after `Save()` |
| `import-conflict-preserve-both` | yes | 26 | `TestProcessImportOutput_preservesHookConflict` + dry-run test: alternate `hooks/…/HOOK.yaml`, `review-notes/import-conflicts/ic-*.yaml`; `Origin` empty still uses replace path |
| `imported-dir-to-managed-symlink` | yes | 27 | Full `runRefresh` test: import-from-refresh updates canonical SKILL.md then shared projection replaces imported `.agents/skills/review/` dir with symlink to `AGENTS_HOME/skills/proj/review` |

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
| `multi-project-drift-detected` | yes | 11 | `workflow drift` showed all 3 projects missing .agents/workflow/ — valid no-workflow-initialized state; report saved to drift-report.json |
| `sweep-dry-run` | yes | 13 | `workflow sweep --dry-run` proposed 5 actions across 3 projects (create .agents/workflow/ + checkpoint reminders); matches drift report from iteration 11 |
| `refresh-install-dry-run` | yes | 25 | Iter 18: `refresh --dry-run` centralized shared-target rows; iter 25: `install --dry-run dot-agents` — six skill shared-target lines + duplicate merges aligned with refresh |
| `sweep-apply-confirmed` | no | - | apply path is both untested and more invasive |

### KG Lifecycle

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `no-kg-home-configured` | yes | 12 | `kg health` fails with actionable "run kg setup" guidance; confirmed again post-Phase 4 (kg-uninitialized state) |
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
| `install-generate-source-merge` | yes | 14 | MergeGenerateAgentsRC unit tests: git+local preserved, dedupe, ExtraFields; live `install --generate` not run (loop guardrail) |
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
| `planner-diagnostic-visible` | yes | 21 | `explain links` now references `status --audit` for live merged-plan lines; iteration 8 baseline was explain-only |
| `retry-recovered` | yes | 2 | naming collision fixed before final commit |
| `retry-recovered-with-error-log` | yes | 2 | Iteration 2 naming-collision retry is now mirrored in `## Error Log` for later analysis |
| `pre-existing-tool-bug-confirmed` | yes | 5 | `kg flows` help text is wrong after postprocess when igraph is absent |
| `blocked-environment` | no | - | no scenario explicitly tagged as blocked by missing dependency or approval gate |

## Skip List

Plans to skip (blocked, requires architectural work, completed, or out of scope for loop):
- `refresh-skill-relink` — paused; convergence items still tied to shared executor
- `platform-dir-unification` — paused; bash parity deferred
- `crg-kg-integration` Phase G — deferred until Phases E/F land and are exercised

(Completed plans archived to .agents/history/ on 2026-04-11:)
- kg-phase-1 through kg-phase-6, wave-3 through wave-7, workflow-automation-product-spec-review, agentsrc-local-schema, resource-sync-architecture-analysis

## Blockers

- Phase **5** — shared-target **command entry** is unified (`RunSharedTargetProjection` for refresh/install/add); **`remove`** uses `RemoveSharedTargetPlan`; Phase **6** — aggregation tests, import conflict preserve-both, **refresh/import `runRefresh` regression** (iter 27); remaining Phase **6** bullets: executor-only replacement coverage, status/explain registry tests; canonical YAML `phase-5-unify-commands` may advance when Phase 3/5 scope is reconciled with TASKS.
- `plan-wave-picker` SKILL.md at `~/.agents/skills/dot-agents/plan-wave-picker/SKILL.md` has invalid frontmatter (missing `---` delimiters). Codex warns on load.

## CLI Traces

### Iteration 27 — 2026-04-11

Trace: workflow-tasks-resource-intent-after-refresh-regression-test
Command: `go run ./cmd/dot-agents workflow tasks resource-intent-centralization`
Scenario: [clean-repo, canonical-plan-present, imported-dir-to-managed-symlink]
Feedback goal: After closing the Phase 6 refresh/import markdown item, does canonical YAML still show phase-6-verification as pending (expected until TASKS advance)?
Output summary: phase-3 in_progress; phase-4/5/6 pending; unchanged dependency chain.
Expectation: expected
Follow-on: none — reconcile YAML when Phase 5/6 scope is batch-advanced
Classification: [ok]

### Iteration 26 — 2026-04-11

Trace: import-help-after-conflict-import-change
Command: `go run ./cmd/dot-agents import --help`
Scenario: [clean-repo, canonical-plan-present, import-conflict-preserve-both]
Feedback goal: Does the `import` subcommand still register after hook conflict handling changes (help path intact)?
Output summary: Usage line, `--scope`, global flags including `--dry-run` / `--yes` (replace prompts still relevant when Origin empty).
Expectation: expected
Follow-on: none
Classification: [ok]

### Iteration 25 — 2026-04-11

Trace: install-dry-run-shared-target-parity
Command: `go run ./cmd/dot-agents install --dry-run dot-agents`
Scenario: [clean-repo, canonical-plan-present, refresh-install-dry-run, shared-target-plan-aggregation]
Feedback goal: Does `install --dry-run` still emit six merged `shared target:` skill lines (duplicate counts) consistent with refresh dry-run for this repo?
Output summary: Six shared-target lines — three `.agents/skills/*` with `(2 duplicate intent(s) merged)`, three `.claude/skills/*`; then per-platform dry-run rows. Preceding resolver section shows global skill links dry-run (expected noise: three repo skills missing from sources).
Expectation: expected
Follow-on: none
Classification: [ok]

### Iteration 24 — 2026-04-11

Trace: refresh-dry-run-after-run-shared-target-projection
Command: `go run ./cmd/dot-agents refresh --dry-run dot-agents`
Scenario: [clean-repo, canonical-plan-present, dry-run-shared-target-preview]
Feedback goal: After `RunSharedTargetProjection` refactor, does `refresh --dry-run` still emit six merged `shared target:` skill lines with duplicate-intent counts?
Output summary: Six lines — three `.agents/skills/*` with `(2 duplicate intent(s) merged) (dry run)`; three `.claude/skills/*`; then per-platform dry-run refresh rows and `.agents-refresh` dry-run.
Expectation: expected
Follow-on: none
Classification: [ok]

Trace: workflow-status-readback
Command: `go run ./cmd/dot-agents workflow status` (iteration closeout)
Scenario: [clean-repo, canonical-plan-present]
Feedback goal: Confirm checkpoint-backed readback still shows stale next-action baseline (unchanged).
Output summary: Next action still `Status: Completed (2026-04-11)`; canonical plans 6; dirty files 7 while iteration artifacts staged (expected before commit).
Expectation: expected (stale checkpoint documented baseline)
Follow-on: none
Classification: [ok-warning] (stale next action — known baseline)

### Iteration 23 — 2026-04-11

Trace: workflow-orient-pre-commit
Command: `go run ./cmd/dot-agents workflow orient` (head)
Scenario: [clean-repo, canonical-plan-present]
Feedback goal: Session/orient still coherent before committing remove-path change; any unexpected dirty-file churn?
Output summary: Project dot-agents, branch feature/PA-cursor-projectsync-phase1-extract-293f, sha 8e8bb85; dirty files: 5 during WIP (expected before commit).
Expectation: informative-nonblocking
Follow-on: none
Classification: [ok]

### Iteration 22 — 2026-04-11

Trace: refresh-dry-run-after-build-shared-target-plan
Command: `go run ./cmd/dot-agents refresh --dry-run dot-agents`
Scenario: [clean-repo, canonical-plan-present, dry-run-shared-target-preview]
Feedback goal: After `BuildSharedTargetPlan` centralization, does `refresh --dry-run` still show merged shared-target skill lines with duplicate-intent counts?
Output summary: Six `shared target:` symlink lines (three `.agents/skills/*` with `(2 duplicate intent(s) merged)`; three `.claude/skills/*`); then per-platform dry-run refresh lines and `.agents-refresh` dry-run.
Expectation: expected
Follow-on: none
Classification: [ok]

### Iteration 21 — 2026-04-11

Trace: status-audit-shared-registry-vs-dry-run-semantics
Command: `go run ./cmd/dot-agents status --audit` (excerpt: Shared target registry sections per project)
Scenario: [clean-repo, canonical-plan-present, status-audit-shared-registry]
Feedback goal: Does `status --audit` show merged `shared target:` lines aligned with `refresh --dry-run` (same duplicate-merge counts and shapes)?
Output summary: Per project, "Shared target registry (same merge rules as refresh --dry-run)" lists symlink/write/symlink-file lines — e.g. dot-agents shows six skill rows with `(2 duplicate intent(s) merged)` on `.agents/skills/*`; payout shows agent symlink + codex toml + copilot file symlink rows.
Expectation: expected
Follow-on: none
Classification: [ok]

Trace: explain-links-registry-doc
Command: `go run ./cmd/dot-agents explain links` (tail — Registry diagnostics)
Scenario: [planner-diagnostic-visible, status-audit-shared-registry]
Feedback goal: Does `explain links` point operators at `status --audit` for live merged-plan readback?
Output summary: New "Registry diagnostics" lines reference `dot-agents status --audit` and parity with `refresh --dry-run`.
Expectation: expected
Follow-on: none
Classification: [ok]

### Iteration 20 — 2026-04-11

Trace: refresh-dry-run-post-non-dir-agent-centralization
Command: `go run ./cmd/dot-agents refresh --dry-run dot-agents`
Scenario: [clean-repo, dry-run-shared-target-preview, agents-non-dir-outputs-centralized, canonical-plan-present]
Feedback goal: Does `refresh --dry-run` still show merged shared-target skill rows after centralizing Codex/OpenCode/Copilot agent outputs?
Output summary: Six `shared target:` lines for skills (three `.agents/skills/*` with duplicate merges; three `.claude/skills/*`); per-platform dry-run refresh lines; no OpenCode in enabled platforms so no `.opencode`/`write` lines in this live run.
Expectation: expected — skills-only live trace matches repo/fixture state; non-dir agent intents covered in unit tests
Follow-on: none
Classification: [ok]

### Iteration 19 — 2026-04-11

Trace: workflow-orient-dirty-wip
Command: `go run ./cmd/dot-agents workflow orient` (truncated)
Scenario: [clean-repo, canonical-plan-present]
Feedback goal: Confirm canonical plan readback before dry-run (repo had uncommitted loop-state + platform edits during trace).
Output summary: 6 canonical plans; resource-intent-centralization active; branch feature/PA-cursor-projectsync-phase1-extract-293f; dirty files: 4 (WIP).
Expectation: informative-nonblocking
Follow-on: none — dirty count expected mid-iteration
Classification: [ok]

Trace: refresh-dry-run-post-agent-centralization
Command: `go run ./cmd/dot-agents refresh --dry-run dot-agents`
Scenario: [clean-repo, dry-run-vs-apply, agents-repo-symlink-centralized, dry-run-shared-target-preview]
Feedback goal: Do shared-target lines include `.claude/agents/<name>` when canonical agents exist, and are skill merge counts unchanged?
Output summary: Six shared-target lines for skills only (`.agents/skills/*` with 2 duplicate merges; `.claude/skills/*` without merge count). No `.claude/agents` lines — no `~/.agents/agents/dot-agents/*/AGENT.md` in this environment.
Expectation: expected — agent mirror rows are conditional on canonical agent dirs
Follow-on: none — empty-state for agents is valid evidence paired with unit tests for populated state
Classification: [ok-empty]

Trace: workflow-health-after-dry-run-iter19
Command: `go run ./cmd/dot-agents workflow health`
Scenario: [clean-repo, repo-health-stack]
Feedback goal: Workflow subsystem still healthy after dry-run.
Output summary: status healthy; dirty files 4 (WIP); canonical plans 6; checkpoint true.
Expectation: expected
Follow-on: none
Classification: [ok]

### Iteration 18 — 2026-04-11

Trace: explain-links-planner-baseline
Command: `go run ./cmd/dot-agents explain links` (truncated)
Scenario: [clean-repo, planner-diagnostic-visible]
Feedback goal: Confirm centralized shared-target wording unchanged after dry-run wiring.
Output summary: CENTRALIZED SHARED TARGETS section still present; describes planning before writes.
Expectation: expected
Follow-on: none
Classification: [ok]

Trace: refresh-dry-run-shared-target-chain (mutation-and-reconciliation: dry-run vs apply)
Chain: `explain links` → `refresh --dry-run dot-agents` → `workflow health`
Command: `go run ./cmd/dot-agents refresh --dry-run dot-agents`
Scenario: [clean-repo, dry-run-vs-apply, dry-run-shared-target-preview, canonical-plan-present]
Feedback goal: Does dry-run list merged shared symlink rows with duplicate-intent counts before per-platform dry-run lines?
Output summary: Six `shared target: symlink … -> …` lines for three skills — `.agents/skills/*` rows show `(2 duplicate intent(s) merged)`; `.claude/skills/*` rows have no merge count (distinct conflict keys). Then Cursor/Claude/Codex/Copilot dry-run refresh lines and `.agents-refresh` dry-run.
Expectation: expected
Follow-on: none — paired apply path already proven in prior iterations via non-dry refresh/install tests
Classification: [ok]

Trace: workflow-health-after-dry-run
Command: `go run ./cmd/dot-agents workflow health`
Scenario: [clean-repo, repo-health-stack]
Feedback goal: Confirm read-only workflow subsystem still healthy after dry-run (no mutations).
Output summary: status healthy, dirty files 0, canonical plans 6, checkpoint true.
Expectation: expected
Follow-on: none
Classification: [ok]

### Iteration 17 — 2026-04-11

Trace: promote-regression-tests-and-canonical-closeout
Chain: `go test ./commands/ -run TestPromoteSkillIn_PreservesManifestUnknownFields -count=1` → `workflow advance` (×2) → `workflow tasks skill-import-streamline`
Commands: `go test` [ok]; `workflow advance skill-import-streamline --task add-regression-tests --status completed` [ok]; `workflow advance skill-import-streamline --task install-generate-merge --status completed` [ok]; `workflow tasks skill-import-streamline` [ok]
Scenario: [clean-repo, promote-preserves-extra-manifest, workflow-advance-success]
Feedback goal: Does the new test fail if promote drops ExtraFields, and do canonical tasks show both regression and merge tasks completed?
Output summary: Unit test passes; advance confirms both tasks completed; `workflow tasks` lists all skill-import-streamline tasks completed except none — full slate green (install-generate-merge now completed).
Expectation: expected
Follow-on: archive `skill-import-streamline` active plan to history when convenient
Classification: [ok]

### Iteration 15 — 2026-04-11

Trace: skills-promote-help-verify
Command: `go run ./cmd/dot-agents skills --help`
Scenario: [clean-repo, skills-promote-new-command]
Feedback goal: Does `skills promote` appear as a subcommand in the CLI, confirming correct wiring?
Output summary: `Available Commands: list, new, promote` — promote is wired with short description "Promote a repo-local skill to shared storage".
Expectation: expected
Follow-on: none — next iteration exercise `skills list <project>` after a sandbox promote to confirm agentsHome scan works end-to-end.
Classification: [ok]

### Iteration 14 — 2026-04-11

Trace: workflow-plan-canonical-list (read-only; install-generate blocked)
Chain: `workflow plan` (primary evidence after tests — confirms canonical plan registry still healthy post-merge code)
```
$ go run ./cmd/dot-agents workflow plan
```
(Output: lists active-artifact-cleanup completed, crg-kg-integration active, platform-dir-unification paused, refresh-skill-relink paused, resource-intent-centralization active, skill-import-streamline paused.)
Scenario: [clean-repo, install-generate-source-merge]
Feedback goal: Does merge logic for `install --generate` preserve manual/git sources? — answered by six `internal/config` tests; CLI trace is secondary sanity on workflow subsystem.
Expectation: informative-nonblocking — merge behavior not observable via this command; tests carry proof.
Follow-on: none — next iteration may add `explain` line about merge if operators need discoverability.
Classification: [ok] for `workflow plan`

### Iteration 13 — 2026-04-11

Trace: manifest-roundtrip-sweep-dryrun (mutation-and-reconciliation + cross-project)
Chain: `workflow sweep --dry-run` → `workflow health`
```
$ go run ./cmd/dot-agents workflow sweep --dry-run
Sweep Plan [dry-run]
  ○ 1. [ResumeAgent] Create .agents/workflow/ directory in ResumeAgent
  ○ 2. [ResumeAgent] Add checkpoint reminder annotation for ResumeAgent
  ○ 3. [dot-agents] Create .agents/workflow/ directory in dot-agents
  ○ 4. [payout] Create .agents/workflow/ directory in payout
  ○ 5. [payout] Add checkpoint reminder annotation for payout
  Run with --apply to execute these actions.
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: healthy, branch: feature/...293f, dirty: 0, has active plan: true, canonical plans: 0, has checkpoint: true
```
Scenario: [clean-repo, sweep-dry-run, manifest-roundtrip-fixed]
Feedback goal: Does AgentsRC.Save() preserve unknown JSON fields? (confirmed by 2 new tests) + Does workflow sweep dry-run accurately report drift from iteration 11's report?
Expectation: expected — sweep proposes 5 actions matching drift report (3 no-workflow, 2 no-checkpoint). workflow health stable.
Follow-on: documented — sweep-dry-run scenario now covered; --apply remains gated.
Classification: [ok]

### Iteration 12 — 2026-04-11

Trace: all-adapters-thinned-kg-uninitialized (analysis/readback + kg-lifecycle)
Chain: `kg health` → `workflow health`
```
$ go run ./cmd/dot-agents kg health
✗ Error: knowledge graph not initialized at /Users/nikashp/knowledge-graph — run 'dot-agents kg setup' first
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: healthy, branch: feature/PA-cursor-projectsync-phase1-extract-293f, dirty files: 0, has active plan: true, canonical plans: 0, has checkpoint: true
```
Scenario: [clean-repo, kg-uninitialized, platform-adapter-thinned]
Feedback goal: After thinning all 4 adapters, does kg health cleanly surface uninitialized state and does workflow health remain stable?
Expectation: expected — kg health error is actionable and clean; workflow health is unaffected by adapter changes
Follow-on: documented — kg-uninitialized now covered; workflow health confirmed stable post-Phase 4
Classification: [ok-empty] for kg health (expected uninitialized state), [ok] for workflow health

### Iteration 11 — 2026-04-11

Trace: adapter-thin-drift-check (mutation-and-reconciliation)
Chain: `workflow drift` → `explain links`
```
$ go run ./cmd/dot-agents workflow drift
Workflow Drift Report — 3 projects checked. ResumeAgent: warn (no checkpoint, no .agents/workflow/). dot-agents: warn (no .agents/workflow/). payout: warn (no checkpoint, no .agents/workflow/). healthy: 0, warnings: 3, unreachable: 0. report saved to ~/.agents/context/drift-report.json
$ go run ./cmd/dot-agents explain links
[CENTRALIZED SHARED TARGETS section still present and accurate after adapter thinning]
```
Scenario: [clean-repo, platform-adapter-thinned, multi-project-drift-empty-state]
Feedback goal: After thinning claude adapter, does workflow drift expose the project state accurately and does explain links remain coherent?
Expectation: expected — drift correctly surfaces no-workflow projects; explain links is unaffected by adapter change
Follow-on: documented — drift-report.json written outside repo (guardrail-safe, it's in ~/.agents/context/); next iteration: thin codex/opencode/copilot adapters
Classification: [ok-warning] — drift warnings are valid (no workflow initialized in these projects), not implementation bugs

### Iteration 10 — 2026-04-11

Trace: command-layer-wiring-health-check (bootstrap stack)
Chain: `workflow health` → `workflow status`
```
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: healthy, branch: feature/PA-cursor-projectsync-phase1-extract-293f, dirty files: 0, has active plan: true, canonical plans: 0, has checkpoint: true
$ go run ./cmd/dot-agents workflow status
sha: a5ec829 (current commit), dirty files: 0, active plans: 6, has checkpoint: true, next-action UX: still shows "Status: Completed" — known issue
```
Scenario: [clean-repo, command-layer-shared-plan-wired]
Feedback goal: Does workflow health stay healthy after command-layer wiring?
Expectation: expected — command-layer addition is backward-compatible; shared plan runs at command startup before platforms
Follow-on: documented — next iteration: thin platform adapters starting with claude.go
Classification: [ok]

### Iteration 9 — 2026-04-11

Trace: planner-diagnostic-stable-after-interface-expansion (analysis/readback)
Chain: `explain links` → `workflow health`
```
$ go run ./cmd/dot-agents explain links
[CENTRALIZED SHARED TARGETS section present — .agents/skills/<name> planned centrally before writes]
$ go run ./cmd/dot-agents workflow health
Workflow Health — status: healthy, branch: feature/PA-cursor-projectsync-phase1-extract-293f, dirty files: 0, has active plan: true, canonical plans: 0, has checkpoint: true
```
Scenario: [clean-repo, planner-diagnostic-visible]
Feedback goal: Does explain links still surface planner correctly, and does workflow health stay clean after the interface expansion?
Expectation: expected — interface addition is backward-compatible; explain and health surfaces are unaffected
Follow-on: documented — next iteration: wire command layer; feedback goal becomes "was .agents/skills written once not 4x?"
Classification: [ok]

### Iteration 8 — 2026-04-11

Trace: planner-diagnostic-visible (analysis/readback stack)
Chain: `explain links` → `explain platforms`
```
$ go run ./cmd/dot-agents explain links
Link Types — added a "CENTRALIZED SHARED TARGETS" section explaining that `.agents/skills/<name>` is planned centrally before writes so compatible platforms converge on one managed mirror instead of racing to replace the same directory.
$ go run ./cmd/dot-agents explain platforms
Supported Platforms — Claude now lists `.claude/skills/` plus shared `.agents/skills/`; Codex, OpenCode, and GitHub Copilot each list shared `.agents/skills/` explicitly.
```
Scenario: [clean-repo, planner-diagnostic-visible]
Feedback goal: Does `dot-agents explain` now surface centralized shared-skill planning clearly enough to make the new planner visible without a write command?
Expectation: expected — this iteration added the first safe read-only diagnostic for the planner/executor slice because `refresh` / `install` remain off-limits under the loop guardrails
Follow-on: documented — the next iteration should move from planner visibility to command-level shared-target aggregation/integrity evidence
Classification: [ok]

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
Feedback goal: Confirm the new internal model did not regress baseline repo-health surfaces
Expectation: expected — the new internal model is not wired into command behavior yet, so repo health should remain unchanged
Follow-on: documented — low-signal baseline recheck only; next iteration should prefer a planner/executor-facing command path
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

This table tracks the last **loop-traced** invocation per command. For current live readback capability outside a numbered iteration, also see `## Workflow Command Baseline`.

| Command | Tested | Last Iteration | Status |
|---|---|---|---|
| `install --generate` | tests only | 14 | ok (merge behavior unit-tested; live cmd guardrail-blocked in loop) |
| `install --dry-run` | yes | 25 | ok — six shared-target skill rows + per-platform dry-run; resolver shows missing global skills for three repo skills |
| `status` | yes | 21 | ok (incl. `--audit` shared registry trace) |
| `doctor` | yes | 7 | ok-warning |
| `explain` | yes | 21 | ok (links topic: registry diagnostics cross-ref) |
| `refresh` | yes | 24 | ok (dry-run merged skills; post–RunSharedTargetProjection wiring) |
| `remove` | tests only | 23 | ok (RemoveSharedTargetPlan covered in `resource_plan_test`; live cmd guardrail) |
| `workflow status` | yes | 24 | ok-warning (stale next-action baseline) |
| `workflow orient` | yes | 5 | ok |
| `workflow plan` | yes | 14 | ok |
| `workflow checkpoint` | yes | 5 | ok |
| `workflow log` | yes | 6 | ok |
| `workflow tasks` | yes | 27 | ok |
| `workflow advance` | yes | 17 | ok |
| `workflow health` | yes | 13 | ok |
| `workflow verify` | no | - | - |
| `workflow prefs` | no | - | - |
| `workflow graph` | no | - | - |
| `workflow fanout` | no | - | - |
| `workflow merge-back` | no | - | - |
| `workflow drift` | yes | 11 | ok-warning |
| `workflow sweep` | yes | 13 | ok |
| `kg setup` | no | - | - |
| `kg health` | yes | 12 | ok-empty |
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
| `skills list` | no | - | - |
| `skills new` | no | - | - |
| `skills promote` | yes | 15 | ok (--help verified; subcommand wired) |
| `import --help` | yes | 26 | ok — post import-conflict wiring |

## Error Log

### Iteration 2
- type: compile-error
- detail: Naming collision between the new CRG bridge result type and existing `ImpactResult` in `store.go` during Phase C command wiring.
- resolution: Renamed the conflicting result type before the final green test/CLI pass.
- retries: 1

<!--
Format:
### Iteration N
- type: test-failure | compile-error | cli-error
- detail: <what failed>
- resolution: <what fixed it>
- retries: N
-->

## CLI Observations

- `explain links` is now a useful safe trace for planner-oriented iterations: it surfaces centralized shared-target ownership without needing `refresh` or `install`.
- `explain platforms` now makes the shared `.agents/skills/` mirror explicit across Claude, Codex, OpenCode, and Copilot, which is a better operator-facing diagnostic than the older per-platform wording.
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
