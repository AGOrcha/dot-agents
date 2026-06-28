# Composition: ideation-cycle as kg-ideate's evolved idea→spec stage

`ideation-cycle` is the **matured form of `kg-ideate`'s idea/proposal → spec segment** —
not a sibling engine `kg-ideate` happens to dispatch. Read this whenever you are invoked
from `kg-ideate`, or when deciding whether a question belongs here.

## The evolution (segment ownership)

```
kg-ideate lifecycle:  idea/proposal ──▶ spec ──▶ plan ──▶ handoff
                      └────────────────────┘     Phase 3   Phase 4
                       idea→spec SEGMENT
                       = Phase 1 (kg-brief) + Phase 2 (spec-scaffold)
                       ▼ EVOLVED INTO ▼
                      ideation-cycle (this compound)
```

| | `kg-ideate` | `ideation-cycle` (this compound) |
|---|---|---|
| **Kind** | T2 compound — whole-pipeline authoring front-end | **T2 compound** — the evolved idea→spec fork-resolution (orchestrates delegated workers with unbounded judgment) |
| **Owns** | the WHOLE pipeline: idea → spec → plan → handoff | the idea→spec **fork-resolution** (NOT the spec prose) |
| **Verb** | AUTHORS the pipeline grounded in the KG | how idea→spec is now DONE: grounded idea → **ratified decision + evidence** |
| **Output** | a full spec + plan + handoff | **ratified decision(s) + per-fork evidence sidecar** (it returns these; it does not type the spec file) |
| **Decides by** | scaffolding + (for hard forks) this loop | empirical prototype + fidelity gate + cross-brain |

`kg-ideate` still owns the whole pipeline. `ideation-cycle` is how its idea→spec front is now
traversed — the segment where hard/open forks concentrate. It **returns** ratified decisions +
evidence; **`spec-scaffold` writes the spec prose**; Phases 3–4 (`plan-scaffold`, handoff) are
unchanged; control returns to `kg-ideate`.

**Tier note:** `ideation-cycle` is a **compound** (re-tiered from molecule). By the tiering
contract's own definition a compound orchestrates with *unbounded* judgment — and this loop
orchestrates delegated workers (prototype authors, the independent cross-harness auditor, the
cross-brain reviewer), choosing which to run and how to weigh them. That is compound behavior.

**Lineage (for the KG / lineage schema):** `ideation-cycle` `derives_from` /
`supersedes` `kg-ideate`'s original idea→spec stage — an *evolution* edge, not a
`related_to` sibling edge.

## The shared stage: `ideation-cycle` RUNS `kg-brief`

Both skills ground on the **same** primitive — the `kg-brief` molecule (KG / research /
lessons → briefing block). `ideation-cycle` RUNS `kg-brief` as its step 1; it does NOT carry
its own baseline scan and `kg-brief` is NOT handed down pre-baked unconditionally:

- **Dispatched from `kg-ideate`:** the Phase 1 briefing may be reused **by artifact** — but
  ONLY if fresh. Freshness = (i) the briefing's `inputs_digest` over a **concrete input set**
  (idea text hash + KG snapshot id + named-query result hashes + applicable-lessons set +
  cited-artifact content hashes, canonicalized/ordered) still matches, AND (ii) **no entry in
  the brief's dependency manifest** (the KG nodes / decisions / lessons it actually read)
  changed — that is the operational test for "a prior fork mutated shared brief state". On any
  mismatch, **re-run `kg-brief`**. (Same `inputs_digest` primitive config-v2 uses —
  `ComputeInputsDigest`, `sha256:…`; reuse it, don't fork a parallel staleness scheme.)
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
   **returned** for `spec-scaffold` to fold into the prose.

**Triage guard (closes the silent-skip-the-gate hole):** a "briefing-decidable" verdict MUST
**cite the decisive briefing fact** (the specific prior decision / lesson / query result that
settles it). If no decisive fact is citable, the fork **defaults to HARD**. And the step-5
cross-brain pass **reviews the triage decisions themselves**, not only the hard forks — a
different harness checks each "easy" call was genuinely settled by its cited fact, so a fork
can't be quietly waved past the gate by mislabeling it.

The boundary test — "is this briefing-decidable, or does it need discovery?":

- The briefing answers it **with a citable decisive fact** (a prior ratified decision, a clear
  gap with one obvious resolution) → **briefing-decidable**, inline.
- The briefing surfaces a contradiction / `[PROPOSED]` with no backing / unadjudicable
  trade-off, OR nothing decisive is citable, AND getting it wrong is costly → **hard fork**.

**Segment ownership (who writes the spec):** `ideation-cycle` **RETURNS** the ratified
decision(s) + the per-fork evidence sidecar; **`spec-scaffold` WRITES the spec prose.**
`ideation-cycle` does not author the spec file. Control then returns to `kg-ideate` to continue
at Phase 3 (`plan-scaffold`). `ideation-cycle` owns the fork-resolution method; `spec-scaffold`
owns the prose; `kg-ideate` owns the surrounding pipeline.

## Two invocation modes (BOTH registered)

`ideation-cycle` is a **top-level invocable skill AND dispatchable from `kg-ideate`** — it
is independently useful (the fork-resolution loop ran standalone for every config-profiles
decision), not only a `kg-ideate` sub-step.

- **Dispatched** (from `kg-ideate` Phase 2): resolve ONE named fork; **return the ratified
  decision + evidence sidecar pointer** to `spec-scaffold`; do not author any spec prose. Steps
  1–2 are scoped to that fork.
- **Standalone** (a one-off design question, or a fork that surfaced from execution): run the
  full cycle to a ratified decision + sidecar, then hand those to a spec-drafting step (a
  delegated `spec-scaffold`-equivalent) — `ideation-cycle` still does not type the spec file.
  This is the entry point when there is no `kg-ideate` run in flight.

## Fork evidence is a per-fork sidecar

A hard fork's evidence — the prototype dir, the negative-control result, the cross-brain
audit verdicts — is its **own artifact (a sidecar)**, LINKED from the spec's decision entry.
Not inlined into the spec, not buried in transient task notes. This anticipates the lineage
schema (decision `derives_from` evidence edge): the decision points at its evidence sidecar.
Standalone mode links the sidecar from the spec it seeds; dispatched mode returns the
sidecar pointer to `spec-scaffold` to link from the decision it folds in.

## Composition bounds — engineering, not a measured fidelity cliff

The original worry was that `kg-ideate → spec-scaffold → ideation-cycle` crosses a
dispatch-hop "cliff" the tier contract warns about. We **dogfooded that question** (the first
use of this very skill on its own founding fork): an experiment arc across v1 (token-recall),
v2 (multi-constraint drift), v3 (regime-valid token-mass × depth × lossy-relay), and v4
(compounding-constraints × capability) on two harnesses — **v4 folded narrow per its GATE-2
audit; v5 deferred**. What it actually shows, and how to act on it:

- **No same-agent hop-depth fidelity cliff was FOUND in the tested regime** — constraint-honoring
  stayed flat through depth 10 and ~15k accumulated tokens on *local self-checkable* constraints,
  including an adversarial buried-constraint placement, on Opus 4.8 AND codex/GPT. **This null is
  power-limited** (depth-1 ceiling ~97.6%, never sub-90%), so write it *"no cliff found in the
  tested regime,"* **never** *"no cliff exists."* Do **not** cite the contract's "degrades past
  depth ~2–3 hops" as established fact.
- **v4 found a NARROW drift — on compounding work, not hop depth.** Error-prone, referential,
  *compounding* constraints with a binary no-partial-credit metric induced drift (Haiku
  80%→40%→0%; Opus slips to 80%). But its **GATE-2 audit ruled the broad claim NOT-SOUND** (one
  family; confounded length/edges/output-size; not a route-by-tier law). So it supports only:
  **decompose** error-prone compounding chains into independently-verifiable sub-artifacts, and
  prefer a **code-executing agent** for computable closures. Mechanism deferred to **v5**. This is
  about compounding-chain length, NOT skill-to-skill hop depth.
- **Tier-adjacency was the wrong lever anyway.** A compound/molecule may call any tier, so the
  chain is legal with no hoist — independent of the cliff question.
- **The real, evidence-backed bounds are engineering** — and **harness-capability-dependent**
  (the numbers below are **current-harness-observed**, not universal; re-assess per harness as the
  set grows — Hermes, Pi-agent, Aider, Antigravity-CLI, … — treating a new harness's limits as a
  variable to measure, not assume):
  - **Infra delegation-nesting ceiling (~hop 4, current harness).** Nested `Agent`-tool delegation
    collapses past ~hop 4 on Claude Code's `Agent` tool (reproduced): subagents lack
    the dispatcher tool. That number is a property of *this* harness — a harness whose subagents can
    delegate raises it. Regardless, deep multi-hop delegation should be **driver-orchestrated
    hop-by-hop** (a fresh top-level worker per hop, relay via on-disk artifact), not recursively
    nested, until a harness is measured to nest reliably deeper.
  - **Relay discipline (below).**
  - **Decompose error-prone compounding work** (narrow v4 support) — fan-out into bounded,
    independently-verifiable sub-artifacts rather than one long compounding chain.
  - **Hygiene** — context isolation, parallelism, write-scope discipline, inspectability.

## Relay discipline (the headline finding — every hand-back is structured/pointer-based)

The one positive, reproducible result of the whole investigation: **lossy summary relay drops
non-reconstructable detail, and that loss reaches the deliverable.** In one measured case, a
hop's summary hand-back compressed away an arbitrary schema choice ("env as a list of
{key,value} mappings" → "4 key/value entries") and the terminal artifact **degraded 16/16 →
13/16**. Verbatim/structured relay was **lossless across 8 hops**. Reconstructable defaults
(e.g. "cite the version in each section") survive a retell because a competent author
re-derives them; **non-reconstructable structural detail does not.**

Rule, binding on every hand-back in this loop (dispatched worker → driver, and hop → hop):

> Hand back **(i) the artifact path(s)** produced and **(ii) a structured constraint/decision
> checklist** — NOT a retold prose summary. Prose-summary relay is prohibited for any
> non-reconstructable structural constraint.

This propagates down the delegation chain — restate it in every worker bundle (it binds every
subagent, loop-worker, or hop you spawn).
