# Loop State

Last updated: 2026-04-14
Iteration: 37 (orchestrator pass)

## Current Position

Orchestrator pass — 2026-04-15, iteration 38:
- **Plan:** `loop-runtime-refactor`
- **Active slices completed this pass:** 5a (loop-state migration), 6a (loop-worker AGENT.md)
- **No active delegation bundles** — both contracts closed out this session.
- **Planned A/B comparison:** run Pattern E vs script worker in paired sibling worktrees from the same base commit, not in this checkout. If worktrees matching `../dot-agents-pattern-e-*` and `../dot-agents-script-*` exist, treat them as reserved comparison sandboxes and avoid reconciling their results here until metrics are captured.

Slices unblocked for next pass:
- `phase-5b` — update active.loop.md worker instructions (depends: 5a ✓)
- `phase-5c` — implement `workflow checkpoint --log-to-iter` flag (depends: 5a ✓)
- `phase-6b` — add loop-worker to .agentsrc.json + strip inlined orchestrator prompt (depends: 6a ✓)

Background (unchanged baselines):
- **Pre-existing Go build break:** `internal/graphstore/postgres.go` imports `pgx/v5` not in `go.mod` — blocks `go test ./...`; use `go run ./cmd/dot-agents` for CLI work and `cd ports/typescript && npm test` for TS verification.
- **Dirty workspace:** loop-state iteration-log/ (new), AGENT.md (new), fanout bug fix (workflow.go + workflow_test.go), plan/SLICES/TASKS updates, research article — commit pending.

## Loop Health

Orchestrator (2026-04-14, iteration 37):
- **`workflow next` vs canonical tasks (YAML wins):** `workflow next` selects `phase-5-stage2-and-plugin-alignment` (first pending task whose `depends_on` is satisfied — phase-5 only depends on phase-3). Canonical `TASKS.yaml` has `phase-4-advanced-surface-decision` **in_progress** with an **active** delegation bundle; orchestration priority is finishing phase-4, not starting phase-5 in parallel.
- **Canonical vs checkpoint:** aligned on plan and substance — checkpoint `next_action` and plan focus remain Phase 4 boundary work; selector order differs from `workflow next` ordering for the reason above.
- **`workflow orient` active_plans empty:** expected when no markdown plans live under `.agents/active/`; canonical `PLAN.yaml` + `workflow tasks` remain source of truth.
- **Active delegations:** `active_count: 1` — phase-4 bundle path `.agents/active/delegation-bundles/del-phase-4-advanced-surface-decision-1776192016.yaml` (canonical id; superseded ids include `...1776191464`, `...0659`, `...8348`).

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
- **Worker-mode comparison guardrail:** future Pattern E vs script A/B runs should use separate worktrees so `.agents/active/delegation*`, merge-backs, and branch state do not collide across modes.

Operating rules for iteration 18+:
- Prefer one final commit per iteration that includes code plus loop-state/plan updates.
- Use one primary evidence chain plus at most one secondary probe; reconcile coverage tables before closeout.
- Rewrite summary sections in place; do not append duplicate playbook blocks.
- Start each iteration with `workflow orient`, `workflow status`, and `workflow plan`; if the chosen wave has a canonical plan, run `workflow tasks <id>` before selecting the exact checklist item.
- Treat checkpoint-backed `workflow status` as runtime readback and canonical `workflow tasks` as machine-readable plan truth; if they disagree, log it rather than guessing.
- Prefer sandboxed `workflow checkpoint` / `workflow verify record` for closeout dogfooding when real `~/.agents` writes are not explicitly approved.
- After completing a canonical plan's tasks, run `workflow advance` for each task and align PLAN.yaml status with markdown plan headers.

## Next Iteration Playbook

- **Phase 3 closeout:** already completed at `dca9054` (merge-back archived; checkpoint verification pass).
- **AB-test B (`ts-ab-kg-commands`):** implementation + tests landed; merge-back artifact pending parent `workflow advance` + `workflow delegation closeout` for task `ts-ab-kg-commands` after review.
- **Parallel lane:** `ts-ab-workflow-commands` (worker A) may still be in flight — reconcile TASKS + delegations before the next fanout.
- **Evidence:** `go run ./cmd/dot-agents workflow tasks typescript-port`; TS verification `cd ports/typescript && npm test`. Full repo `go test ./...` currently green in this workspace.
- **Orchestrator:** resume phase-5 / phase-6 gating per canonical `TASKS.yaml` after AB-test merge-backs and any open phase-4 delegation closeouts.
- **A/B comparison next time:** use paired sibling worktrees for `loop-runtime-refactor/phase-5d-iter-log-schema`; capture both `metrics.json` files, then append a new run block to `.agents/history/loop-runtime-refactor/worker-mode-comparison.md`.
