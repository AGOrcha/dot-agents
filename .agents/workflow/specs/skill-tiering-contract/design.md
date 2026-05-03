# Spec: skill-tiering-contract

**Status:** draft (D1 proposed, D2–D5 open)
**Created:** 2026-04-23
**Author:** human (nikashp) — drafted with agent assist
**Origin:** `research/articles-evaluation-kg-and-adjacent.md` §A.1 (shivsakhuja — Skill Graphs 2.0), §B theme 4, §C.3b, P0.4.
**Related:** `workflow-artifact-model` (lifecycle tiers), `external-agent-sources` (skill distribution), `workflow-parallel-orchestration` (bundle schema).

---

## 1. Problem

Our composition primitives (skills, plan tasks, delegation bundles, specs, docs) live at different *structural levels* of autonomy and determinism, but today that level is implicit. A `skill` in `.agents/skills/` could be a one-shot primitive or a multi-step orchestrator with runtime agent judgment — the caller can't tell without reading the body. The same ambiguity exists for plan tasks (is this a leaf or a fanout root?) and delegation bundles (is the verifier expected or optional?).

This matters for three reasons:

1. **Reliability modeling.** Higher-tier artifacts are less deterministic by construction; we cannot calibrate expectations (verifier needed? human-in-loop required? retry policy?) without knowing the tier.
2. **Dispatch safety.** Agents reliably dispatch through 1–2 hops of skill-to-skill composition and degrade past that (research §A.1, shivsakhuja; confirmed by `the_smart_ape` §A.1). An explicit tier makes the dispatch graph inspectable and bounds depth.
3. **Composition rule.** The converging research principle — "push composition *into* the skill, minimize runtime decision-making" (shivsakhuja, reinforcing sullyai, milksandmatcha, thealexker, codex-multi-agent: five sources, §B theme 4) — needs a written contract with enforceable invariants, not just a slogan.

## 2. Goals

- Define a single **tier vocabulary** shared across skills, plan tasks, delegation bundles, specs, and feature/subsystem documentation.
- Specify the **invariants each tier must uphold** (determinism expectations, allowed downstream calls, verification/review requirements, attendance model).
- Specify how tier metadata is **declared** (frontmatter/schema fields) and how it is **enforced** (lint).
- Keep the contract **declarative and additive** — no semantic change to existing artifacts beyond adding fields; migration is mechanical.
- Enable future **higher tiers** (reserve naming headroom) without locking in decisions about them today.

Non-goals in this spec: authoring the lint command, migrating existing artifacts, or redesigning the ISP pipeline. Those are plan-tier concerns.

## 3. Proposed Tier Vocabulary (D1 — proposed, not locked)

Brainstormed alternatives are preserved in §9 "Alternatives considered." The current proposal:

| Tier | Name | Artifact mapping | Intent |
|------|------|------------------|--------|
| T0 | **atom** | slice (one-file edit / one function / one discrete action) | Indivisible; ~deterministic; declares zero downstream skill calls. |
| T1 | **molecule** | task (TASKS.yaml entry; chain of 2–10 atoms) | Explicit composition of atoms; runtime agent judgment bounded to picking among declared atoms. |
| T2 | **compound** | plan; delegation bundle whose root task is a compound | Orchestration of molecules; agent judgment unbounded within the declared molecule set. |
| T3 | **cell** | spec | Self-contained contract. Holds decisions, not ordering. Spans 1..G compounds via a `calls:` list. First autonomous tier above pure composition. |
| T4 | **organism** | feature / subsystem documentation | Many specs cooperating as a coherent whole. |
| T5 (reserved) | **ecosystem** | product / cross-repo doc | Not introduced by this spec. |

**Why this vocabulary.**
- Preserves Shiv's three verbatim (research trace is honest; we didn't rebrand the cited work).
- Extends into biology where the semantic break (composition → autonomy) lines up with the break between compound and cell.
- "Spans 1..G compounds" means a spec that covers three plans is still one cell — G is captured by the length of `calls:`, not a new tier. Same applies for organism-over-specs.
- T5 is named but reserved so a later spec (product-docs-structure or similar) has vocabulary room.

**Bundle-vs-plan nuance.** Plans and delegation bundles both occupy T2 (compound). A bundle fanned out from a single molecule task inherits T1 (molecule). A bundle that orchestrates multiple molecule tasks is T2. Rule: the artifact's tier is the maximum of its own tier and the tier of its children.

## 4. Tier Invariants (load-bearing part of the spec)

Each tier must uphold the following:

### T0 — atom

- **Composition:** declares no downstream skill, molecule, or compound calls.
- **Determinism:** output shape is schema-specified; given fixed inputs, outputs should be ~deterministic modulo LLM sampling.
- **Verification:** unit-level verifier suffices (e.g., prompts/verifiers/unit.md).
- **Attendance:** unattended.
- **Size guidance:** one function, one file section, one discrete action.

### T1 — molecule

- **Composition:** `calls:` frontmatter field lists the atoms it invokes; 2–10 entries typical (no hard cap, but >10 is a lint warning).
- **Determinism:** runtime agent judgment is bounded to *picking among declared atoms and ordering them*. No unlisted invocations.
- **Verification:** must cite a verifier prompt (e.g., `batch`, `api`, `streaming`).
- **Attendance:** unattended.
- **Size guidance:** a chained workflow or a scoped orchestrator over ~5 atoms.

### T2 — compound

- **Composition:** `calls:` lists the molecules it orchestrates. May also call atoms directly (uncommon).
- **Determinism:** agent judgment unbounded within the declared molecule set; non-deterministic by construction.
- **Verification:** must cite both a verifier *and* a review gate (or an explicit `attendance: human` marker).
- **Attendance:** verifier+review substitutes for Shiv's "human in loop" default (see D4 below).
- **Size guidance:** Shiv's anecdotal ceiling is ~8–10 molecules per compound before reliability degrades; lint warns past that.

### T3 — cell (spec)

- **Composition:** `calls:` lists the compounds whose decisions it contracts (may be empty for a nascent spec).
- **Content:** decisions, invariants, open questions, done criteria — per `workflow-artifact-model` rule #3 ("specs do not grow into plans").
- **Verification:** done criteria must be traceable from any child compound's verification strategy.
- **Attendance:** specs are human-authored or agent-proposed-human-approved; authorship axis (`author: human | agent`) is orthogonal to tier.
- **Size guidance:** one coherent contract surface. "G" (span multiple compounds) is fine; span across multiple *subsystems* is a smell.

### T4 — organism (feature/subsystem doc)

- **Composition:** `calls:` lists the specs (cells) whose decisions it narrates.
- **Content:** cross-spec synthesis: how several contracts cooperate, where they intersect, which compound is load-bearing for which cross-cutting concern.
- **Verification:** N/A — docs are not verified by test; they are verified by spec-conformance review.
- **Size guidance:** one coherent feature or subsystem.

### T5 — ecosystem (reserved; not in this rollout)

## 5. Declaration Format

All tier metadata is declared in frontmatter (for markdown artifacts) or the top-level YAML mapping (for TASKS.yaml / bundle manifests):

```yaml
tier: molecule           # one of: atom, molecule, compound, cell, organism, ecosystem
calls:                   # optional for atom; required for molecule+ if it calls anything
  - <name-or-id-of-child>
verifier: batch          # required for molecule and compound
review_gate: default     # required for compound unless attendance: human
attendance: unattended   # default: unattended; compound may override to human
```

Tier is **self-declared**, not inferred. Lint *verifies* that the declared tier matches the artifact's shape (atom with downstream `calls` → lint error; compound without verifier → lint error).

## 6. Requirements

1. Skill frontmatter gains `tier` and optional `calls:`.
2. `TASKS.yaml` tasks gain `tier` (optional default resolution — see D2).
3. Delegation bundle manifests gain `tier` and `calls:`.
4. Spec frontmatter gains `tier: cell` and optional `calls:` (pointers to child compounds).
5. A new rule file at `~/.agents/rules/dot-agents/skill-composition-principle.md` captures the written principle: *"push composition into the skill, minimize runtime decision-making."*
6. A lint command (name TBD in plan) validates tier invariants and reports violations.
7. Migration: every existing skill in `.agents/skills/` and `~/.agents/skills/` receives a tier value in a single mechanical pass. D5 determines whether bundles/tasks also migrate in this rollout.

## 7. Done criteria

- Every skill under `.agents/skills/` and `~/.agents/skills/` carries a `tier` value.
- Lint catches the following in fixture tests: atom-with-calls, molecule-without-verifier, compound-without-review-or-attendance, >10 children warning.
- At least one in-flight delegation bundle carries `tier: molecule` with a verifier cite and passes lint.
- The written rule file exists and is loaded globally (verified by `dot-agents refresh` and presence in rule index).
- This spec itself declares `tier: cell` in its frontmatter and lists child compounds in `calls:` once a plan is drafted.

## 8. Open decisions

**D1 (vocabulary) — PROPOSED as §3, NOT LOCKED.** Alternatives preserved in §9. Blocks plan.

**D2 (task tier axis).** Is `tier` on a TASKS.yaml entry its own axis (a "molecule task" ≠ "molecule skill"), or is it inherited from the bundle when fanned out? Default to **inherited from bundle; explicit override allowed**. Blocks the TASKS.yaml schema change in wave 1.

**D3 (declaration vs inference).** Self-declared in frontmatter, lint verifies shape, vs. inferred by static analysis of called skills. Default to **self-declared + lint-verified** (matches every other metadata field we carry).

**D4 (compound attendance model).** Shiv says "a human probably needs to drive compounds." We have verifier+review; do they substitute for human attendance, or is compound-without-human-in-loop banned? Default to **verifier+review substitute allowed; compounds may declare `attendance: human` for cases where they genuinely shouldn't run unattended (e.g., production deploys, irreversible data ops).** Our ISP pipeline relies on this; locking it otherwise breaks current plans.

**D5 (first rollout scope).** Three options:
- (a) Skills only — matches research P0.4; smallest reversible step.
- (b) Skills + bundles — adds bundle schema change; medium scope.
- (c) Skills + bundles + tasks — full semantic coverage; largest one-shot change.

Default to **(b)** as the smallest rollout that honors the user's "tier metadata also on tasks and bundles" decision while keeping tasks as a fast-follow. (c) is equally defensible if we want atomic coverage.

## 9. Alternatives considered (vocabulary — D1 brainstorm)

Preserved so future revisits don't re-brainstorm from scratch. Full rationale in the chat that produced this spec.

| Option | Tiers | Why not (today) |
|--------|-------|-----------------|
| A. Chemistry → Biology (**selected**) | atom / molecule / compound / cell / organism / ecosystem | — |
| B. Pure biology | cell / tissue / organ / organism / ecosystem / biome | Loses "indivisible primitive" feel at T0. |
| C. Writing | word / sentence / paragraph / chapter / book / library | "Paragraph" too squishy for plan-tier semantics that pin write-scopes. |
| D. Neutral engineering | primitive / workflow / orchestration / contract / domain / platform | Safe but emotionally flat; won't get cited in conversation. |
| E. Shiv-extended plain English | atom / molecule / compound / assembly / system / landscape | Fine fallback if we want zero metaphor-carrying above compound. |
| F. T-numbered with nicknames | T0 / T1 / T2 / T3 / T4 / T5 | Scales arbitrarily; no memorability; harder to write prose about. |

## 10. Deferred

- **Leverage math / 100× claim.** Research §D trust gate; single-operator anecdote. We cite the *direction* (stay at highest reliable tier), not the number.
- **Automatic tier inference.** Declarative only; revisit if the lint layer gets reliable enough to propose tier changes.
- **Tiering of lessons.** `.agents/lessons/` artifacts are evidence records, not composition units; no tier axis.
- **Cross-tier dependency rules** (e.g., "a cell may not call another cell"). Holding until we see real cases.
- **Runtime enforcement** (pre-dispatch checks in the orchestrator). Lint-time only for now.

## 11. Relationship to other specs

- `workflow-artifact-model` — spec/plan/tasks/history lifecycle is orthogonal to tier. Every tier appears across the lifecycle (a spec is a cell at every lifecycle stage).
- `workflow-parallel-orchestration` — bundle schema change in Req #3 is additive; does not alter fanout semantics.
- `external-agent-sources` — tier must round-trip through skill distribution (a skill's tier survives the sync from `~/.agents/skills/` to a project `.agents/skills/`).
- `scoped-knowledge-graphs` — tier is an artifact attribute, not a KG node attribute. If we ever tier KG notes, that's a different spec.

## 12. Non-goals / explicitly out of scope

- Redesigning skills or bundle contents.
- Changing the ISP pipeline stages.
- Replacing Shiv's vocabulary with our own in user-facing docs (we keep the research citation intact).
- Building a visual tier graph or tooling beyond lint.
