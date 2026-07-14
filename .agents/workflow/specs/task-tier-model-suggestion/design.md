# Spec: task-tier-model-suggestion

**Status:** draft (all decisions proposed; D2/D6 open)
**Created:** 2026-06-07
**Author:** human (nikashp) — drafted with agent assist
**Origin:** wave-engine readiness — the wave engine needs a `(provider, model)` pair per dispatched task, and the `skill-tiering-contract` already encodes the determinism/autonomy signal that should pick model *size*.
**Related:** `skill-tiering-contract` (tier vocabulary + invariants), `app-type-profiles` / `execution_profile` (the scope-merged routing surface), `multi-provider-design` (sibling spec; owns the *provider* axis), `workflow-parallel-orchestration` (delegation bundle schema), `workflow-artifact-model` (lifecycle tiers).

---

## 1. Problem

The wave engine dispatches each task to an agent, and that dispatch ultimately requires a concrete `(provider, model)` pair. Today nothing in the workflow data model suggests *which model size* a task warrants. A one-file mechanical edit and a multi-molecule orchestration are dispatched identically, so operators either over-provision everything to the largest model (wasted budget and latency) or pick by gut feel (under-provisioning autonomy-heavy work, which then fails or burns retries).

We already have a signal for this and it is not being used. The `skill-tiering-contract` (`.agents/workflow/specs/skill-tiering-contract/design.md` §3–§4) classifies composition primitives by **composition depth and agent autonomy**:

- **T0 / atom** — indivisible, ~deterministic, declares zero downstream calls (design.md §4 "T0 — atom", lines 56–62).
- **T1 / molecule** — runtime judgment *bounded* to picking among declared atoms (design.md §4 "T1 — molecule", lines 64–70).
- **T2 / compound** — agent judgment *unbounded* within the molecule set; non-deterministic by construction; empirically ceilings at ~8–10 molecules / ~60–70% reliability before degrading (design.md §4 "T2 — compound", lines 72–78; `research/articles/shivsakhuja-skill-graphs-2.md` — "8–10 molecules max before degrading").
- **T3 / cell** — a spec; a contract surface, not a dispatched work unit (design.md §4 "T3 — cell", lines 80–86).

Tier *is* the determinism/autonomy gradient. That gradient is exactly the property that should pick model size: deterministic atoms tolerate a small model; bounded molecules need medium reasoning; autonomous compounds need a large model for orchestration judgment within their reliability ceiling. The mapping is defensible because tier already captures the reasoning depth and error-rate tolerance the model must cover.

This is currently impossible because **tier is not carried on the dispatched artifacts.** Skills declare `tier` (`~/.agents/skills/dot-agents/iteration-close/SKILL.md` lines 5–33; pinned by `commands/workflow/tier_declarations_test.go` lines 9–68), but `CanonicalTask` (`commands/workflow/types.go:120`) and `delegationBundleYAML` (`commands/workflow/types.go:235`) have **no** `tier` field, and neither `schemas/workflow-delegation-bundle.schema.json` nor the tasks schema declare one. The tier→model suggestion has nowhere to attach until that gap is closed (which the `skill-tiering-contract` D5 rollout already plans to close for bundles, with tasks as a fast-follow).

## 2. Goals

- Define a **tier → model-size default mapping** (`small/medium/large` → `haiku/sonnet/opus`) with rationale grounded in the tier determinism/autonomy gradient.
- Specify **how a task's tier is determined** (declared vs inherited vs inferred) so the suggestion has a stable input.
- Specify **where the suggestion surfaces**: a `model_size` (and resolved `model`) hint on the TASKS.yaml entry, in `da workflow next` / `eligible` output, on the `execution_profile` (`AppTypeProfile`) as the scope-merged default vehicle, and on the delegation bundle handed to the loop-worker.
- Specify how the suggestion is **overridable** per scope (config-v2 org→team→repo→project ladder) and per task, with a clear precedence order.
- Specify how this **composes with the multi-provider design** so the wave engine resolves a complete `(provider, model)` pair — model *size* is this spec's axis; *provider* and the size→concrete-model-id resolution are the sibling spec's axis.
- Keep the whole thing **additive and advisory**: a suggestion, not a hard gate, unless an operator opts into enforcement.

**Non-goals (this spec):** authoring the lint command, building the dispatcher that consumes the hint, migrating existing artifacts, choosing concrete model IDs / pricing, or deciding the provider. Those are plan-tier or sibling-spec concerns (§10, §11).

## 3. Decisions

### D1 — Tier → model-size mapping (proposed)

The mapping is expressed as **abstract sizes** (`small | medium | large`), with a default size→Anthropic-model binding. The size layer is the contract; the concrete binding is config-defaultable so model churn does not break the contract (see D5 and the risk in §6 about model reordering).

| Tier (name / T#) | Determinism / autonomy signal | Default size | Default model |
|---|---|---|---|
| atom (T0) | ~deterministic, zero downstream calls, mechanical | **small** | haiku |
| molecule (T1) | bounded judgment among declared atoms, coordination | **medium** | sonnet |
| compound (T2) | unbounded judgment within molecule set, ~60–70% reliability ceiling | **large** | opus |
| cell (T3) | spec / contract surface — **not a dispatched work unit** | — (advisory only) | — |

**Rationale.** The mapping tracks the tier invariants verbatim (`skill-tiering-contract` design.md §4): an atom is "~deterministic modulo LLM sampling" and tolerates the smallest model; a molecule's "runtime agent judgment is bounded to picking among declared atoms" — medium reasoning suffices; a compound is "non-deterministic by construction" with judgment "unbounded within the declared molecule set" and needs the strongest orchestration model to stay inside the empirical ~60–70% reliability ceiling (shivsakhuja). This extends the established `impl_defaults.model_preference: [opus, sonnet]` precedent in `app-type-profiles` (design.md §3, lines 148–152) — that file already ranks models by capability for an app_type; tier→size is the same idea ordered by the determinism gradient instead of by app_type.

**Why three sizes, not the full tier range.** T3/cell (spec) and T4/organism (doc) are not dispatched as work units — a spec is authored/reviewed, not executed against a model-size budget — so they get no size mapping. The wave engine only dispatches T0–T2 (slices, tasks/molecules, plans/compounds). The mapping is deliberately defined only over the dispatchable tiers.

### D2 — How a task's tier is determined (proposed; partially open)

Tier is **self-declared, lint-verified — never silently inferred** at the point a suggestion is computed. This matches `skill-tiering-contract` D3 ("self-declared + lint-verified") and the spec's §10 deferral of automatic inference.

Resolution order for a task's effective tier (first hit wins):

1. **Explicit `tier` on the `CanonicalTask`** (TASKS.yaml) — authoritative.
2. **Inherited from the delegation bundle** when the task is fanned out, per `skill-tiering-contract` D2 default ("task tier inherited from bundle; explicit override allowed"). The bundle's `tier` is itself resolved per the tiering contract (§3 "Bundle-vs-plan nuance": a bundle's tier is the max of its own tier and its children).
3. **Plan-level default** — if the plan declares a default tier, unmarked tasks adopt it.
4. **Absent → no suggestion.** If tier cannot be resolved, the suggestion is simply *not emitted* (the field is omitted). Absence of a tier must never block dispatch or fabricate a size — that would violate the additive/advisory goal.

**OPEN (D2-a):** whether a *missing* tier should default to `molecule` (medium) as a safe middle, or stay absent. Default: **stay absent** (no suggestion) — fabricating `medium` for every untiered task would make the suggestion meaningless once most tasks are untiered during rollout. Revisit once tier adoption on tasks is high.

Static inference of tier from a task's `calls:` list is **deferred** (matches `skill-tiering-contract` §10 "Automatic tier inference"). Lint may *propose* a tier mismatch, but the suggestion engine reads the declared value only.

### D3 — Where the suggestion surfaces (proposed)

The suggestion is computed from tier and surfaces at four points, in flow order:

1. **`execution_profile` (the scope-merged default vehicle).** Add a `model_provider` facet to `AppTypeProfile` (alongside `Relevance`, `Topology`, `Lenses` — `internal/config/execution_profile.go:32-44`) carrying an optional **tier→size table** plus optional scalar `model` / `provider`. This is where org/team/repo/project scopes set defaults, because `execution_profile` already merges through the config-v2 ladder via `CategoryMapMerge` (`internal/config/resolver.go:40-47, 67`); per-key deep-merge means a scope can override just the tier table without disturbing topology/lenses. This is the primary vehicle, consistent with the surface-research finding that `(model, provider)` hints belong as a fourth `AppTypeProfile` facet.

2. **`CanonicalTask` in TASKS.yaml.** Add optional `tier` and optional `model_size` (and optional `model` / `provider` for a hard per-task override). `tier` is the input; `model_size` is the *suggestion output*, written advisory so it is visible/diffable in the plan. A human or the engine may pin `model_size` directly to override the tier-derived default.

3. **`da workflow next` / `da workflow eligible` output.** When the CLI emits an eligible/next task, include the resolved `model_size` (and resolved `model`/`provider` once the sibling spec lands) in both human and `--json` output, so an operator picking work sees the suggestion before dispatch. This is read-only projection — it does not mutate TASKS.yaml.

4. **The delegation bundle.** Extend `delegationBundleYAML.Verification` (`commands/workflow/types.go:264-274`) — which already carries `app_type` and `verifier_sequence` — with an optional `model_provider` block (`tier`, `model_size`, resolved `model`, `provider`). `resolveFanoutVerifierDispatch` (`commands/workflow/delegation.go:1385-1405`) already resolves `verifier_sequence` from `execution_profile.by_app_type[app_type]`; it resolves the model facet from the same path and populates the bundle alongside the verifier dispatch. The loop-worker receives the bundle and can introspect `bundle.Verification.ModelProvider` (the worker already consumes `bundle.Verification` fields).

The bundle is the hand-off boundary: whatever the engine resolved becomes the worker's instruction. The other three surfaces are upstream (config defaults → plan record → operator preview).

### D4 — Override model (per scope and per task)

Two override planes, composed as a precedence ladder (highest wins):

```
per-task explicit model / provider        (CanonicalTask.model, .provider)         ← hard pin, bypasses tier
per-task explicit model_size              (CanonicalTask.model_size)               ← size pin, still size→model via config
per-task tier                             (CanonicalTask.tier → mapping)
bundle tier (inherited)                   (delegationBundleYAML.tier → mapping)
execution_profile model_provider facet    (scope-merged: org→team→repo→project)    ← defaults + tier→size table override
built-in default mapping                  (atom→small, molecule→medium, compound→large)
```

- **Per-scope override** is the `execution_profile.by_app_type[*].model_provider` facet, merged through the config-v2 ladder. A scope can (a) override the tier→size table (e.g. a budget-constrained repo maps `compound→medium`), or (b) pin a flat `model`/`provider` for an app_type regardless of tier.
- **Per-task override** is `model_size` (size pin) or `model`/`provider` (hard pin) on the `CanonicalTask`. A hard pin bypasses the tier mapping entirely.
- The **built-in mapping** (D1) is the floor: it always produces a suggestion when a tier resolves and nothing higher overrides.

### D5 — Size→model binding lives in config, not code

The `small/medium/large → haiku/sonnet/opus` binding is a **config default** (a tier-to-model rule file + the `execution_profile` facet), not a hard-coded Go map. Rationale: the mapping assumes `haiku < sonnet < opus` by cost/reasoning; if models are reordered or renamed, a code constant would silently break (§6 "Model availability churn"). A config-layer default lets the binding move without a code change and lets scopes rebind sizes. The Go layer hard-codes only the **tier→size** relationship (atom→small, etc.), which is semantic and stable; the **size→concrete-model** binding is data.

Proposed rule file: `.agents/rules/dot-agents/tier-to-model-size-defaults.yaml` declaring the tier→size table and the default size→model binding, mirroring the `app-type-profiles` `impl_defaults` precedent.

### D6 — Advisory by default; enforcement is opt-in (proposed; open)

The suggestion is **advisory**: it populates fields and CLI output but never blocks dispatch. Enforcement (rejecting a dispatch whose chosen model is smaller than the tier-suggested size) is **opt-in** via a config flag on the `execution_profile` facet (e.g. `enforce_model_size: true`). Lint provides the *soft* signal regardless of enforcement: warn on `atom + opus` (overkill / budget waste) and `compound + haiku` (under-specified / will likely fail), per the override-hygiene finding. Cross-tier `calls:` remain a lint **error** owned by `skill-tiering-contract`, not this spec.

**OPEN (D6-a):** whether enforcement, when enabled, *blocks* or *auto-upgrades* (silently bumps a too-small model to the suggested size). Default: **block with a clear message** — auto-upgrade hides operator intent and surprises budgets.

### D7 — Composition with the multi-provider sibling spec

This spec owns the **model-size axis** (small/medium/large) derived from tier. The `multi-provider-design` sibling spec owns the **provider axis** and the **size→concrete-model resolution per provider**. The wave engine needs `(provider, model)` together; the two axes compose as:

```
tier ──(this spec)──▶ model_size ──┐
                                    ├──(sibling spec: provider table)──▶ (provider, concrete model id)
provider/scope ─────────(sibling)──┘
```

- The shared carrier is the `model_provider` facet on `AppTypeProfile` and the `ModelProvider` block on the bundle `Verification`. This spec defines `tier` and `model_size`; the sibling adds `provider` and the per-provider size→model table. Defining one shared struct (rather than two competing ones) is a hard requirement (§5 R8) so the schema/struct stays single-sourced.
- If the sibling lands first, this spec fills the `model_size` field it left open. If this spec lands first, `model`/`provider` resolution falls back to the Anthropic default binding (haiku/sonnet/opus) until the sibling generalizes it.

## 4. Tier → size reference (normative)

```yaml
# .agents/rules/dot-agents/tier-to-model-size-defaults.yaml (proposed shape)
tier_to_size:
  atom:     small
  molecule: medium
  compound: large
  # cell/organism: dispatched=false → no size
size_to_model:        # config-defaultable; Anthropic default binding
  small:  haiku
  medium: sonnet
  large:  opus
enforce_model_size: false   # D6: advisory by default
```

## 5. Requirements (behavioral)

1. Given a task with a resolvable tier, the system MUST emit a `model_size` suggestion per the D1 mapping (or a scope-overridden table), defaulting `atom→small`, `molecule→medium`, `compound→large`.
2. Given a task whose tier cannot be resolved (D2 order exhausted), the system MUST emit **no** `model_size` and MUST NOT block dispatch.
3. A per-task `model_size` MUST override the tier-derived size; a per-task `model`/`provider` MUST override both (hard pin).
4. A scope-level `execution_profile.model_provider` facet MUST override the built-in mapping and MUST merge through the config-v2 ladder (org→team→repo→project) via `CategoryMapMerge`, per-key (a scope overriding the tier table MUST NOT disturb that app_type's topology/lenses).
5. `da workflow next` and `da workflow eligible` MUST surface the resolved `model_size` (and resolved `model`/`provider` when available) in both human and `--json` output, as read-only projection (no TASKS.yaml mutation).
6. A fanned-out delegation bundle MUST carry the resolved model hint in `Verification.model_provider`, resolved from the same `execution_profile` path as `verifier_sequence`, so the loop-worker can read it from the bundle alone.
7. The suggestion MUST be advisory by default; enforcement MUST be opt-in via config (D6) and, when off, MUST never reject a dispatch on model-size grounds.
8. The `model_provider` carrier (struct + schema, on both `AppTypeProfile` and the bundle) MUST be a single shared shape co-defined with the multi-provider sibling spec — this spec's fields (`tier`, `model_size`) and the sibling's (`provider`, size→model table) MUST NOT introduce two competing structs.
9. Every new field MUST be additive and optional: absent fields MUST round-trip unchanged and MUST NOT alter existing fanout/verifier/lens behavior.
10. Schema/struct sync MUST hold across all four mirror sites for any new `execution_profile` sub-field (struct in `agentsrc.go`, `agentsRCCore` alias, `MarshalJSON`/`UnmarshalJSON`, `agentsRCKnown` list at `resolver.go:529-550`, and `schemas/agentsrc.schema.json`) and across both bundle sites (`delegationBundleYAML` in `types.go` and `schemas/workflow-delegation-bundle.schema.json`).

## 6. Open questions / risks

- **D2-a:** missing tier → absent suggestion vs default `medium`. Default to absent (§D2). Revisit at high task-tier adoption.
- **D6-a:** enforcement = block vs auto-upgrade. Default to block (§D6).
- **Model size is not the only signal.** Tier captures determinism/autonomy; cost, latency, and capability (tool-use, multimodal, context length) are orthogonal. `app-type-profiles` remains the primary capability vehicle; tier→size is an optimization layer over it, not a replacement.
- **Model availability churn.** The size→model binding assumes `haiku < sonnet < opus` by cost/reasoning. Mitigated by D5 (binding lives in config, not code).
- **Compound reliability floor.** A larger model does not eliminate the ~60–70% non-determinism ceiling of T2 compounds (shivsakhuja); the verifier+review gate (`skill-tiering-contract` §4 T2) remains load-bearing. Model size raises the floor; it is not a substitute for the gate.
- **Override hygiene.** Without lint, humans can declare `atom + opus` (waste) or `compound + haiku` (under-spec). Lint warnings (D6) are the guardrail.
- **`agentsRCKnown` fragility.** Any new `execution_profile` sub-field that slips the four-place sync (R10) silently lands in `ExtraFields` and breaks round-trip. This is a known config-v2 footgun (`internal/config/resolver.go:529-550`).
- **Merge semantics.** The `model_provider` facet must stay a scalar/nested-object (not an array) so `CategoryMapMerge`'s merge-by-key recursion (`resolver.go:292-315`) applies. A per-verifier-different-model need would force an array and complicate merge — deferred (§7).
- **Tier adoption gap.** Bundles and tasks lack `tier` fields today; this spec depends on the `skill-tiering-contract` D5 rollout (bundles default, tasks fast-follow) landing the field first. Until then the suggestion is inert for tasks/bundles (it works once tier is present).

## 7. Deferred (explicitly out of scope)

- **Per-verifier / per-stage model selection** (a different model for each verifier in a sequence). Would require an array carrier and special merge handling; revisit only if the staged runtime needs it.
- **`StageProfile`-level model inheritance.** `StageProfile` (`agentsrc.go:490-502`) has no facet structure (just label + prompt_files); wiring model inheritance there needs its own schema field. Held until there is a concrete per-stage need.
- **Automatic tier inference** from `calls:` (owned by `skill-tiering-contract` §10; this spec reads declared tier only).
- **Concrete model IDs, pricing, and provider routing.** Owned by `multi-provider-design`.
- **Recording the model size actually used per task** in the verification record for post-hoc tier→model calibration. A good follow-up once data exists; not required for the suggestion to function.
- **Cost/latency budgets as a second routing signal.** Orthogonal axis; not this spec.

## 8. Done criteria (verifiable)

- `CanonicalTask` and `delegationBundleYAML` carry optional `tier` and the shared `model_provider`/`model_size` fields, with matching entries in `schemas/workflow-tasks.schema.json` and `schemas/workflow-delegation-bundle.schema.json` (schema↔struct sync verified by `go test ./commands/... ./internal/config/...`).
- `AppTypeProfile` carries the `model_provider` facet with all four AgentsRC mirror sites + schema updated; a focused test asserts a scope-merged tier→size override survives `CategoryMapMerge` per-key without disturbing topology/lenses.
- Given a fixture plan with one `atom`, one `molecule`, one `compound` task and no overrides, `da workflow eligible --json` reports `model_size` of `small`, `medium`, `large` respectively; a task with no tier reports no `model_size`.
- A per-task `model_size` and a per-task `model` override are each demonstrated to win over the tier mapping in a test.
- A fanned-out bundle for a `molecule` task carries `Verification.model_provider.model_size: medium` resolved from `execution_profile`, readable by the loop-worker from the bundle alone.
- With `enforce_model_size: false` (default), a dispatch choosing a smaller-than-suggested model is **not** rejected; lint emits a warning for `atom + opus` and `compound + haiku` fixtures.
- The `tier-to-model-size-defaults.yaml` rule file exists and is loaded (verified by `da refresh` + rule index presence).

## 9. Relationship to other specs

- **`skill-tiering-contract`** — provides the tier vocabulary and invariants this spec consumes. This spec adds *no* new tier semantics; it maps the existing gradient to model size. It depends on that spec's D5 rollout to land `tier` on bundles (and, fast-follow, tasks).
- **`multi-provider-design`** (sibling) — owns the provider axis and size→concrete-model resolution. Shares the `model_provider` carrier (R8, D7). Either may land first; the carrier is co-defined.
- **`app-type-profiles` / `execution_profile`** — the scope-merged routing surface and the `impl_defaults.model_preference` precedent this spec extends. The `model_provider` facet is the fourth `AppTypeProfile` facet next to relevance/topology/lenses.
- **`workflow-parallel-orchestration`** — bundle schema change is additive (mirrors the tiering-contract Req #3 bundle change); does not alter fanout semantics.
- **`workflow-artifact-model`** — lifecycle (spec/plan/tasks/history) is orthogonal to model size; the suggestion is a task/bundle attribute, computed at dispatch, recorded in the plan.

## 10. Non-goals / explicitly out of scope

- Authoring the lint command (owned by the tiering-contract plan; this spec only specifies the warnings it must emit).
- Building the dispatcher that selects and invokes the model from the hint.
- Migrating existing artifacts to carry `tier` (mechanical pass owned by the tiering-contract rollout).
- Choosing concrete model IDs, prices, or providers (sibling spec).
- Changing ISP pipeline stages or verifier/reviewer semantics.
