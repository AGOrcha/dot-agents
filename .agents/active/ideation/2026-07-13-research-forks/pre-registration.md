# Ideation-cycle pre-registration — research-intake surfaced forks (2026-07-13)

Driver-framed. Grounding reused-by-freshness from THIS session's research-intake pass
(companion doc Parts L/M written today; `.agentsrc.json` `stage_profiles` + `internal/config/
profile*.go` inspected today; `config-transitive-layering` plan read today) — no separate
`kg-brief` re-run needed (inputs unchanged, same session). Sources: research/articles/
{joon-lee-fable-cheaper-than-opus-delegation, vtrivedy-deepagents-per-model-harness-profiles,
bilevel-autoresearch-meta-autoresearching-itself}.md; eval Parts L.1, L.5, M.1.

## Fork classification (step 3)

| Fork | Nature | Classification | Handling |
|---|---|---|---|
| **B** per-model-family override layer | bounded software/config design | **empirically-testable** | full spine — pre-register → GATE 1 → prototype+neg-control → GATE 2 → ratify |
| **A** meta-loop mechanism-generation | large architecture + safety | **judgment-call** (empirical handle = a whole runtime; disproportionate) | single cross-harness adversarial pass → ratify parked-with-trigger + safety envelope |
| **C** orchestrator-swap + brief-quality axis | future experiment design | **deferred** (frozen-adjacent — resolving now touches frozen pareto cells) | pre-frame the future wave gate-neutrally; DO NOT resolve |

Triage guard: B is not "briefing-decidable" (no citable fact settles the two-phase-resolution
hazard) → defaults to HARD/empirical. A has no cheap empirical handle → judgment. C is frozen →
deferred. The step-5 cross-brain pass also reviews these triage calls.

---

## Fork B — per-model-family harness override layer (EMPIRICAL)

**Baseline (what exists).** `internal/config/profile.go`: `ProfileSelector{Role, AppType, Stage,
Harness}` scopes WHERE a fragment applies; selector-scoped fragments merge by `Order`
(value-precedence layer) with a `specificity()` + absolute-ref tie-break (Decisions 5/6);
deep-map bundle merge; `PolicyMode` narrow/replace; deny/value locks. **`model` and
`model_family` are BUNDLE VALUES a profile sets** (`stage_profiles.*.model_family: "claude"`),
NOT selector dimensions. So a Codex-specific tool alias (`execute`→`shell_command`, `apply_patch`)
or an Opus-specific prompt block (`<tool_usage>`) today must be duplicated into each stage-profile
that pins that model — there is no fragment that says "wherever the resolved model_family is gpt,
apply these overrides."

**The gap this fork tests (from L.5 / vtrivedy LangChain Harness Profiles, measured +10–20pt
tau2).** Whether a reusable per-model-family override layer belongs in our profile engine.

**Competing hypotheses.**
- **H_B1 — selector-key extension.** Add `model_family` (and/or `model`) to `ProfileSelector`.
  Resolution becomes two-phase: phase-1 resolves the effective `model_family` for a context;
  phase-2 applies `model_family`-scoped fragments. Overrides declared once, compose across all
  stages/app_types on that family.
- **H_B2 — post-resolution harness-adapter layer.** Keep the selector engine as-is; add a
  SEPARATE, non-selector adapter keyed off the already-resolved model that rewrites tool
  names / appends prompt affixes AFTER profile resolution (outside the merge engine).
- **H_B0 — null / status quo.** Per-stage duplication is cheap enough; the two-phase complexity
  (or a second resolution mechanism) is not worth it.

**What evidence discriminates.**
1. **Composition hazard (decisive).** Does `model_family`-as-selector create a resolution-order
   cycle or non-determinism when a fragment BOTH sets `model_family` (as a value) AND another
   fragment selects on it? A faithful prototype of the two-phase resolver either (a) terminates
   deterministically with a clean phase split, or (b) exhibits a cycle / order-dependence / a
   fragment that selects on a family a higher-Order fragment later overrides. (b) falsifies H_B1.
2. **Duplication magnitude (negative control).** In the ACTUAL `.agentsrc.json`, how many
   stage-profile fragments would collapse under a per-family layer? If ~0–1, H_B0 wins on
   parsimony regardless of (1).
3. **Semantic fit.** Does H_B1 preserve the existing `specificity()` tie-break and narrow/replace
   + lock semantics, or does it need new precedence rules (a complexity tax counting against it)?

**Pre-registered predictions.**
- P1: two-phase resolution is expressible but exposes a real order hazard when family-as-value
  and family-as-selector coexist — so H_B1 is viable ONLY with an explicit rule that
  `model_family` is resolved in a frozen phase-1 and is NOT itself overridable by a family-scoped
  fragment (no self-reference). Absent that rule, H_B1 is non-deterministic.
- P2: the actual duplication today is small (few profiles, all claude except one gpt
  cross-harness reviewer), so the *measured* payoff is low NOW but grows with model diversity.
- P3: H_B2 (post-resolution adapter) sidesteps the hazard but splits config into two mental
  models (selector engine + adapter) — a coherence cost the repo's single-engine design
  (`profile.go` "one engine carries app_type facets, stage composition, and capability sets")
  explicitly avoids.

**Discrimination / power criterion — REVISED per GATE 1 (v2).** The prototype MUST:
- (a) construct the concrete **3-fragment permutation fixture** (the hazard regime, not a toy):
  - fragment A (low `Order`): `bundle.model_family = "claude"`
  - fragment B: `selector.model_family = "claude"` + a sentinel override value
  - fragment C (higher `Order`): `bundle.model_family = "gpt"`
  - run **all input-order permutations**; assert whether B matches the FINAL frozen phase-1 value.
  A prototype that never builds this collision proves nothing (fidelity-gate: "a green prototype
  can prove the wrong thing").
- (b) report the real `.agentsrc.json` collapsible-duplication count as a **BENEFIT/demand
  baseline** (NOT a negative control — per GATE 1 it conflates intentional role/stage differences
  with collapsible dup), AND
- (c) add a real **no-collision control fixture** (family-as-selector with NO family-as-value
  fragment) — the actual negative control: it must resolve deterministically and identically
  regardless of order.

**Blocking acceptance invariants (GATE 1 — all must hold or H_B1 is falsified):**
1. **Locks** — a value-lock on `model_family` and a selector-scoped lock both behave correctly
   under the two-phase split (a lock is absolute; phase-2 family-scoped fragments never beat it).
2. **PolicyModeReplace** — a family-scoped fragment with `replace` resets precedence/permissions
   while locks still accumulate; verify the reset does not leak across the phase boundary.
3. **Order/specificity tie-break** — adding `ModelFamily` to `specificity()` must not silently
   change existing tie outcomes for profiles that DON'T use it (regression: existing fixtures
   resolve identically).
4. **Schema** — `schemas/agentsrc.schema.json` permits `model_family` only as a bundle value;
   H_B1 needs it as a selector key too. The prototype records this as a required schema/migration
   change (selector-key addition + `selectorKeys`/`decodeSelector` fail-closed update).
5. **Cache/projection** — the resolution cache key must include the phase-1-resolved family, or a
   family-scoped result is served stale under a different context.

**Decision rule — REVISED (v2).** Ratify **H_B1 design-approved, adopt-deferred** ONLY IF: the
permutation fixture resolves deterministically under an explicit **frozen-phase-1, no-self-
reference** rule (`model_family` resolved first and NOT itself overridable by a family-scoped
fragment) — invariant 1 above — AND all five blocking invariants hold in the prototype. If any
invariant fails, H_B1 is falsified → fall back to **H_B2** (post-resolution adapter) if the
coherence cost is acceptable, else **H_B0** (status quo). Adopt-deferred trigger is CONCRETE:
build when `.agentsrc.json` carries **≥2 distinct non-claude `model_family` values across ≥3
stage-profiles** (today: 1 gpt reviewer among ~all-claude — below threshold, so the measured
payoff is low NOW). Feeds proposal `obs-per-model-family-harness-override-layer` +
`config-transitive-layering`.

---

## Fork A — meta-loop mechanism-generation under a validation envelope (JUDGMENT)

**Baseline.** dot-agents self-improvement is human-gated distillation: fold-back → reviewed
lesson/rule/skill via `da review` + `skill-architect` validation. `full-loop-orchestration-
runtime` has meta-loop reconciliation; project memory "meta-loop launch 2026-06-25".

**The fork (from M.1 / Bilevel Autoresearch).** Bilevel Autoresearch measures that an outer loop
autonomously generating+injecting NEW mechanisms beats parameter-tuning (~5× vs no reliable
gain), and generalizes the carrier to skills/prompts/workflows/evaluators/memory-schemas — our
artifact model. The decision: **what, if anything, changes now about the dot-agents meta-loop?**

**Competing positions (judgment call — no proportionate empirical handle; the handle is a whole
runtime).**
- Pos-1 (adopt the framing, not the autonomy): encode "mechanism-change ≫ parameter-change" as
  an explicit routing heuristic in `knowledge-fold-back` (prefer authoring a new mechanism over
  tweaking an existing param), and define the safety envelope (skill-architect + `da review` as
  the validation/rollback gate the paper lacks) as the standing precondition — but keep humans in
  the loop; no autonomous generation.
- Pos-2 (prototype autonomous generation now): build a minimal outer loop that proposes a new
  lesson/rule from fold-back history and injects it under the validation gate.
- Pos-3 (pure park): note it, change nothing.

**What the cross-brain pass must resolve.** Is Pos-1's heuristic actually additive (does
`knowledge-fold-back` already imply it, making the change noise?), and is the skill-architect
validation gate a SUFFICIENT safety envelope for any future autonomous generation, or does
autonomous mechanism-injection need controls the paper itself lacks (rollback, staged canary,
blast-radius bound)? Recommended default going in: **Pos-1** (adopt framing + name the envelope;
park autonomy with a concrete trigger). The cross-brain pass stress-tests that default.

---

## Fork C — orchestrator-swap + brief-quality axis (DEFERRED, gate-neutral pre-frame only)

**Why deferred, not resolved.** The transcript plan's pareto waves are FROZEN
(post-red-team-disposition). Fork C is a NEW measurement axis (swap the orchestrator/lead model,
hold the executor, measure brief-quality as the mediator — from L.1 Cognition's finding that cost
variance lives in lead delegation behavior, orthogonal to H1's executor-swap cells). Resolving it
into a spec decision now would define a wave over a frozen plan → disallowed by freeze discipline.

**Pre-framed future experiment (ready for post-freeze, NOT run now).**
- Hypothesis: total cost/quality variance attributable to the ORCHESTRATOR model swap (holding
  executor + task fixed) exceeds the executor-swap variance H1 bounds (~4%), mediated by
  brief-quality (constraint-enumeration density).
- Discrimination: a cell that swaps only the orchestrator model across ≥2 families, with
  brief-quality scored (constraints/edge-cases/done-definition present), on the same disposable
  tasks; sub-ceiling baseline required.
- Trigger to run: the transcript plan's pareto freeze lifts (post-wave), scheduled alongside the
  K.3 effort-dial axis (Part K open-Q #3) as a joint "orchestrator-side axes" wave.
- Status: **parked** — recorded here + Part L open-Q #2 + digest addendum #11. No wave defined,
  no frozen decision reopened.
