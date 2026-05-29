# Deferral note: kg-ideate-skill

**Decision date:** 2026-05-20
**Status:** deferred — do NOT apply via `da review approve` until the trigger condition below clears.
**Original proposal:** `~/.agents/proposals/kg-ideate-skill.yaml` (unmodified — kept in the queue so next session sees it)

## Trigger to revisit

The proposal's Phase 2 contradiction-handling step is **adapter-conditional**: it depends on the active profile shipping a `contradicting_claims` named query. The proposal names `dotagents-builtin:graph/citation@^1.0` as the reference adapter. That adapter is specified in the `graph-backend-adapter-contract` spec (§13.4) but has not been **implemented** — graph-backend-adapter-contract plan is currently `draft` with 6 pending tasks.

Revisit when:
- Graph-backend-adapter-contract v1 plan transitions to `complete`, AND
- The `dotagents-builtin:graph/citation@^1.0` adapter is shipped end-to-end (build → query → MCP) per spec §13.4.

A weaker partial-shipping trigger would be acceptable: if the citation adapter ships only `claims_citing_source` and `claims_supported_by_source` (not `contradicting_claims`), Phase 2 step 7 falls into its documented competing-decisions fallback, and the skill is partially useful. But the cleanest moment to land it is full adapter availability.

## Why not reject

The capability gap kg-ideate-skill addresses is real and recurring. This session's
sibling proposal `~/.agents/proposals/` → ... → `.agents/proposals/kg-crg-aware-bundle-authoring.md`
(in the dot-agents repo) targets the same gap from the delegation-bundle side. Together they would close the "we have accumulated KG knowledge but the orchestrator and spec author re-discover the same ground every session" failure mode end-to-end.

## Why not approve

Approval would `da review approve` the proposal, which is intended to materialize the skill scaffold. The scaffold's Phase 2 references a `da kg query --list-queries` preflight and `da graph query contradicting_claims --param claim_id=<id>` calls that have no live targets today. Shipping a skill whose Phase 2 silently no-ops is worse than not shipping it — the silent no-op would erode trust in the skill's brief itself.

## Cross-reference

- Companion proposal: `.agents/proposals/kg-crg-aware-bundle-authoring.md` (dot-agents repo) — write_scope-from-impact-radius pre-bundle step. Different lifecycle hook (delegation authoring vs spec authoring), same underlying capability bet.
- Blocking plan: `graph-backend-adapter-contract` in dot-agents `.agents/workflow/plans/` (currently `draft`).
