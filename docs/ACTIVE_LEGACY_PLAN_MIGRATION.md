# Active Legacy Plan Migration Map

Status: inventory with partial canonical migration

## Scope

This inventory covers repo-local legacy `*.plan.md` artifacts still living under `.agents/active/`.

Use these rules when reconciling them:

- Promote actionable repo work to `.agents/workflow/plans/<plan-id>/`.
- Promote durable analysis, design, and decision docs to `.agents/workflow/specs/<topic>/`.
- Keep `.agents/active/` for transient coordination state and unreconciled legacy remnants only.

This pass intentionally excludes Ralph runtime scripts and the live loop-agent `PLAN.yaml` / `TASKS.yaml`.

## Migration Map

| Legacy artifact | Recommendation | Canonical target | Notes |
|---|---|---|---|
| `.agents/active/planning-evidence-backed-write-scope.plan.md` | Already represented canonically; archive or replace with a pointer when convenient | `.agents/workflow/specs/planner-evidence-backed-write-scope/design.md` | The active file is a lineage note for the spec, not the canonical home for new work. |
| `.agents/active/loop-agent-pipeline-resurrection.plan.md` | Already represented canonically; archive or replace with a pointer when convenient | `.agents/workflow/plans/loop-agent-pipeline/` | The active file has been superseded by the canonical plan bundle. |
| `.agents/active/graph-bridge-command-readiness-resurrection.plan.md` | Promoted to a workflow plan | `.agents/workflow/plans/graph-bridge-command-readiness/` | Canonical plan bundle created on 2026-04-19. The active Markdown artifact is now lineage, not the canonical execution home. |
| `.agents/active/kg-command-surface-readiness-analysis.plan.md` | Promoted to a workflow spec | `.agents/workflow/specs/kg-command-surface-readiness/` | Canonical design doc created on 2026-04-19. Follow-on executable work should hang off workflow plans later. |
| `.agents/active/go-native-code-graph-analysis.plan.md` | Promoted to a workflow spec | `.agents/workflow/specs/go-native-code-graph-analysis/` | Canonical design doc created on 2026-04-19 for the longer-range architectural track. |
| `.agents/active/ci-smoke-suite-hardening.plan.md` | Promoted to a workflow plan | `.agents/workflow/plans/ci-smoke-suite-hardening/` | Canonical plan bundle created on 2026-04-19. The active Markdown artifact is now lineage, not the canonical execution home. |
| `.agents/active/closeout-ci-fix.plan.md` | Do not create a standalone canonical plan; fold into the existing canonical loop-agent plan/history if the follow-up remains relevant | `.agents/workflow/plans/loop-agent-pipeline/` and `.agents/history/loop-agent-pipeline/` | This is a narrow tactical follow-up to post-closeout work, not a durable independent roadmap. |
| `.agents/active/ralph-fanout-and-runtime-overrides.plan.md` | Promoted to a workflow plan | `.agents/workflow/plans/ralph-fanout-and-runtime-overrides/` | Canonical plan bundle created on 2026-04-19. The active Markdown artifact is now lineage, not the canonical execution home. |
| `.agents/active/ralph-runtime-permissions-and-error-handling.plan.md` | No migration needed in this pass | `.agents/history/ralph-runtime-permissions-and-error-handling/` | Completed runtime work already has a history home and is script-focused. |

## Cleanup Status

The following legacy active plan bodies have now been replaced with short lineage pointers in `.agents/active/`:

- `planning-evidence-backed-write-scope`
- `loop-agent-pipeline-resurrection`
- `graph-bridge-command-readiness-resurrection`
- `kg-command-surface-readiness-analysis`
- `go-native-code-graph-analysis`
- `ci-smoke-suite-hardening`
- `closeout-ci-fix`
- `ralph-fanout-and-runtime-overrides`
- `ralph-runtime-permissions-and-error-handling`

## Do Not Migrate

These are transient coordination artifacts, not canonical plan/spec candidates:

- `.agents/active/active.loop.md`
- `.agents/active/orchestrator.loop.md`
- `.agents/active/loop-state.md`
- `.agents/active/delegation/`
- `.agents/active/delegation-bundles/`
- `.agents/active/iteration-log/`
- `.agents/active/fold-back/`

They should stay transient or move to history/archive paths, not to `.agents/workflow/plans/` or `.agents/workflow/specs/`.
