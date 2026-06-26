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

## The shared stage: `ideation-cycle` RUNS `kg-brief`

Both skills ground on the **same** primitive — the `kg-brief` molecule (KG / research /
lessons → briefing block). `ideation-cycle` RUNS `kg-brief` as its step 1; it does NOT carry
its own baseline scan and `kg-brief` is NOT handed down pre-baked unconditionally:

- **Dispatched from `kg-ideate`:** the Phase 1 briefing may be reused **by artifact** (0
  dispatch hops) — but ONLY if fresh. Freshness = the briefing's `inputs_digest` still
  matches the current KG snapshot / research set / lessons / idea text, AND no prior fork's
  resolution mutated shared state the brief depended on. On any mismatch, **re-run
  `kg-brief`**. (Same `inputs_digest` primitive config-v2 uses — `ComputeInputsDigest`,
  `sha256:…`; reuse it, don't fork a parallel staleness scheme.)
- **Standalone:** always run `kg-brief` fresh.

This is the anti-duplication invariant plus the no-stale-brief invariant. See
`instructions/ground-via-kg-brief.md`.

## The segment boundary + triage

The idea→spec segment (Phase 1 `kg-brief` + Phase 2 `spec-scaffold`) is now traversed by
this loop. Not every decision needs the full loop — `ideation-cycle` triages each fork
**autonomously and surfaces its rationale** (it does NOT ask for per-fork confirmation; the
human gate is spec ratification at converge, not triage — that is what stops the
re-explaining):

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

## Two invocation modes (BOTH registered)

`ideation-cycle` is a **top-level invocable skill AND dispatchable from `kg-ideate`** — it
is independently useful (the fork-resolution loop ran standalone for every config-profiles
decision), not only a `kg-ideate` sub-step.

- **Dispatched** (from `kg-ideate` Phase 2): resolve ONE named fork; return decision +
  evidence; do not author the surrounding spec. Steps 1–2 are scoped to that fork.
- **Standalone** (a one-off design question, or a fork that surfaced from execution): run
  the full cycle including seeding/refining the spec at converge (step 6). This is the
  entry point when there is no `kg-ideate` run in flight.

## Fork evidence is a per-fork sidecar

A hard fork's evidence — the prototype dir, the negative-control result, the cross-brain
audit verdicts — is its **own artifact (a sidecar)**, LINKED from the spec's decision entry.
Not inlined into the spec, not buried in transient task notes. This anticipates the lineage
schema (decision `derives_from` evidence edge): the decision points at its evidence sidecar.
Standalone mode links the sidecar from the spec it seeds; dispatched mode returns the
sidecar pointer to `spec-scaffold` to link from the decision it folds in.

## Dispatch depth (resolved — depth governs, not tier-adjacency)

The reliability finding behind the tier contract is about dispatch **DEPTH**, not
tier-adjacency. Composition is governed by **≤2 reliable skill-to-skill hops on any runtime
path**; a molecule MAY call molecules (see the refined contract delta:
`.agents/proposals/skill-tiering-molecule-composition.md`). The chain here:

```
kg-ideate (compound)  →  spec-scaffold (molecule)  →  ideation-cycle (molecule)  →  kg-brief
        hop 1                     hop 2                        ↑ TERMINAL LEAF
```

`kg-brief` calls nothing downstream. On any runtime path it either **reuses the upstream
briefing by artifact (0 dispatch hops)** or **re-runs as a leaf** — neither extends
unbounded-dispatch depth. So `kg-ideate → spec-scaffold → ideation-cycle` is a 2-hop path,
in-bound. **No hoist is needed** — forcing `spec-scaffold` to bounce control back to the
compound purely to satisfy tier-adjacency was the artifact of an over-strict rule; the
depth-governed contract removes it. The static call-graph depth here is 2 (warning fires
only at >2).
