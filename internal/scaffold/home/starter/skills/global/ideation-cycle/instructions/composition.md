# Composition: ideation-cycle as kg-ideate's evolved idea→spec stage

`ideation-cycle` is the **matured form of `kg-ideate`'s idea/proposal → spec segment** —
not a sibling engine `kg-ideate` happens to dispatch. Read this whenever you are invoked
from `kg-ideate`, or when deciding whether a question belongs here. Canonical
formalization: `.agents/workflow/specs/ideation-system-composition/design.md`.

## The evolution (segment ownership)

```
kg-ideate lifecycle:  idea/proposal ──▶ spec ──▶ plan ──▶ handoff
                      └────────────────────┘     Phase 3   Phase 4
                       idea→spec SEGMENT
                       = Phase 1 (kg-brief) + Phase 2 (spec-scaffold)
                       ▼ EVOLVED INTO ▼
                      ideation-cycle (this molecule)
```

| | `kg-ideate` | `ideation-cycle` (this molecule) |
|---|---|---|
| **Kind** | T2 compound — whole-pipeline authoring front-end | T1 molecule — the evolved idea→spec segment |
| **Owns** | the WHOLE pipeline: idea → spec → plan → handoff | the idea→spec SEGMENT + fork-resolution within it |
| **Verb** | AUTHORS the pipeline grounded in the KG | how idea→spec is now DONE: grounded idea → ratified spec |
| **Output** | a full spec + plan + handoff | a ratified spec decision + evidence (prototype, audit, cross-brain) |
| **Decides by** | scaffolding + (for hard forks) this loop | empirical prototype + fidelity gate + cross-brain |

`kg-ideate` still owns the whole pipeline. `ideation-cycle` is how its idea→spec front is
now traversed — the segment where hard/open forks concentrate. Phases 3–4 (`plan-scaffold`,
handoff) are unchanged; control returns to `kg-ideate` with a ratified spec in hand.

**Lineage (for the KG / lineage schema):** `ideation-cycle` `derives_from` /
`supersedes` `kg-ideate`'s original idea→spec stage — an *evolution* edge, not a
`related_to` sibling edge.

## The shared stage: grounding IS `kg-brief`

Both skills ground on the **same** primitive — the `kg-brief` molecule (KG / research /
lessons → briefing block). `ideation-cycle` does NOT carry its own baseline scan:

- **Dispatched from `kg-ideate`:** the Phase 1 briefing already exists; consume it.
- **Standalone:** invoke `kg-brief` first to produce that same briefing.

This is the anti-duplication invariant. See `instructions/ground-via-kg-brief.md`.

## The segment boundary + triage

The idea→spec segment (Phase 1 `kg-brief` + Phase 2 `spec-scaffold`) is now traversed by
this loop. Not every decision needs the full loop — triage within the segment:

1. **Briefing-decidable** → resolved inline, exactly as `spec-scaffold` does today. The
   briefing settles it; no prototype, no cross-brain.
2. **Hard / open fork** → runs the full `ideation-cycle` loop (classify →
   empirical[fidelity-gate] / cross-brain → ratify). The ratified decision + evidence is
   folded into the spec being authored.

The boundary test — "is this briefing-decidable, or does it need discovery?":

- The briefing answers it (a prior ratified decision, a clear gap with one obvious
  resolution) → **briefing-decidable**, inline.
- The briefing surfaces a contradiction, a `[PROPOSED]` with no backing, or a trade-off
  the briefing can't adjudicate, AND getting it wrong is costly → **hard fork**, full loop.

The segment's output is a **ratified spec in hand**; control returns to `kg-ideate` to
continue at Phase 3 (`plan-scaffold`). `ideation-cycle` owns the idea→spec method;
`kg-ideate` owns the surrounding pipeline.

## Two invocation modes

- **Dispatched** (from `kg-ideate` Phase 2): resolve ONE named fork; return decision +
  evidence; do not author the surrounding spec. Steps 1–2 are scoped to that fork.
- **Standalone** (a one-off design question, or a fork that surfaced from execution): run
  the full cycle including seeding/refining the spec at converge (step 6). This is the
  entry point when there is no `kg-ideate` run in flight.

## Dispatch-hop note (surface to the owner if it bites)

The tier contract (`skill-tiering-contract` §1.2) holds that agents reliably dispatch
through **1–2 hops** of skill-to-skill composition and degrade past that. The full chain
here is:

```
kg-ideate (compound)  →  spec-scaffold (molecule)  →  ideation-cycle (molecule)  →  kg-brief (reuse)
        hop 1                     hop 2                        hop 3
```

Dispatching `ideation-cycle` from inside `spec-scaffold`, and then reusing `kg-brief` from
inside `ideation-cycle`, can exceed the reliable 1–2-hop bound. Two ways to stay in bound,
**for the owner to rule** (see the composition spec's open questions):

- **(a) Hoist the dispatch to the compound.** `spec-scaffold` does not call
  `ideation-cycle` itself; it *flags* a decision as a hard fork and returns control to the
  `kg-ideate` compound, which dispatches `ideation-cycle` as a sibling phase. Keeps every
  edge at hop ≤ 2 from the compound. **(default recommendation.)**
- **(b) Reuse `kg-brief` by artifact, not by dispatch.** When dispatched, `ideation-cycle`
  consumes the *already-produced* briefing (no `kg-brief` re-dispatch), so the chain is
  data-passing, not a 3rd dispatch hop. Standalone runs are hop-1 to `kg-brief` and fine.

These are not yet ratified — they are the live composition ambiguity. Do not hard-code one
until the owner picks; the composition spec tracks it as an open question.
