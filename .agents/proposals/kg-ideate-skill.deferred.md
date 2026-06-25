# Deferral note: kg-ideate-skill

**PROMOTED 2026-06-25** → spec `.agents/workflow/specs/kg-ideate-skill/design.md` + plan
`.agents/workflow/plans/kg-ideate-skill/`. The non-citation phases (Phase 1 degraded + Phases
2–4) are scheduled active now; only the structural-contradiction step stays gated — see the
plan's `t7-structural-contradiction-traversal` (cross-plan dep on the citation adapter,
graph-backend-adapter-contract §13.4). This note is retained for the degradation rationale.

**Decision date:** 2026-05-20
**Re-scoped:** 2026-05-29 — proposal broadened from "KG-grounded spec authoring" to the full
ideation→execution pipeline (idea/proposal → spec → plan → concurrent staged execution) and
reframed as a **planned starter skill**. The re-scope weakens the original "fully deferred"
stance — see "Graceful degradation re-scope" below.
**Status:** partially deferred — only the adapter-gated contradiction step is blocked. The
broader skill can ship before the citation adapter lands.
**Proposal:** `.agents/proposals/kg-ideate-skill.yaml` (repo-local; `status: draft`).

## Graceful degradation re-scope (2026-05-29)

The original note treated the whole skill as deferred because Phase 2's contradiction step
depends on a `contradicting_claims` named query (graph-backend-adapter-contract §13.4) that is
not yet implemented. Under the broadened scope this is too strong:

- **Phase 1 (briefing)** grounds on semantic KG search (`da kg query` / MCP), the research
  corpus, and lessons — none of which need the citation adapter.
- **Phase 2 step 7 (structural contradiction traversal)** is the ONLY adapter-gated step. It
  degrades gracefully: when the adapter lacks `contradicting_claims`, the skill falls back to
  the competing-decisions treatment (two decision nodes with conflicting rationale). No other
  phase depends on it.
- **Phases 3–4 (plan scaffolding, execution handoff)** consume the briefing + spec and feed
  the fanout/ISP flow. They are adapter-independent.

Therefore the skill's non-citation parts (Phase 1 degraded-mode + Phases 2–4) are useful and
shippable **before** full citation-adapter availability. Only the structural-contradiction step
arrives later — and it no-ops cleanly into the documented fallback until then, so shipping early
does NOT reintroduce the silent-no-op trust problem the original note worried about.

## Trigger to revisit (now narrowed)

What was a blanket trigger is now scoped to the adapter-gated step only:

- The `contradicting_claims` structural traversal (Phase 2 step 7) flips from
  competing-decisions fallback to structural traversal **when**
  `dotagents-builtin:graph/citation@^1.0` ships that named query end-to-end
  (build → query → MCP) per graph-backend-adapter-contract §13.4.
- A partial-shipping path is fine: if the adapter ships only `claims_citing_source` and
  `claims_supported_by_source` (not `contradicting_claims`), the skill stays in the
  competing-decisions fallback and is otherwise fully functional.

The rest of the skill is **not** gated on the adapter and can be promoted to the starter
scaffold on its own schedule (see "Starter framing").

## Starter framing

This is a **planned starter skill**. Source lands at
`internal/scaffold/home/starter/skills/global/kg-ideate/` (ships via `da init`, installs at
`~/.agents/skills/kg-ideate/`). It is repo-local for now, pending the scoped proposal-routing
rework; routing may move it back to a starter-registration path once proposal-routing v2 lands.

Promotion timing is an open question flagged in the proposal (ship non-citation phases now vs
wait for full adapter availability) — for maintainer review.

## Why not reject

The capability gap kg-ideate addresses is real and recurring, and now spans the whole pipeline,
not just spec authoring. This session's sibling proposal
`.agents/proposals/kg-crg-aware-bundle-authoring.md` (dot-agents repo) targets the same gap from
the delegation-bundle side. Together they close the "we have accumulated KG knowledge but the
orchestrator and spec/plan author re-discover the same ground every session" failure mode
end-to-end.

## Cross-reference

- Companion proposal: `.agents/proposals/kg-crg-aware-bundle-authoring.md` (dot-agents repo) —
  write_scope-from-impact-radius pre-bundle step. Different lifecycle hook (delegation authoring
  vs spec/plan authoring), same underlying capability bet.
- Staged-execution tail: `.agents/workflow/specs/layered-pr-fanout/design.md` and the
  `[[orchestrator-session-start]]` / `[[isp]]` / `[[delegation-lifecycle]]` skills.
- Adapter-gated step source: `graph-backend-adapter-contract` plan in dot-agents
  `.agents/workflow/plans/` (currently `draft`; §13.4 defines `contradicting_claims`).
