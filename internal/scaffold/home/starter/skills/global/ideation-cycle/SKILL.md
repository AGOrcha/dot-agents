---
name: "ideation-cycle"
description: "The ideation/refinement half of the meta-loop. Use when an open design FORK needs to become a ratified, fidelity-audited spec decision: an under-specified spec, a deferred architectural question, a recurring 'we keep re-explaining this', or a [PROPOSED] item that has to be settled before implementation can start. Drives ground-in-baseline → enumerate forks → classify → empirical pass (under the fidelity gate) → cross-brain the judgment calls → converge/ratify → dogfood. Pairs with isp/orchestrator-session-start (the execution half): ideation produces the spec/proposal, execution implements it. Not for implementing an already-decided spec — that is isp."
argument-hint: "[<fork-or-spec-id> | --spec <path> | --question \"<open design question>\"]"
---

# Ideation Cycle — the fidelity-gated design-refinement loop

This is the **ideation half** of the meta-loop. Its job is to turn an open design
**fork** — an under-specified spec, a deferred architectural question, a "we keep
re-explaining this" — into a **ratified, fidelity-audited spec decision** that the
execution half (`isp` / `orchestrator-session-start`) can implement without guessing.

It does not write product code. It produces (or refines) the canonical **spec**
(`workflow/specs/<id>/design.md`) plus the **evidence** behind each decision: audited
prototype experiments and cross-brain opinions. The spec is the contract; execution is
accountable to it (`workflow-artifact-model` rule).

## Operating posture (read before starting)

- **Empirical where possible, else cross-brain.** A fork that a prototype can decide
  gets a prototype (under the fidelity gate). A fork that is a judgment call gets a
  cross-harness adversarial opinion. Never settle a determinable fork by opinion, and
  never fake-empiricize a judgment call.
- **The fidelity gate is mandatory for any prototype-based claim.** No prototype result
  informs the spec until it passes the gate in step 4. See lesson
  `prototype-experiment-fidelity-gate`.
- **Dispatch the authoring; orchestrate, don't hand-build.** The ideation driver scopes,
  steers, and audits. Prototypes and spec drafts are authored by dispatched subagents
  (Hybrid orchestration model). The driver's leverage is in framing the forks and
  running the gates, not in typing the experiment.

## Workflow

1. **Ground in the baseline**
   Load → `instructions/ground-in-baseline.md`
   Search existing proposals, specs, plans, lessons, and schemas FIRST so every fork
   carries the prior thinking. Cite what already exists vs what is open / `[PROPOSED]`.
   Do not reinvent a decision that is already settled in the repo.

2. **Enumerate the forks**
   Load → `instructions/enumerate-forks.md`
   List the open design questions and deferred forks, each framed against the baseline
   from step 1 (the prior decision it extends, the gap it fills).

3. **Classify each fork**
   Load → `instructions/classify-forks.md`
   Tag every fork: already-settled (prior / dogfood) · empirically-determinable
   (prototype) · judgment-call (cross-brain) · owner-decision · deferred. The tag
   routes the fork to step 4, step 6, or straight to step 7.

4. **Empirical pass — under the fidelity gate**
   Load → `instructions/empirical-pass.md`
   THEN Load → `instructions/fidelity-gate.md` (load-bearing — read before trusting any
   result). For each empirically-determinable fork, dispatch a SELF-CONTAINED prototype
   (own `go.mod`, isolated from the coverage gate) that runs the REAL scenario and
   asserts the hypotheses, then run it through the fidelity gate (faithful inputs +
   negative control + real execution + self-audit + independent cross-harness audit).
   Only an audited-sound experiment is allowed to inform the spec.

5. **Cross-brain the judgment calls**
   Load → `instructions/cross-brain.md`
   For each non-empirical fork, get a cross-harness (codex) adversarial opinion. Produce
   a ranked list of must-decide items, each with a recommended default and its rationale.

6. **Converge — ratify and seed the spec**
   Load → `instructions/converge.md`
   The owner ratifies the decisions. Seed or refine the canonical spec per the
   `workflow-artifact-model` rule: spec owns what/why + each decision and its rationale +
   remaining open questions + done-criteria. The prototype artifacts and audits are the
   recorded evidence. Hand the spec to the execution half (`isp`).

7. **Dogfood / migration**
   Load → `instructions/dogfood.md`
   Validate the ratified decision against real data (e.g. a zero-diff migration on the
   live corpus). Close the loop: surface any gap as a new fork (back to step 2) or a
   fold-back, and capture lessons.

## Gotchas

Load → `instructions/gotchas.md` — read the failure points (especially "a green
prototype can prove the wrong thing" and "don't turn the ideation driver into the
prototype author") **before** the empirical pass.

## Pairing with the execution half

```
ideation-cycle  (this skill)   →  ratified spec + evidence   →  isp / orchestrator-session-start
   what & why, decisions,                                          how & in what order,
   audited evidence                                                staged impl → verify → review
```

A fork that comes out of execution under-specified comes back here. A spec that comes
out of here ratified goes there. Do not collapse the two halves.
