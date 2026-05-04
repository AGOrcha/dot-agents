# Architecture Decision Records

This directory holds Architecture Decision Records (ADRs) for the dot-agents
project. ADRs capture **load-bearing decisions** with their context, the
choice made, and the consequences — so that engineers reading the source
later can reconstruct *why* the system is shaped the way it is.

ADRs follow the [Michael Nygard format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).
Convention is borrowed from the broader software engineering community
(Spring, AWS, Microsoft, ThoughtWorks, etc.) so engineers familiar with
ADRs in any other repo can navigate this one without retraining.

---

## Format

```markdown
# ADR-NNNN: <Short Decision Title>

**Status:** proposed | accepted | superseded by ADR-XXXX | deprecated
**Date:** YYYY-MM-DD
**Owners:** <team or person responsible>
**Related:** ADR-XXXX, spec/<id>, plan/<id>

## Context

What is the issue motivating this decision? What forces are at play
(technical, organizational, political, time)? What constraints exist?

## Decision

What is the change being proposed or made? State it as an active
imperative. Be specific.

## Consequences

What becomes easier? What becomes harder? What new risks does this
introduce? What follow-up work does it imply? What gets locked in
that future ADRs would need to supersede?

## Alternatives Considered (optional)

What other options were on the table? Why were they not chosen?

## References (optional)

Links to research, prior discussions, related specs/plans, external
sources.
```

---

## Conventions

- **Sequential numbering.** New ADRs get the next free integer, padded to
  four digits (`0007`, not `7`). Numbers are never reused or reordered.
- **Filename:** `NNNN-kebab-case-title.md`. Match the `# ADR-NNNN: ...`
  heading exactly.
- **Status field is load-bearing.** An accepted ADR is the durable
  decision; a superseded ADR is preserved verbatim with its `Status:`
  updated to `superseded by ADR-XXXX` so the chain is auditable.
- **No deletion.** Even if a decision was wrong, the ADR stays. Mark it
  `deprecated` or `superseded` instead.
- **One decision per ADR.** If the rationale needs more than one decision,
  split into multiple ADRs and cross-reference.
- **Index this file.** Add new entries to the table below in the same
  commit that creates the ADR.

---

## Relationship to other artifact tiers

ADRs are **between** specs and plans in the dot-agents artifact model:

```
spec   → workflow/specs/<id>/design.md       — what & why; multi-page contract
ADR    → docs/adr/NNNN-<title>.md             — single load-bearing decision; portable, citable
plan   → workflow/plans/<id>/                 — how & in what order
tasks  → TASKS.yaml                           — work queue
history → history/<id>/                       — permanent record
```

- A **spec** holds multiple decisions inline; load-bearing ones get
  *extracted* to ADRs and cited by ID.
- A **plan** that makes architectural choices during execution should
  produce ADRs as deliverables — those decisions become discoverable
  outside the plan's narrative.
- A **lesson** that has matured into an organizational design choice
  (rather than a one-off correction) is a candidate for graduation
  to an ADR.
- An **ADR** does NOT replace the proposal/review system in
  `~/.agents/proposals/` — proposals propose changes to shared resources
  (rules, skills); ADRs document architectural decisions for the engineer
  reading source.

---

## ADR ↔ Knowledge Graph

ADRs are scoped-KG `decision`-typed notes (per `interaction_label: decision`
from research evaluation §A.2 ashwingop ontology). Future tooling may
ingest ADRs into the KG so that:

- `dot-agents kg adr index` — scans `docs/adr/*.md`, ingests as decision-typed
  scoped-KG notes with status, supersedes graph, and content hash.
- `dot-agents kg adr query <id>` — returns the ADR plus its supersedes-graph
  and **sightings** (every spec/plan/commit/code reference).
- `kg lint --adr` — finds orphaned ADRs (no sightings), broken supersedes
  chains, missing status, and ADRs without owners.

Code can cite ADRs by inline comment: `// see ADR-0007`. The KG bridge
should treat such citations as edges.

This integration is **deferred** to a future plan; the present-day
convention is the markdown format and index below.

---

## Index

| ADR | Status | Title | Owners | Date |
|---|---|---|---|---|
| [0001](0001-adopt-architecture-decision-records.md) | accepted | Adopt Architecture Decision Records | dot-agents | 2026-05-03 |
| 0002 | proposed | Self-review output destination and schema | dot-agents | TBD (produced by `self-review-iteration-close-wiring` plan, t1) |
| 0003 | proposed | Self-review fires before verify-record-test | dot-agents | TBD (produced by `self-review-iteration-close-wiring` plan, t1) |
| 0004 | proposed | Execution-telemetry schema seeded by review-decision.yaml | dot-agents | TBD (produced by `self-review-iteration-close-wiring` plan, t4) |
| 0005 | proposed | Restore KG-context calls in self-review (regression fix) | dot-agents | TBD (produced by `self-review-iteration-close-wiring` plan, t1) |
| [0006](0006-da-rename-strategy.md) | accepted | Binary rename strategy — `dot-agents` → `da` via hard cutover | dot-agents | 2026-05-03 |
| 0007 | conditional | TS port binary naming (`da-ts` vs `dot-agents-ts`) | dot-agents | TBD (only produced if `binary-rename-da-sweep` plan t6 surfaces a non-trivial decision) |

---

## How to write a new ADR

1. Pick the next free number from the index above.
2. Copy the format template above into `NNNN-kebab-case-title.md`.
3. Fill in Context / Decision / Consequences. Keep it under one page if
   possible.
4. Set `Status: proposed` if you want review before adopting; `accepted`
   if the decision is being recorded after the fact.
5. Add the row to the index in the same commit.
6. If the ADR supersedes a prior one, update the prior ADR's `Status:`
   line to `superseded by ADR-NNNN` in the same commit.
