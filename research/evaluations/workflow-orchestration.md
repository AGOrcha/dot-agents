# Articles Evaluation — Workflow Orchestration

**Written:** 2026-04-21
**Scope:** How the articles in `research/articles/` speak to dot-agents' workflow layer — the `workflow/specs/` → `workflow/plans/` → `TASKS.yaml` → `active/` → `history/` lifecycle, fanout and dep-graph mechanics, conflict detection, ISP pipeline, and the Research → Plan → Implement discipline.
**Siblings:** `articles-evaluation-kg-and-adjacent.md`, plus the four other non-KG eval docs in this directory.

**Per-article rubric:** Core / Pros / Cons / Risk profile / Mapping.

- *Risk profile* is four compact lenses:
  - **Failure mode** — what does it look like when this breaks in practice?
  - **Evidence** — measured (benchmarks), anecdotal (one operator's report), structural (self-evident from the idea).
  - **Reversibility** — easy / moderate / painful if we adopt and it turns out wrong.
  - **Second-order** — downstream patterns this encourages or blocks.
- *Mapping label* is one of:
  - **[OVERLAP-SHARPEN]** — we do it, they do it better in a way to learn from.
  - **[GAP-ADOPT]** — we don't do it, worth adding.
  - **[WE-AHEAD]** — we do it better, but a quirk is worth noting.

---

## Part A — Per-article evaluation

### thealexker — *Harnesses Are Everything (R.P.I.)*

**Core.** Research → Plan → Implement as the load-bearing shape of any agent task. "Research" produces artifacts an agent can reason over; "Plan" commits to a shape; "Implement" is the cheap step once the other two are done. Harnesses (tool surfaces, rules, outputs) define what the agent can even see.

**Pros.**
- Names the shape of our own spec → plan → task pipeline with less jargon. The R.P.I. framing is a good communication aid for new contributors.
- Treating implementation as the cheap step validates our "plans are load-bearing, tasks are mechanical" stance.

**Cons.**
- R.P.I. doesn't distinguish research-for-design from research-for-verification. Our stack splits these (specs vs verifier prompts); collapsing them is a regression.
- The post is a meta-claim; concrete examples are thin.

**Risk profile.**
- *Failure mode:* skipped Research phase produces plans that resolve open questions with guesses. Silent — the plan looks complete.
- *Evidence:* structural + anecdotal.
- *Reversibility:* easy (it's a rule, not infrastructure).
- *Second-order:* encourages spec authors to surface open questions explicitly; discourages jumping from idea straight to PLAN.yaml.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our `workflow-artifact-model.md` rule says "Spec before plan." Add R.P.I. as its explicit naming and a reminder that the spec *is* the Research artifact. One-line rule update, not a new primitive.

---

### sullyai — *Pipelines Are Slow When Agents Do Too Much*

**Core.** The fix for a slow agent pipeline is almost never "prompt harder." It's context engineering: if an agent is iterating to correct itself, the upstream decomposition/context has failed. Context engineering and iteration are substitutes, not complements.

**Pros.**
- Directly challenges the "add a verifier loop and it'll converge" anti-pattern. Our own ISP pipeline is vulnerable to this if verifier corrections become routine.
- The substitution framing is sharp: every iteration cycle is a signal that decomposition was wrong.

**Cons.**
- Doesn't give a threshold. One correction pass is normal; ten is obviously broken. Where's the line?
- Requires instrumentation we partly have (fold-back observations) but don't systematically analyze.

**Risk profile.**
- *Failure mode:* silent — a pipeline with a high iteration count looks productive; the team spends cycles fixing outputs instead of fixing decomposition. Productivity stays flat over quarters.
- *Evidence:* anecdotal but widely replicated across adjacent articles (milksandmatcha, thealexker).
- *Reversibility:* trivial (a review rule); the downstream work it produces (re-decomposing bundles) is more expensive.
- *Second-order:* forces plan authors to front-load write-scope and output contract decisions. Reduces loop-worker scope creep.

**Mapping.**
- **[GAP-ADOPT — P0]** — one-line rule in `self-review` or `iteration-close`: "if this work relied on a correction loop, first answer: is this compensating for overloaded context?" Same recommendation as the KG doc's C.3.
- **[GAP-ADOPT]** — instrument fold-back counts per plan. High fold-back per plan is a signal the plan was under-decomposed, not just that the work was hard.

---

### milksandmatcha — *Single-Agent AI Coding Nightmare (Kitchen Patterns)*

**Core.** Five patterns borrowed from restaurant kitchens: mise en place (front-load state), prep stations (decomposition), expediter (coordinator), parallel lines (independent tracks), taste-before-serve (verification). Single-agent iteration fails at any of these; multi-agent coordination succeeds when these shapes are explicit.

**Pros.**
- The kitchen analogy is a better teaching metaphor than "multi-agent swarm." It makes decomposition concrete: each agent has one station, not one task.
- Parallel lines maps cleanly onto our fanout; the article's insistence that the expediter sees the whole ticket matches our orchestrator's role.

**Cons.**
- Skips the question of *how* to decompose. Kitchens have centuries of received tradition on station layout; plan authors do not.
- "Taste-before-serve" is expensive if done per step; the article under-weights the cost.

**Risk profile.**
- *Failure mode:* missing expediter = two agents writing to overlapping files; missing mise en place = agents re-deriving context every call; missing taste = bad output ships.
- *Evidence:* anecdotal (one operator's kitchen framing) but internally consistent.
- *Reversibility:* easy (these are review rules and prompt templates).
- *Second-order:* encourages stable task shapes over ad-hoc decomposition. Can rigidify a team if the metaphor is taken too literally.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our orchestrator is the expediter; our delegation bundles are mise en place; our conflict-detector is the expediter's ticket-owner check. Articulate this mapping in `workflow-artifact-model.md` to make the roles legible.
- **[GAP-ADOPT — P2]** — parallel verification ("taste-before-serve as a parallel track, not a serial blocker"). Verifier runs concurrent with next impl; gated before fan-in. Reduces ISP wall-clock.

---

### arscontexta — *Six-R Pipeline (Record / Reduce / Reflect / Reweave / Verify / Rethink)*

**Core.** Six explicit phases, each spawning a fresh subagent. Record = capture raw input. Reduce = compress. Reflect = derive implications. Reweave = update *prior* artifacts with the new findings. Verify = check. Rethink = consider whether the plan itself changed. Context isolation per phase is the load-bearing property.

**Pros.**
- Reweave is the missing phase in most pipelines, including ours. Today when a plan completes, we write `impl-results.md` and move on; we don't walk backward to update prior plan docs that assumed the old state.
- Fresh-subagent-per-phase cleanly isolates context and prevents the earlier phases from contaminating the later ones.

**Cons.**
- Six phases is a lot of ceremony for a small change. Needs a "skip modes" rule.
- "Rethink" is genuinely hard to automate — it's the judgment phase.

**Risk profile.**
- *Failure mode:* skipped Reweave = silent drift; prior plans' decisions still cited even after their assumptions are invalidated. Looks fine in diffs; breaks at the next plan that reads the stale context.
- *Evidence:* pattern-based; arscontexta reports the shape works across projects but with no hard metric.
- *Reversibility:* moderate — Reweave as a phase requires a skill/prompt that doesn't exist yet. Rolling back = deleting the skill.
- *Second-order:* forces plans to declare which prior plans they update, which tightens the plan graph.

**Mapping.**
- **[GAP-ADOPT — P1]** — adopt Reweave as an explicit phase in `iteration-close` or as a standalone `/reweave` skill. When a plan completes, Reweave walks the plan graph backward, flags prior plans whose assumptions changed, and proposes `status: superseded` or `status: needs-review`.
- **[OVERLAP-SHARPEN]** — our existing `self-review` is Verify+Rethink collapsed. Worth splitting so Rethink gets its own prompt and isn't buried.
- **[WE-AHEAD]** — our spec/plan/tasks/history tiers are a cleaner artifact model than arscontexta's 6-phase-per-task flow. We should keep the tiers and adopt Reweave *between* plans, not within each task.

---

### intuitiveml — *AI-First Six-Phase CI/CD*

**Core.** `Verify CI → Build and Deploy Dev → Test Dev → Deploy Prod → Test Prod → Release`. Pipeline is deterministic with no manual overrides. The stated reason: agents need predictable outcomes to reason about failure. Three parallel Claude review passes (quality / security / deps) sit on every PR.

**Pros.**
- Deterministic + no-overrides is the property that makes agent-driven deploys tractable. Every override is a place where the agent's mental model diverges from reality.
- Three parallel review passes at fixed lenses is a stronger pattern than one "general" review.

**Cons.**
- CREAO is a 25-person product company with real production users; the pipeline shape is for their risk profile, not ours.
- "No manual overrides" is load-bearing but politically expensive; teams push back.

**Risk profile.**
- *Failure mode:* if an override exists, agents learn to request it; the deterministic property erodes. Rapid drift once the first exception is granted.
- *Evidence:* anecdotal (one company, published numbers: 3–8 deploys/day over 14 days).
- *Reversibility:* painful — once agents are trained to use overrides, retraining is a social/skill-file rewrite.
- *Second-order:* forces verification investment up-front (you can't skip the gate), which is healthy. But it also creates a single point of failure in the pipeline definition itself.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our loop-close / verifier / reviewer stages are already phase-structured. Make the phase ordering explicit in one place (today it's implicit in skill files).
- **[GAP-ADOPT — P2]** — adopt the "three parallel review passes at fixed lenses" pattern for review-heavy plans. Today `/review` is one generic pass; split into quality / security / dependency + arch lenses for PR-grade work. Composes with the planning-lenses idea from the KG doc (§C.7 there).
- **[WE-AHEAD]** — our spec/plan/history discipline is more structured than CREAO's ad-hoc Linear triage; we shouldn't adopt their flat ticket model.

---

### codex-multi-agent-swarms-playbook — *Front-loading Context*

**Core.** Effective swarms load context into the bundle up-front; poor swarms expect workers to discover context mid-task. The playbook gives a recipe for bundle prompts: stated goal, relevant files, failure modes already considered, output contract, verification criteria.

**Pros.**
- Exactly the shape of our delegation bundles. Validates the design and gives a concrete audit checklist for bundle prompts.
- "Failure modes already considered" is a field we don't explicitly have; it would reduce repeated dead-ends.

**Cons.**
- Playbook is tactical — doesn't engage the architectural question of when a bundle should exist at all.
- Output contract shape is under-specified; each task writes its own.

**Risk profile.**
- *Failure mode:* workers ask clarifying questions mid-task (expensive, breaks parallelism) OR guess, producing output that fails verification. Either is a signal of front-loading failure.
- *Evidence:* structural — the claim is self-evident for anyone who has written bundle prompts.
- *Reversibility:* trivial (template change).
- *Second-order:* encourages plan authors to resolve open questions before fanout. Exactly what we want.

**Mapping.**
- **[OVERLAP-SHARPEN]** — audit our bundle-prompt template against this checklist; specifically add a "known failure modes / anti-scope" block and a structured output contract section.
- **[GAP-ADOPT — P2]** — **dynamic output contracts on delegation bundles**: typed output schema per task; deterministic fan-in. Same recommendation as the KG doc's §C.9, and the place it most naturally lands is here.

---

### the_smart_ape — *Compound Mode*

**Core.** Open questions from one research project become the index of the next project. The research-skill-graph compounds: each session closes questions and opens new ones, and the open ones are the spec for the next cycle.

**Pros.**
- Maps perfectly onto our "spec open questions must be resolved in the plan, and leftover questions become fold-back / next-spec seeds" discipline.
- Gives a clean mental model for the lifecycle: open questions are the edges of the plan graph.

**Cons.**
- Requires open questions to be structured and searchable. Today ours are free-text in spec bodies.

**Risk profile.**
- *Failure mode:* open questions are buried in spec prose; the next cycle doesn't find them; the same question gets re-derived in a later plan. Silent drift.
- *Evidence:* pattern-based, reported from personal practice.
- *Reversibility:* easy — a frontmatter field and a lint rule.
- *Second-order:* surfacing open questions as first-class artifacts creates a backlog of things-to-figure-out, which is itself a planning input. Net positive.

**Mapping.**
- **[GAP-ADOPT — P1]** — add an `open_questions:` structured list to spec frontmatter (YAML). `dot-agents workflow` reads them and exposes via a `workflow open-questions` command that lists every open question across specs.
- **[OVERLAP-SHARPEN]** — our fold-back artifacts already play this role at the observation level. Elevate *spec* open questions to the same surface.

---

### witcheer — *Two Camps*

**Core.** Memory/context tooling divides into Camp 1 (memory-backend services: Mem0, Zep) and Camp 2 (context substrates: TrustGraph, MemSearch, Thoth). Camp 2 treats durable state as files + derived indexes + scheduled consolidation; Camp 1 treats it as a managed service.

**Pros.**
- Names the architectural lane we're already in. Gives us vocabulary for future proposals.
- Validates our decision to keep the warm store local and have markdown files be the source of truth.

**Cons.**
- The taxonomy is self-proclaimed; reality has hybrids.

**Risk profile.**
- *Failure mode:* none directly — it's a lens, not a primitive. The risk is missing it: future proposals drift toward Camp 1 (hosted services) without a principled reason.
- *Evidence:* structural, with existence-proofs on each side.
- *Reversibility:* N/A (it's a lens, not a change).
- *Second-order:* a named architectural lane lets us reject Camp 1 proposals quickly when they arrive.

**Mapping.**
- **[GAP-ADOPT — P0]** — add one paragraph to `workflow-artifact-model.md` (or a new `architectural-stance.md`) naming dot-agents as a Camp 2 system and why. Filters future proposals and orients new contributors.

---

## Part B — Synthesis against our stack

### Five patterns the workflow layer should internalize

1. **Spec-before-plan is Research-before-Plan.** R.P.I. (thealexker) + compound-mode (the_smart_ape) + spec/plan split (our own `workflow-artifact-model.md`) are the same idea at different abstraction levels.
2. **Iteration is a substitute for context engineering.** sullyai + milksandmatcha + codex-multi-agent-swarms all converge on this. Every correction loop is a decomposition audit. Our ISP pipeline should expose a per-plan iteration-count metric and treat high counts as decomposition failure.
3. **Reweave is the missing phase.** arscontexta's backward-pass update to prior artifacts has no analog in our pipeline. Today plans are closed and forgotten. Adding Reweave closes the loop on the plan graph the same way derivation-propagation (from the scoped-KG spec) closes the loop on the KG.
4. **Deterministic pipelines enable agent reasoning; overrides break it.** intuitiveml's deterministic-by-design principle applies to our skill/verifier/loop-close chain just as much as their CI/CD. Every `--force` flag erodes agent predictability.
5. **Front-loaded bundle context is non-negotiable.** codex-multi-agent-swarms gives the checklist; we should apply it to our bundle prompts explicitly.

### What we do well

- **Separated artifact tiers** (spec / plan / tasks / history) with explicit lifecycle and contract. Most articles collapse two or more tiers.
- **Deterministic fanout with dep graph + conflict detection.** No one else in this set has this.
- **Fold-back as a structured observation channel.** arscontexta's "session-capture" hook gestures at it; ours is more disciplined.

### What we miss (priority-ordered)

**P0 — rule / one-line changes:**
- Name dot-agents as a Camp 2 system in `workflow-artifact-model.md` (witcheer).
- Add the "correction-loop compensation check" rule to `self-review` / `iteration-close` (sullyai).

**P1 — small new primitives:**
- `open_questions:` structured list on specs + `workflow open-questions` command (the_smart_ape).
- Reweave phase at plan close (arscontexta) — a `/reweave` skill that walks the plan graph backward and flags superseded assumptions.

**P2 — structural:**
- Audit bundle-prompt template against the codex-multi-agent-swarms checklist; add anti-scope + output-contract sections.
- Dynamic output contracts on delegation bundles.
- Parallel verification (Gordon Ramsay pattern) — verifier runs concurrent with next impl.
- Three-lens parallel PR review (intuitiveml) for review-heavy plans.

### What we do better than them

- **vs. thealexker (R.P.I.):** our spec/plan/tasks/history lifecycle is more structured than R.P.I.'s three phases; R.P.I. is a good communication aid but not a replacement.
- **vs. arscontexta (6-phase):** our tiers > their 6-phase-per-task; adopt Reweave *between* plans, not inside every task.
- **vs. codex-multi-agent-swarms:** our conflict detection + dep graph are strictly more principled than their ad-hoc coordination; borrow their bundle-prompt checklist but keep our orchestration.
- **vs. intuitiveml:** our plan/spec discipline > their Linear-triage model for knowledge work; borrow their deterministic-pipeline principle, not their flat ticketing.

---

## Part C — Recommended next steps (workflow layer)

Commitment-graded. No bundling.

**Immediate (one session each):**
- **W.1** — Add a "Camp 2" paragraph to `workflow-artifact-model.md`; point new proposals at it (witcheer).
- **W.2** — Add the correction-loop compensation check to `self-review` or `iteration-close` (sullyai).

**Short-term (one plan each):**
- **W.3** — `open_questions:` structured list on specs + `dot-agents workflow open-questions` command (the_smart_ape).
- **W.4** — Bundle-prompt template audit: add anti-scope + output-contract sections (codex-multi-agent-swarms).
- **W.5** — `/reweave` skill: at plan close, walk prior plans whose assumptions may be invalidated, propose `status: superseded` or `status: needs-review` (arscontexta).

**Medium-term (one plan, possibly stacked on `kg ingest`/`kg lint`):**
- **W.6** — Dynamic output contracts on delegation bundles; deterministic fan-in (sullyai + codex-multi-agent-swarms).
- **W.7** — Parallel verification track: verifier concurrent with next impl, gated before fan-in (milksandmatcha).
- **W.8** — Three-lens parallel PR review for review-heavy plans (intuitiveml). Composes with planning-lenses (the_smart_ape) from the KG doc.

**Explicitly deferred:**
- CI/CD pipeline ceremony of intuitiveml's shape — we're not shipping a product from this repo.
- Cross-company KG / DKG handoff (covered in KG doc).

---

## Part D — Trust gate (read before acting on any P0/P1 above)

The priority labels (P0/P1/P2/P3) in Part C are **author judgment**, not
validated evidence. Every per-article Risk profile block reports an
`Evidence` strength (structural / anecdotal / pattern / measured).
Priority rankings were written *without systematically downgrading
recommendations whose underlying evidence is a single operator's report*.

Before turning any P0/P1 here into a plan:

1. **Re-tier the underlying evidence.** If the article body is a single
   operator's anecdote, treat the recommendation as *directional*, not
   *load-bearing*. Demand a second independent source, a small internal
   pilot, or a written rationale that does not depend on the anecdote.
2. **Check for converging sources.** A recommendation is stronger when
   two or more articles arrive at it independently (synthesis themes in
   Part B flag these). Converged items are safer to prioritize.
3. **Prefer reversible adoption.** Per-article Risk profiles report
   `Reversibility`. Prefer P0 items whose rollback cost is trivial
   (rule edits, template changes). Defer items whose rollback is
   painful (infrastructure, widespread hook deployment) until a
   specific internal need pulls them.
4. **Caveat communication.** When pitching any recommendation from this
   doc, cite the underlying Evidence strength and Reversibility so the
   decision-maker is not misled by the priority label.

This trust gate applies equally to the sibling evaluation docs and the
original `articles-evaluation-kg-and-adjacent.md`.

---

*Document status: draft evaluation. No changes made to code, specs, or plans.*
