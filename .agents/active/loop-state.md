# Loop State

Last updated: 2026-04-11
Iteration: 0 (initial seed — no automated iteration has run yet)

## Current Position

Driving specs:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — post-MVP workflow automation waves
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — KG subsystem with code-structure layer

Active wave summary (from `.agents/active/*.plan.md`):
- **crg-kg-integration**: Phase A + D complete, Phase B (parser port, tree-sitter) is next — primary active track
- **kg-phase-6-shared-memory-research**: research steps complete, optional prototype remaining
- **wave-7-cross-repo-sweep-drift**: status says Completed but has unchecked items — verify before picking
- **wave-6-delegation-merge-back**: all items checked, done
- **resource-intent-centralization**: plan written, implementation not started (architectural — skip)

Note: Many older plans (kg-phase-1 through 5, wave-3 through 5) show "Completed" in their status header but still have unchecked `- [ ]` items. The status header is authoritative — unchecked boxes on completed plans are stale plan hygiene, not real work.

## Last Completed

(Seed from manual Codex sessions `019d7a6d` and `019d7a9d`):
- Managed resource cleanup: path normalization, stale rule pruning, import-before-relink fix
- AGENTS.md regenerated as repo-local contributor guide
- Skill architect transforms for plan-wave-picker, delegation-lifecycle, provider-consumer-pair
- Skill import/promotion into canonical `~/.agents/skills/dot-agents/`
- AgentsRC local schema at `schemas/agentsrc.schema.json`
- Resource-intent-centralization plan written

## What's Next

Pick the first actionable implementation wave item. Likely candidates:
- `crg-kg-integration` Phase B items
- `kg-phase-6-shared-memory-research` next unchecked item
- `wave-7-cross-repo-sweep-drift` next unchecked item

## Skip List

Plans to skip (blocked, requires architectural work, completed, or out of scope for loop):
- `resource-intent-centralization` — architectural redesign, needs focused RFC session
- `resource-sync-architecture-analysis` — analysis-only, superseded by resource-intent-centralization
- `refresh-skill-relink` — blocked on resource-intent-centralization
- `skill-import-streamline` — blocked on resource-intent-centralization
- `platform-dir-unification` — blocked on resource-intent-centralization
- `agentsrc-local-schema` — completed
- `workflow-automation-product-spec-review` — review artifact, not implementation
- `wave-6-delegation-merge-back` — all items checked, completed
- `kg-phase-1-graph-core` — status: Completed
- `kg-phase-2-basic-ingest` — status: Completed
- `kg-phase-3-deterministic-query` — status: Completed
- `kg-phase-4-lint-maintenance` — status: Completed
- `kg-phase-5-bridge-readiness` — status: Completed
- `wave-3-structured-query-health-surface` — status: Completed
- `wave-4-shared-preferences` — status: Completed
- `wave-5-knowledge-graph-bridge` — status: Completed

## Blockers

- Shared `.agents/skills/*` projection bug: repo-local skill directories are not converted to managed symlinks after import. Documented in `skill-import-streamline.plan.md`. Blocked on resource-intent-centralization.
- `plan-wave-picker` SKILL.md at `~/.agents/skills/dot-agents/plan-wave-picker/SKILL.md` has invalid frontmatter (missing `---` delimiters). Codex warns on load.

## CLI Traces

(No traces yet — this section will be populated by automated iterations)

<!--
Format for CLI trace entries:

### Iteration N — YYYY-MM-DD HH:MM
```
$ go run ./cmd/dot-agents <command>
<output summary or error>
```
Classification: [ok] | [impl-bug] | [tool-bug] | [missing-feature]
-->

## CLI Observations

(No observations yet — this section captures UX friction, awkward flows, and feature requests from using the CLI during iterations)
