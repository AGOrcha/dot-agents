# ADR-0001: Adopt Architecture Decision Records

**Status:** accepted
**Date:** 2026-05-03
**Owners:** dot-agents
**Related:** [`agent-context-resolution-architecture.md`](../../.agents/proposals/agent-context-resolution-architecture.md), [`workflow-artifact-model`](../../../../.agents/rules/dot-agents/workflow-artifact-model.md) (rule)

## Context

The dot-agents project has accumulated load-bearing architectural
decisions across specs, plans, lessons, and inline prose:

- "We are a Camp 2 (context substrate) system, not Camp 1
  (memory backend)" — currently in research evaluation §B theme 1, not
  cited from any rule or spec.
- "We adopt async peer review for promotion gates" — referenced in the
  agent-context-resolution architecture note but not extractable.
- "Tier is declared in the plan, validated at contract time, with a
  runtime escape hatch" — buried in conversation history.
- "Iter-log review block populates only via `workflow checkpoint --role
  review`, not via direct file edit" — discovered through audit, not
  written down.

These decisions are stable, cross-cutting, and non-obvious. New engineers
reading the source today have no way to find them without reading every
spec, every plan narrative, and every research evaluation. Tying decisions
to specs makes them invisible to engineers who never open the spec; tying
them to plans makes them invisible after the plan is archived.

The wider software-engineering community has a well-established format
for this exact problem: Architecture Decision Records, popularized by
Michael Nygard's 2011 essay and adopted broadly (Spring, AWS, Microsoft,
ThoughtWorks, ADR Tools project, etc.).

The existing dot-agents artifact tiers (spec → plan → tasks → history)
do not have a slot for *single load-bearing decisions*. Specs hold many
decisions inline; plans hold none. ADRs fit cleanly **between** spec and
plan: a spec produces several ADRs as it stabilizes; an ADR is referenced
by multiple plans.

## Decision

Adopt Architecture Decision Records using the Nygard format under
`docs/adr/NNNN-kebab-case-title.md`, indexed in `docs/adr/README.md`.

Specifics:

- **Format:** Nygard (Title / Status / Context / Decision / Consequences;
  optional Alternatives, References).
- **Location:** top-level `docs/adr/` — *not* under `.agents/`. ADRs are
  for engineers reading the source, not for the agent runtime. Top-level
  `docs/` matches industry convention and is discoverable.
- **Numbering:** sequential integers, four-digit zero-padded. Never
  reused, never reordered.
- **Lifecycle:** `proposed` → `accepted` → `superseded by ADR-XXXX` |
  `deprecated`. Superseded ADRs are preserved verbatim with updated
  Status; the supersession chain is auditable.
- **Granularity:** one decision per ADR.
- **Indexing:** every ADR gets a row in `docs/adr/README.md` in the same
  commit that creates it.

ADRs do **not** replace:

- Specs (`workflow/specs/<id>/design.md`) — multi-page contracts.
- Plans (`workflow/plans/<id>/`) — how and in what order.
- Proposals (`~/.agents/proposals/<id>.yaml`) — change requests for
  shared resources, processed by `da review approve`.
- Lessons (`.agents/lessons/<name>/LESSON.md`) — corrections from
  mistakes.

A lesson that matures into a design *choice* (rather than a correction)
is a candidate for graduation to an ADR.

## Consequences

**Easier:**

- Engineers reading source can grep `docs/adr/` to find load-bearing
  decisions without reading every spec.
- Code citations (`// see ADR-0007`) become navigable.
- Future KG integration: ADRs are scoped-KG `decision`-typed notes
  (per ashwingop ontology, research §A.2); a future `da kg adr
  index` command can ingest them automatically.
- Plans can produce ADRs as deliverables, externalizing decisions made
  during execution from the plan narrative.

**Harder:**

- One more artifact type to maintain.
- Convention discipline required: numbering, status updates on
  supersession, index sync.
- Drift risk if ADRs are written and then never updated when superseded
  by code reality.

**New risks:**

- "Decision theater" — writing ADRs because the convention says so,
  without actual decisions to record. Mitigate by reserving ADRs for
  *load-bearing* decisions only; routine implementation choices stay in
  plans.
- Numbering collisions in concurrent branches. Mitigate by treating
  numbers as proposal-only until merge; reserve numbers in PR
  descriptions.
- Splintering: decisions split across too many ADRs lose their
  relationships. Mitigate by liberal cross-references and by keeping
  the index narrative-readable.

**Follow-up work:**

- The first plan to use ADRs is `self-review-iteration-close-wiring`,
  which will produce ADR-0002 through ADR-0005 as part of its
  execution.
- A future KG-ADR plan will land `da kg adr {index, query,
  supersede, sightings}` commands so ADRs become first-class citizens
  of the scoped knowledge graph.
- A retroactive backfill task may write ADRs for prior conversation-level
  decisions: managed-compounding terminology, proposal-system reuse for
  KG promotions, tier declaration site, async peer review. Backfill is
  not blocking and can be scheduled later.

**What gets locked in:**

- The Nygard format. Switching to a different format (MADR, Y-statement,
  etc.) later would require a superseding ADR and a migration pass.
- The `docs/adr/` location. Moving ADRs into `.agents/` later would
  break code citations and external links.

## Alternatives Considered

- **Embed all decisions in spec `## Decisions` sections.** Rejected:
  decisions become invisible once the engineer leaves the spec; they
  cannot be cited from code; they cannot be superseded cleanly.
- **`.agents/decisions/` instead of `docs/adr/`.** Rejected: ADRs are
  for human source-readers, not agent runtime. Industry convention is
  `docs/adr/`. Sticking with the convention is the whole point.
- **Y-statement format ("In the context of X, facing Y, we decided Z to
  achieve A, accepting downsides B").** Considered: more compact than
  Nygard. Rejected for now: Nygard's separated Context/Decision/
  Consequences sections support more thorough rationale, which matters
  for the kind of decisions we expect to record. Future ADR can supersede
  this one if Y-statements prove sufficient.
- **Skip ADRs; rely on commit messages and PR descriptions.** Rejected:
  commit history is hard to query for "what was decided about X"; PR
  descriptions are too ephemeral.

## References

- Michael Nygard, *Documenting Architecture Decisions* (2011): https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions
- ADR Tools community: https://adr.github.io/
- `agent-context-resolution-architecture.md` §1.5 (resource graduation
  matrix) and §6.5 (audit-confirmed pipeline state) — the architectural
  context that motivated this adoption.
- Research evaluation §A.2 ashwingop *Company Brain* Part 3 — the
  ontology framing that maps ADRs to scoped-KG `decision`-typed notes.
