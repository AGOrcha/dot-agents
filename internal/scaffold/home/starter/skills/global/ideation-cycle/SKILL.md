---
name: "ideation-cycle"
description: "Fork-resolution engine: turn a HARD or OPEN design fork into a ratified, fidelity-audited decision + evidence. Use when a briefing alone can't decide a fork — an under-specified spec decision, a deferred architectural question, a recurring 'we keep re-explaining this', or a [PROPOSED] item — and you need to DISCOVER the answer: empirical prototype (under the prototype-experiment-fidelity-gate) → independent cross-harness audit → cross-brain the judgment calls → ratified decision. Composable molecule: it REUSES kg-brief for grounding and is dispatchable from kg-ideate Phase 2 (spec-scaffold) for hard forks, AND independently invocable for a one-off design question. Not the authoring front-end (that is kg-ideate) and not implementation (that is isp)."
argument-hint: "[<fork-or-spec-id> | --spec <path> | --question \"<open design question>\" | --brief <briefing-path>]"
tier: molecule
calls:
  - enumerate-forks
  - classify-forks
  - empirical-pass
  - fidelity-gate
  - cross-brain
  - converge-decision
  - dogfood-decision
reuses:
  - kg-brief        # the grounding stage IS kg-brief — not a reinvented baseline scan
dispatchable_from:
  - kg-ideate       # invoked at Phase 2 (spec-scaffold) when a decision is a HARD/OPEN fork
verifier: batch
review_gate: default
---

# Ideation Cycle — the fork-resolution engine

`ideation-cycle` is the **matured form of `kg-ideate`'s idea→spec stage** — the rigorous
way to go from a grounded idea/proposal to a **ratified, fidelity-audited spec decision**.
It is NOT a sibling engine `kg-ideate` happens to dispatch; it is the **evolution of the
idea→spec segment** at the front of `kg-ideate`'s lifecycle, where hard/open forks
concentrate. The loop: ground (`kg-brief`) → enumerate forks → classify → empirical
prototype (under the fidelity gate) → independent cross-harness audit → cross-brain the
judgment calls → ratified decision folded back into the spec.

It is **not** the whole authoring front-end and **not** implementation:

| Role | Skill | Owns |
|------|-------|------|
| Authoring front-end | **`kg-ideate`** (T2 compound) | the WHOLE pipeline: idea → spec → plan → staged-execution handoff |
| **idea→spec segment** | **`ideation-cycle`** (this molecule) | how idea→spec is now done: grounded idea → ratified spec + evidence |
| Execution | **`[[isp]]` / `[[orchestrator-session-start]]`** | how & in what order: staged impl → verify → review |

`kg-ideate` owns the whole pipeline; `ideation-cycle` owns its idea→spec SEGMENT (Phase 1
`kg-brief` + Phase 2 `spec-scaffold`, evolved into rigor). `kg-ideate` Phases 3–4
(`plan-scaffold`, handoff) are unchanged — control returns to `kg-ideate` with a ratified
spec in hand. See `instructions/composition.md` for the segment boundary and the handoff.
Canonical formalization:
`.agents/workflow/specs/ideation-system-composition/design.md`.

## Operating posture (read before starting)

- **Empirical where possible, else cross-brain.** A fork a prototype can decide gets a
  prototype (under the fidelity gate). A fork that is a judgment call gets a cross-harness
  adversarial opinion. Never settle a determinable fork by opinion; never fake-empiricize
  a judgment call.
- **The fidelity gate is mandatory for any prototype-based claim.** No prototype result
  informs the decision until it passes the gate in step 4. See lesson
  `prototype-experiment-fidelity-gate` and `instructions/fidelity-gate.md`.
- **Dispatch the authoring; orchestrate, don't hand-build.** The ideation driver scopes,
  steers, and audits. Prototypes and spec drafts are authored by dispatched subagents
  (Hybrid orchestration model). The driver's leverage is framing the forks and running
  the gates, not typing the experiment.

## Workflow

1. **Ground — reuse `kg-brief`, don't reinvent it**
   Load → `instructions/ground-via-kg-brief.md`
   The grounding stage **IS** the `kg-brief` molecule (KG / research / lessons traversal →
   the shared briefing block). When dispatched from `kg-ideate`, the Phase 1 briefing
   already exists — consume it, do not re-run grounding. When invoked standalone, invoke
   `kg-brief` (or run it inline) to produce that same briefing first. Either way, the fork
   carries the prior thinking and we never reinvent a separate baseline scan.

2. **Enumerate the forks**
   Load → `instructions/enumerate-forks.md`
   From the briefing, list the open design questions and deferred forks, each framed
   against the baseline (the prior decision it extends, the gap it fills). When dispatched
   from `kg-ideate` for a single decision, the fork is already named — enumerate only its
   sub-forks.

3. **Classify each fork**
   Load → `instructions/classify-forks.md`
   Tag every fork: already-settled (prior / dogfood) · empirically-determinable
   (prototype) · judgment-call (cross-brain) · owner-decision · deferred. The tag routes
   the fork to step 4, step 5, or straight to step 6.

4. **Empirical pass — under the fidelity gate**
   Load → `instructions/empirical-pass.md`
   THEN Load → `instructions/fidelity-gate.md` (load-bearing — read before trusting any
   result). For each empirically-determinable fork, dispatch a SELF-CONTAINED prototype
   (own `go.mod`, isolated from the coverage gate) that runs the REAL scenario and asserts
   the hypotheses, then run it through the fidelity gate (faithful inputs + negative
   control + real execution + self-audit + **independent cross-harness audit**). Only an
   audited-sound experiment is allowed to inform the decision.

5. **Cross-brain the judgment calls**
   Load → `instructions/cross-brain.md`
   For each non-empirical fork, get a cross-harness (codex) adversarial opinion. Produce a
   ranked list of must-decide items, each with a recommended default and its rationale.

6. **Converge — ratify the decision**
   Load → `instructions/converge.md`
   The owner ratifies. Standalone: seed or refine the canonical spec per the
   `workflow-artifact-model` rule (decisions + rationale + open questions + done-criteria;
   the prototype artifacts and audits are the recorded evidence). Dispatched from
   `kg-ideate`: return the ratified decision + evidence so Phase 2 (`spec-scaffold`) folds
   it back into the spec it is authoring — `ideation-cycle` does not own that spec, it
   resolves one fork inside it.

7. **Dogfood / migration**
   Load → `instructions/dogfood.md`
   Validate the ratified decision against real data (e.g. a zero-diff migration on the
   live corpus). Close the loop: surface any gap as a new fork (back to step 2) or a
   fold-back, and capture lessons.

## Composition

Load → `instructions/composition.md` — the role-split with `kg-ideate`, how the shared
`kg-brief` grounding is reused, the dispatch boundary (kg-ideate Phase 2 triage → dispatch
hard forks here), the two invocation modes (dispatched vs standalone), and the
dispatch-hop note. Read this whenever invoked from `kg-ideate`.

## Gotchas

Load → `instructions/gotchas.md` — read the failure points (especially "a green prototype
can prove the wrong thing", "don't reinvent kg-brief grounding", and "don't turn the
ideation driver into the prototype author") **before** the empirical pass.
