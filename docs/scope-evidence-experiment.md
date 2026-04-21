# Scope Evidence Sidecar — Manual Experiment

**Date:** 2026-04-21
**Task:** sidecar-manual-experiment (planner-evidence-backed-write-scope plan)

## What was done

Two `.scope.yaml` sidecar files were hand-authored using `git log`, `git show --stat`,
and `git show --name-only` as ground truth for actual changed files, plus TASKS.yaml
task notes and delegation.yaml artifacts as context for decisions and write-scope intent.

### Sidecars produced

1. `.agents/workflow/plans/graph-bridge-command-readiness/evidence/implement-graph-bridge-readiness-fixes.scope.yaml`
   - Task: `implement-graph-bridge-readiness-fixes` in `graph-bridge-command-readiness`
   - Ground truth commit: `ed0ff9f` (1289 insertions across 31 files)
   - Confidence: high

2. `.agents/workflow/plans/kg-command-surface-readiness/evidence/kg-freshness-impl.scope.yaml`
   - Task: `kg-freshness-impl` in `kg-command-surface-readiness`
   - Ground truth commit: `e09e1ac` (multiple tasks bundled)
   - Confidence: high

## What worked

- **Seeds produced accurate required_paths.** For `implement-graph-bridge-readiness-fixes`,
  seeding on `runWorkflowGraphQuery`, `AgentsRCKG`, and `syncCodeWarmLink` would have
  identified all 14 final_write_scope files via callers and symbol_lookup queries. The
  seed-to-path mapping is reliable when the implementation is anchored to well-named entrypoints.

- **decision_locks were clear from task notes.** The TASKS.yaml notes for both tasks
  contained enough rationale to populate decision_locks without ambiguity. The note format
  actively supported sidecar authoring.

- **excluded_paths were non-obvious without git.** Without inspecting the actual commit,
  it would have been impossible to know that `commands/agents/` (error-message-compliance
  scope) and `.agents/prompts/isp.prompt.md` (orchestrator scope) landed in the same
  commit as `kg-freshness-impl`. A future `derive-scope` command must have access to
  git-diff data, not just graph queries.

## Gaps and surprises

### Gap 1 — Multi-task commits break clean attribution

`kg-freshness-impl` and `error-message-compliance` changes landed in commit `e09e1ac`.
The `excluded_paths` list had to explicitly call out `commands/agents/` and
`docs/ERROR_MESSAGE_CONTRACT.md` as out-of-scope. A one-task-per-commit discipline would
make sidecars dramatically more accurate and reduce the excluded_paths burden.

**Implication for `derive-scope`:** the command must accept a git ref or file list as
ground truth input, not just rely on graph queries which cannot distinguish task attribution.

### Gap 2 — Graph queries add little for pure research/audit tasks

For `inventory-current-bridge-behavior` and `kg-freshness-audit` (the audit tasks),
graph queries (symbol_lookup, callers, impact) would produce no useful signal because
those tasks write only docs/ and .agents/ artifacts, not code. The schema's `queries`
array would be empty or trivially populated for research/doc mode tasks.

**Implication for schema:** the `mode` field (`code` | `research` | `doc`) is load-bearing;
`derive-scope` should skip scope-lane graph queries entirely for non-code tasks.

### Gap 3 — optional_paths vs required_paths boundary is judgment-heavy

For `implement-graph-bridge-readiness-fixes`, `commands/add.go` had a 2-line change
(ExtraFields guard replacement). Placing it as optional rather than required was a
judgment call — small change, low risk, but still a correctness concern if missed.

A `check-scope` command that flags "touched optional path" differently from "touched
excluded path" would surface this case appropriately.

### Gap 4 — `stop_conditions` are easy to under-specify

The stop_conditions for both sidecars were synthesized from task notes (what was deferred).
Without a canonical "deferred items" section in the task note format, stop_conditions
require reading across multiple tasks and their depends_on relationships. A structured
`deferred_to` field in TASKS.yaml task notes would make stop_condition authoring mechanical.

### Gap 5 — Schema validation requires tooling not yet wired

Validation of `.scope.yaml` against `schemas/workflow-scope-evidence.schema.json` was
done manually by cross-checking field names. The `go test ./commands/workflow/...` suite
only validates the Go struct round-trip; it does not validate actual sidecar YAML files
on disk against the JSON schema. A `workflow plan check-scope --validate-schema` path
or a test fixture that loads real sidecars would close this gap.

## Recommendations for `derive-scope` command

1. Accept `--from-commit <sha>` to extract changed-file ground truth from git, not only graph.
2. Distinguish `mode: code` vs `mode: research` before running scope-lane graph queries.
3. Surface `excluded_paths` candidates by comparing `--from-commit` diff against TASKS.yaml
   write_scope to flag out-of-scope files in the diff.
4. Add a `--validate-schema` flag that runs JSON schema validation on the output sidecar.
