---
name: "ideation-cycle"
description: "Fork-resolution loop: turn a HARD or OPEN design fork into a ratified, fidelity-audited decision + evidence. Use when a briefing alone can't decide a fork — an under-specified spec decision, a deferred architectural question, a recurring 'we keep re-explaining this', or a [PROPOSED] item — and you need to DISCOVER the answer: empirical prototype (under the prototype-experiment-fidelity-gate) → independent cross-harness audit → cross-brain the judgment calls → ratified decision. Composable compound: it RUNS kg-brief for grounding and is dispatchable from kg-ideate Phase 2 (spec-scaffold) for hard forks, AND independently invocable for a one-off design question. It RETURNS the ratified decision + evidence sidecar; spec-scaffold writes the spec prose. Not the authoring front-end (that is kg-ideate) and not implementation (that is isp)."
argument-hint: "[<fork-or-spec-id> | --spec <path> | --question \"<open design question>\" | --brief <briefing-path>]"
# tier: compound. ideation-cycle orchestrates delegated workers (prototype authors, the
# independent cross-harness auditor, the cross-brain reviewer) with UNBOUNDED judgment over
# which to run and how to weigh them — that is a compound, not a bounded molecule.
tier: compound
# Composition is governed by judgment-autonomy, NOT tier-adjacency: a compound MAY call
# molecules/compounds (refined tiering delta:
# .agents/proposals/skill-tiering-molecule-composition.md). NOTE: the tiering contract's old
# "fidelity degrades past depth ~2–3 hops" premise did NOT replicate (sidecar v1–v3). v4 found a
# NARROW drift on error-prone COMPOUNDING work (not hop depth), GATE-2-audited NOT-SOUND for the
# broad claim; mechanism deferred to v5. The real, evidence-backed bounds are: infra
# delegation-nesting ceiling (~hop 4), RELAY DISCIPLINE (structured/pointer hand-backs, never
# retold prose), and DECOMPOSING error-prone compounding chains into verifiable sub-artifacts.
# These bounds are CURRENT-HARNESS-OBSERVED (Claude Code Agent tool + codex), NOT universal — the
# numbers are per-harness, re-assess as the harness set grows (Hermes, Pi-agent, Aider, …).
# kg-brief is RUN by ideation-cycle as step 1 (reuse-by-artifact when fresh, else re-run).
calls:
  - kg-brief        # grounding step 1 (reuse-by-artifact when fresh, else re-run — see below)
  - enumerate-forks
  - classify-forks
  - empirical-pass
  - fidelity-gate
  - cross-brain
  - converge-decision
  - dogfood-decision
# Deep multi-hop delegation must be DRIVER-ORCHESTRATED hop-by-hop (fresh Agent per hop, relay
# via on-disk artifact), NOT recursively nested — nested Agent-tool delegation collapses past
# ~hop 4 on the CURRENT harness (reproduced; sidecar v2/v3; re-assess per harness). Reuse of an
# upstream briefing is gated on an inputs_digest + dependency-manifest freshness check
# (see instructions/ground-via-kg-brief.md).
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
| **idea→spec fork-resolution** | **`ideation-cycle`** (T2 compound) | grounded idea → ratified decision + evidence sidecar (it does NOT write the spec prose) |
| Execution | **`[[isp]]` / `[[orchestrator-session-start]]`** | how & in what order: staged impl → verify → review |

`kg-ideate` owns the whole pipeline; `ideation-cycle` owns the idea→spec **fork-resolution**
(the evolved, fidelity-gated form of that transition). It **RETURNS** ratified decisions + a
per-fork evidence sidecar; **`spec-scaffold` writes the spec prose**, then `kg-ideate` Phases
3–4 (`plan-scaffold`, handoff) continue unchanged. See `instructions/composition.md` for the
segment boundary, the relay-discipline hand-back rule, and the handoff. Canonical
formalization: `.agents/workflow/specs/ideation-system-composition/design.md` (DRAFT — owner-ruled,
pending human ratification; no longer blocked on v4 — v4 folded narrow, v5 deferred; the
depth/compounding claims rest on the evidence sidecar).

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

1. **Ground — run `kg-brief` (reuse upstream briefing only if fresh)**
   Load → `instructions/ground-via-kg-brief.md`
   `ideation-cycle` RUNS `kg-brief` as its grounding step (KG / research / lessons traversal
   → the shared briefing block). When dispatched from `kg-ideate`, the Phase 1 briefing may
   be reused BY ARTIFACT — but only if it is FRESH: its `inputs_digest` (over a concrete input
   set: idea text + KG snapshot id + named-query results + applicable-lessons set + cited-artifact
   hashes) still matches, AND no entry in the brief's **dependency manifest** (the KG nodes /
   decisions / lessons it read) changed. On any digest mismatch or manifest change, RE-RUN
   `kg-brief` — a stale brief must never silently propagate. Standalone runs always run
   `kg-brief` fresh. Either way we never reinvent a separate baseline scan.

2. **Enumerate the forks**
   Load → `instructions/enumerate-forks.md`
   From the briefing, list the open design questions and deferred forks, each framed
   against the baseline (the prior decision it extends, the gap it fills). When dispatched
   from `kg-ideate` for a single decision, the fork is already named — enumerate only its
   sub-forks.

3. **Classify each fork (triage — with a guard)**
   Load → `instructions/classify-forks.md`
   Tag every fork: already-settled (prior / dogfood) · empirically-determinable
   (prototype) · judgment-call (cross-brain) · owner-decision · deferred. The tag routes
   the fork to step 4, step 5, or straight to step 6. **Triage guard:** a "briefing-decidable
   / already-settled" verdict MUST cite the decisive briefing fact; if none is citable, the
   fork defaults to HARD. The step-5 cross-brain pass also reviews the triage calls themselves,
   so a fork cannot be waved past the gate by mislabeling it easy.

4. **Empirical pass — under the fidelity gate**
   Load → `instructions/empirical-pass.md`
   THEN Load → `instructions/fidelity-gate.md` (load-bearing — read before trusting any
   result). For each empirically-determinable fork, dispatch a SELF-CONTAINED prototype
   (own `go.mod`, isolated from the coverage gate) that runs the REAL scenario and asserts
   the hypotheses, then run it through the fidelity gate (faithful inputs + negative
   control + real execution + self-audit + **independent cross-harness audit**). Only an
   audited-sound experiment is allowed to inform the decision. **Relay discipline:** every
   hand-back from a dispatched worker (and hop→hop in any multi-hop chain) MUST be
   structured/pointer-based — artifact path + a constraint/decision checklist — never retold
   prose. Lossy summary relay drops non-reconstructable detail that reaches the deliverable
   (evidence sidecar v3 family-2: 16→13). Deep delegation is driver-orchestrated hop-by-hop,
   not recursively nested (nested `Agent` delegation collapses past ~hop 4 on the current
   harness; the ceiling is per-harness, re-assess as the harness set grows).

5. **Cross-brain the judgment calls**
   Load → `instructions/cross-brain.md`
   For each non-empirical fork, get a cross-harness (codex) adversarial opinion. Produce a
   ranked list of must-decide items, each with a recommended default and its rationale.

6. **Converge — ratify the decision, RETURN it (don't write the spec)**
   Load → `instructions/converge.md`
   The owner ratifies. `ideation-cycle` produces a **ratified decision + a per-fork evidence
   sidecar** — it does **not** type the spec file. Dispatched from `kg-ideate`: return the
   decision + sidecar pointer; **`spec-scaffold` writes the spec prose**. Standalone: hand the
   decision + sidecar to a spec-drafting step (a delegated `spec-scaffold`-equivalent) to write
   the prose, per the `workflow-artifact-model` rule. Evidence is linked from the decision, not
   inlined.

7. **Dogfood / migration**
   Load → `instructions/dogfood.md`
   Validate the ratified decision against real data (e.g. a zero-diff migration on the
   live corpus). Close the loop: surface any gap as a new fork (back to step 2) or a
   fold-back, and capture lessons.

## Composition

Load → `instructions/composition.md` — the role-split with `kg-ideate`, how the shared
`kg-brief` grounding is reused-by-artifact-when-fresh (and re-run on staleness), the segment
boundary (kg-ideate Phase 2 autonomous triage → hard forks run the loop), the relay-discipline
hand-back rule, the two invocation modes (dispatched vs standalone), and the engineering bounds
(infra nesting ceiling + relay discipline — NOT a measured fidelity cliff; see the evidence
sidecar). Read this whenever invoked from `kg-ideate`.

## Gotchas

Load → `instructions/gotchas.md` — read the failure points (especially "a green prototype
can prove the wrong thing", "don't reinvent kg-brief grounding", and "don't turn the
ideation driver into the prototype author") **before** the empirical pass.
