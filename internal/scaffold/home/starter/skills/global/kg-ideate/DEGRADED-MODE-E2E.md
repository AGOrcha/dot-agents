# kg-ideate — Degraded-Mode End-to-End Walkthrough

This is the **ship-now proof** for kg-ideate: a full four-phase run on a profile that
does **not** ship the citation adapter (no `contradicting_claims` named query), against a
topic the knowledge graph has never seen. It demonstrates that shipping before the
citation adapter exists does **not** reintroduce the silent-no-op problem — every
adapter-gated step degrades to an explicit, labelled fallback rather than quietly doing
nothing.

Illustrative output only. The blocks below are example renderings, not directives to load
files; nothing here is executed.

## Preconditions

- Active profile selects a plain graph adapter (e.g. no `dotagents-builtin:graph/citation@^1.0`).
  Preflight `da graph query --list-queries` returns **no** `contradicting_claims` entry.
- Topic: **"outbound webhook retry/backoff policy"** — a new area with zero prior KG nodes,
  no prior spec, and no matching lessons.

## Phase 1 — Briefing Producer (kg-brief)

KG traversal runs CLI-first (`da kg query "outbound webhook retry/backoff policy"`), falls
back to MCP semantic/graph search, and finds nothing. Per the Phase 1 rules, an empty KG
is reported as **"KG has no prior decisions on this topic"** — it is never fabricated. The
adapter preflight records that `contradicting_claims` is unavailable for this session, so
the Contradictions section is labelled **[adapter-absent]** (distinct from `[none]`, which
would mean "the adapter looked and found zero").

Rendered briefing block:

```
## KG Briefing: outbound webhook retry/backoff policy
Generated: 2026-07-04

### Prior Decisions (0 found)
[none]  (KG has no prior decisions on this topic)

### Research Findings (0 relevant)
[none]

### Contradictions (0 found)
[adapter-absent]  (contradicting_claims query unavailable on this profile)

### Applicable Lessons (0 found)
[none]

### Gaps (2 identified)
- No policy yet for max retry attempts vs. dead-letter — matters for spec's done criteria
- Backoff base/jitter unspecified — affects downstream throughput assumptions

### Prior Spec / Plan Overlap
[none]

### Impact Radius
[none]  (topic did not name code)
```

Key property: the absent adapter changes exactly **one** line (`[adapter-absent]` on
Contradictions). Every other section renders normally. The briefing is complete and
self-contained, so a spawned subagent planner can consume it as cold-start context.

## Phase 2 — Spec Scaffolding (spec-scaffold)

The contradiction step (step 7) is adapter-conditional. Because the preflight found no
`contradicting_claims` query, the step **no-ops cleanly into the competing-decisions
fallback** — it does not silently skip decision-making. Any competing positions surfaced in
Phase 1 are presented in step 5 as "competing decisions — choose one" (two positions with
conflicting rationale), and the spec still resolves them **explicitly** as decisions with
recorded rationale.

For this topic there was no prior competing decision in the KG, so the two Phase-1 gaps
are converted to open questions and the design forces an explicit choice:

```
## Decisions (design.md excerpt)
- D1: Bounded exponential backoff with full jitter, base 500ms, cap 30s.
      Rationale: chosen over fixed-interval retry (competing option) to avoid
      thundering-herd on a recovering downstream. Resolved explicitly, not defaulted.
- D2: 6 attempts, then dead-letter. Rationale: bounds worst-case delivery latency.

## Open Questions
- OQ1: Should per-endpoint retry budgets override the global policy?

## Done Criteria
- A webhook that fails N times lands in the dead-letter queue with the original payload.
- Backoff intervals observe the configured cap under sustained failure.
```

The absent adapter did **not** remove any decision from the spec; it only changed *how*
contradictions were sourced (fallback prose instead of a structural graph query). This is
the anti-silent-no-op guarantee in action.

## Phase 3 — Plan Scaffolding (plan-scaffold)

Fully normal — plan scaffolding does not depend on the citation adapter at all. The
sequential-vs-concurrent fork is decided (sequential here: OQ1 could change task ordering),
the spec requirements become bounded tasks with explicit `depends_on` ordering, and
write-scopes are derived from the Phase 1 impact radius (empty here, so scopes are grounded
from the design rather than a code radius). The plan is authored via
`da workflow plan create webhook-backoff-policy` and its `PLAN.yaml` / `TASKS.yaml` are kept
in sync — nothing about the missing adapter degrades this phase.

## Phase 4 — Execution Handoff (staged-execution-handoff)

kg-ideate **emits no code**. Phase 4 makes the direct-vs-fanout decision and passes the
briefing + spec + plan as orientation context into the orchestrator / staged runtime
(`[[orchestrator-session-start]]`, `[[isp]]`), which owns implementation, verification, and
review. The handoff is identical whether or not the citation adapter is present.

## Why this is the ship-now proof

Done-criteria 3 requires that early shipping — before the citation adapter lands — cannot
reintroduce the silent-no-op problem this pipeline was designed to avoid. This run shows the
single adapter-gated step (Phase 2 step 7) degrading through a **visible, labelled**
path at every point:

1. Phase 1 renders `[adapter-absent]` (not a blank, not `[none]`) and states "KG has no
   prior decisions on this topic" rather than fabricating.
2. Phase 2's contradiction step no-ops into the competing-decisions fallback while the spec
   still resolves competing decisions explicitly.
3. Phase 3 plan scaffolding is unaffected.
4. Phase 4 hands to the staged runtime and emits no code.

Nothing is skipped without a trace. The pipeline is useful today, and adding the citation
adapter later only upgrades Contradictions from the fallback to structural traversal — it
changes no other phase.
