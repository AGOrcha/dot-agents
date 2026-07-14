# Fork B empirical prototype — RESULTS

Self-contained faithful re-implementation (module `forkbproto`, no dep on the real
repo) of the dot-agents selector-merge engine, EXTENDED with `ModelFamily` as a new
`ProfileSelector` dimension, to test H_B1 (two-phase resolution) against the
pre-registration's decisive hazard fixture and 5 blocking acceptance invariants.

Fidelity anchors (verified this session against the real source, NOT edited):
- `internal/config/profile.go` — `ProfileSelector`/`matches`/`specificity`,
  `ConfigProfile{Order,Selector,Bundle,Scope}`, `PolicyMode`, `ProfileLock`.
- `internal/config/profile_resolver.go` — `orderProfiles` (Order → value-rank →
  specificity → ref), `foldPolicy` (replace resets prec+perms, locks accumulate),
  two-phase policy/profile split.
- `internal/config/resolver.go` `mergeMaps` — deep-map merge (objects recurse,
  arrays/scalars replace).

## Results table

| # | Experiment | Verdict | Fact established |
|---|---|---|---|
| 1 | Hazard permutation (decisive) | PASS | Naive single-pass = ORDER-DEPENDENT (4 distinct outcomes over 6 input perms); two-phase = deterministic (1 outcome, frozen family=gpt) across all perms; B applies iff frozen phase-1 family==claude (confirmed both directions via Order flip). |
| 2 | No-collision negative control | PASS | With family-as-selector but NO family-as-value fragment, family stays unresolved, selectors inert, and BOTH resolvers are order-invariant AND identical (`{base:x}`) — the hazard is specifically caused by family-as-value ⊕ family-as-selector coexisting. |
| 3 | Invariant 1 — locks | PASS | A value-lock on `model_family` pins the FROZEN phase-1 family (beats a higher-Order gpt fragment); a role-selector deny-lock strips `Write` from `tools.allow`; phase-2 family-scoped fragments never beat either lock. |
| 4 | Invariant 2 — PolicyModeReplace | PASS | A family-scoped `replace` policy resets precedence+permissions in phase-2 (mode=replace, by=runtime) while the lower policy's deny-lock STILL accumulates (`Delete` denied); phase-1 frozen family unaffected — no leak across the phase boundary. |
| 5 | Invariant 3 — tie-break regression | PASS | For every family-free selector `specificity()==legacySpecificity()`; adding `ModelFamily` to `specificity()` changes NO existing tie outcome (more-specific and pure-ref ties resolve identically). |
| 6 | Invariant 5 — cache key | PASS | A cache key omitting the frozen family (harness-only) serves a STALE claude bundle for a different-context gpt resolution; keying WITH the frozen family fixes it. |

Invariant 4 (schema) is a documentation finding, not a runtime test — see below.

Raw: `ok  	forkbproto	0.213s`  (`go test ./... -run . -v -count=1`)

## `.agentsrc.json` benefit / demand baseline

- Total `stage_profiles` fragments: **27** (executor 1, orchestrator 1, reviewer 9, verifier 16).
- `model_family` value distribution: **26 × `claude`, 1 × `gpt`**.
- Distinct NON-claude `model_family` values: **1** (`{gpt}`).
- All 27 fragments restate `model` + `model_family` inline → a per-family default
  layer collapses 27 restatements into **2** family defaults.
- Genuine family-DRIVEN duplication (the fork's real demand): exactly **1** pair —
  `reviewer.cross-harness-adversarial` (gpt-5.4/gpt) ↔ `cross-harness-adversarial-claude`
  (claude-opus-4-8/claude): identical `prompt_files`, differ ONLY by model/family.

Adopt-deferred trigger ("≥2 distinct non-claude families across ≥3 stage-profiles")
is NOT met today (1 non-claude family, 1 profile) — measured payoff is low NOW.

## Invariant 4 — schema/migration finding (documentation, not runtime)

H_B1 requires `model_family` to become a SELECTOR key in addition to a bundle value.
Real code changes needed:
- `internal/config/profile.go`: add `ModelFamily` to `ProfileSelector`, to
  `selectorKeys` (currently `{role, app_type, stage, harness}`), and to the
  `decodeSelector` fail-closed unknown-key list; count it in `specificity()`.
- `schemas/agentsrc.schema.json`: permit `model_family` as a selector property
  (today only a bundle value), keeping `additionalProperties:false`.
- The resolver needs the phase-1/phase-2 split with the no-self-reference rule
  (a family-scoped fragment may not write `model_family`) and a cache key that
  includes the frozen family.

## Verdict

**H_B1 (two-phase, frozen phase-1, no-self-reference) is deterministic and
invariant-preserving: YES** — all 6 runtime experiments pass; the naive control
faithfully exhibits the order hazard that the two-phase rule removes.

**Single most important caveat:** determinism holds ONLY because of the added
no-self-reference rule (a family-scoped fragment cannot re-pin `model_family`) plus
re-asserting the frozen family in phase-2. This is a NEW resolver rule with no
analogue in the current single-phase engine — a real (if bounded) complexity tax.
Combined with the demand baseline (1 non-claude family, below the ≥2 trigger),
H_B1 is best ratified **design-approved, adopt-deferred**: the mechanism is sound
and buildable, but the measured payoff does not yet justify shipping it.
