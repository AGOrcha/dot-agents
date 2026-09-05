# Proposal: a `model` facet on `appTypeProfile`

**Status:** ratified (implemented in this PR)
**Scope:** project-local (`dot-agents` config surface) — per `.agents/rules/dot-agents/proposal-routing.md`
**Origin:** payout-team fold-back `model-facet-apptypeprofile` — team config sources need to pin
model-tier/provider routing per app_type through `execution_profile`, instead of relying on advisory
lessons ("use haiku for mechanical slices, opus for orchestration") that no orchestrator can read.
**Related specs:** `task-tier-model-suggestion` (D3/D4/D5/D7), `app-type-profiles` (§2.6, §3.1),
`graph-backend-adapter-contract` (the adapter-ref shape being mirrored),
`stage-profile-and-routing-consolidation` (stage_profiles as the per-stage route carrier).

---

## 1. Problem

A worker's model tier is chosen by humans reading lessons. Nothing in the resolved config surface
tells an orchestrator "tasks of app_type `docs` run on haiku; tasks of app_type `go-cli` run on
opus." The routing facets that *are* mechanical — `topology.verifier_sequence`, `lenses.lens_set`,
`graph_backend` — all hang off `execution_profile.by_app_type[<app_type>]` and all reach the
orchestrator through resolved JSON (`workflow app-types --json`, the delegation bundle). Model tier
is the one routing axis missing from that surface, so it cannot be set by an org/team config source
and cannot be consumed mechanically.

## 2. Decisions

### D1 — Placement: a scalar `model` at `appTypeProfile` level (facet 5)

`model` is a top-level string property of `$defs.appTypeProfile`, a sibling of `graph_backend` —
not nested inside a `model_provider` object, and not stage-scoped.

**Why not per-stage.** Per-stage model routing **already exists and is load-bearing**:
`StageProfile` (`internal/config/agentsrc.go`) carries `model` + `model_family`, and
`internal/platform/pipeline_projection.go` refuses to emit a `StageRoute` whose model or
model_family is empty (craft §2 RULE). The `task-tier-model-suggestion` spec §7 defers
"`StageProfile`-level model inheritance" on the grounds that `StageProfile` "has no facet structure
(just label + prompt_files)" — **that deferral is stale**; the fields shipped. So the gap is not
per-stage routing, it is the *app-type-level default* above it. Building a second per-stage carrier
would duplicate `stage_profiles`.

**Why not `model_provider: {…}` (spec D3.1).** That spec asks for a nested facet carrying a
tier→size table (`atom→small`, …) plus scalar `model`/`provider`. The tier axis is inert today: the
`tier` field it reads does not exist on `CanonicalTask` or the delegation bundle (spec §6, "Tier
adoption gap"), so shipping the table now would ship a table nothing can key into. This proposal
ships the **flat pin** half of spec D4 — "(b) pin a flat `model`/`provider` for an app_type
regardless of tier" — as a scalar, and leaves the tier table to land later as a sibling object.

**Non-duplication commitment (binds the future tier work).** When the tier→size table lands it MUST
land as a sibling block (`model_provider: {tier_to_size, size_to_model, provider, …}`) that does
**not** redeclare a scalar `model`. `appTypeProfile.model` stays the single flat pin, exactly as
`graph_backend` is the single flat adapter-ref. This satisfies spec R8 ("MUST NOT introduce two
competing structs") by keeping one field per concept rather than one struct per spec.

### D2 — Semantics: open adapter-ref-style string; omit = inherit

`model` is an open string (`minLength: 1`, **no enum**), mirroring `graph_backend`:

- a bare tier alias — `haiku`, `sonnet`, `opus`
- or a provider-qualified ref — `anthropic:claude-opus-4-8`, `openai:gpt-5`
- or any source-defined ref a downstream dispatcher understands

No enum, deliberately, per `task-tier-model-suggestion` D5/§6 ("model availability churn"): a closed
enum makes every model rename a schema bump, and this repo's own harness rules already name models
that no shipped enum would contain. Validation of the ref is the dispatcher's job, not the schema's
— identical to how `graph_backend` defers to the adapter registry.

Absence is meaningful and must round-trip as absence: `omitempty` on the Go field, no default value
materialized anywhere. `ModelRef()` is the single accessor (mirroring `GraphBackendRef()`) so a
future default-model policy has exactly one place to land.

### D3 — Inheritance chain (as implemented)

Most specific wins; each rung is skipped when empty:

```
1. stage_profiles[<stage>][<slug>].model        explicit per-stage route — ALWAYS wins
2. execution_profile.by_app_type[<t>].model     the app_type default          ← this proposal
   └─ itself scope-merged: org → team → repo → project-local, per-key
3. (empty)                                      no opinion; the harness/runner default applies
```

Rung 2 is *itself* a chain. `execution_profile` merges through the config-v2 scope ladder by
per-key deep merge (`mergeField` / `CategoryMapMerge`, and equivalently through the unified profile
engine via `profilesFromExecutionProfile`). Therefore, within rung 2:

- an org layer setting `model: sonnet` is inherited by every downstream scope that omits it;
- a team layer setting `model: opus` overrides org for that app_type only;
- a repo layer omitting `model` inherits — it does **not** blank the value (the Defect-B guarantee
  already pinned for topology/lenses now covers `model` too);
- a layer touching only `model` must not disturb that app_type's `topology`/`lenses`/`graph_backend`
  (and vice versa).

Rung 1 is deliberately *above* rung 2, not below: a `stage_profiles` entry naming a model is an
explicit route for a named stage; the app-type facet is a default for the app_type as a whole. The
app-type facet therefore never overrides an explicit stage route, and this PR changes no existing
stage resolution. (`BuildPipelineSpec` still refuses an empty stage model rather than silently
falling back to the app-type value — making the app-type facet a *fallback* for empty stage routes
would change an existing hard error into a soft default, which is a separate, spec-owned decision.)

**Rejected: default to a built-in tier when absent.** Absent stays absent (spec D2 "OPEN (D2-a)"
resolves the same way). Fabricating a model for every unconfigured app_type would make the field
meaningless during rollout and would silently redirect spend.

### D4 — Consumption surfaces

`model` must exit through the same resolved surfaces that already carry app_type routing, so a loop
can route worker model tiers mechanically without re-implementing config resolution:

1. **`da workflow app-types --json`** → `model` on each app_type entry (and in the human table as a
   `model=…` suffix). This is the read-only projection an orchestrator queries before dispatch. It
   resolves from the same units-lock-backed `Snapshot` as `verifier_sequence`, so scope layers apply.
2. **The delegation bundle** → `verification.model`, resolved from
   `execution_profile.by_app_type[app_type]` on the same path that resolves `verifier_sequence`
   (`resolveFanoutVerifierDispatch`). The bundle is the hand-off boundary, so a bounded worker can
   read its model tier from the bundle alone — matching `task-tier-model-suggestion` requirement 6.

Both additions are additive optional fields (`omitempty`, schema-optional), so existing bundles and
existing `--json` consumers are unaffected.

**Known limitation (documented, not fixed here):** `workflow app-types` only lists app_types whose
`topology.verifier_sequence` is non-empty. An app_type that sets *only* `model` is therefore not
listed. Changing that inclusion rule would also change the app_type set `validateTaskAppType`
accepts — a behavioral change outside this facet's scope. Deferred.

**Deferred surfaces (rationale, not oversight):**
- `da worktree create` metadata (`internal/gitwt.CreateOptions`/`Metadata`), which already records
  `graph_backend`. Additive but touches the worktree metadata persistence format; separate change.
- `da config relevance --filter model` facet view. `graph` has one; a `model` facet view is
  ergonomic, not load-bearing for mechanical routing. Follow-up.

### D5 — Schema/struct sync obligations actually required

Per `.agents/rules/dot-agents/schema-usage.md`, the full five-place lifecycle (struct, `agentsRCCore`
mirror, `UnmarshalJSON`, `MarshalJSON`, `agentsRCKnown`) applies to **top-level `AgentsRC` keys**.
`model` is a nested field of an already-registered top-level key (`execution_profile`), and
`ExecutionProfile`/`AppTypeProfile` are plain structs with no custom (un)marshaler — the whole
`*ExecutionProfile` pointer is already carried through every mirror. The required sync is therefore
exactly two places: the Go struct field and `schemas/agentsrc.schema.json`. `additionalProperties:
false` on `$defs.appTypeProfile` is what makes that pair sufficient and makes future drift fail
loudly rather than silently.

## 3. Rejected alternatives

| Alternative | Why rejected |
|---|---|
| Nested `model_provider: {model, provider, tier_to_size}` now | The tier axis has no input — `tier` exists on neither `CanonicalTask` nor the bundle. Ships a table nothing keys into. |
| Closed enum (`haiku\|sonnet\|opus`) | Model churn becomes a schema bump; excludes provider-qualified and org-private refs. Same reasoning that made `graph_backend` an open ref. |
| Per-stage only (extend `stage_profiles`) | Already exists (`StageProfile.model`/`model_family`). The missing rung is the app-type default *above* it. |
| Both app-type and per-stage in one change | The brief forbids building both; per-stage is already built. |
| Default to `sonnet` when absent | Silently redirects spend and makes the field meaningless during rollout. Absent = no opinion. |
| App-type `model` as a fallback for empty stage routes in `BuildPipelineSpec` | Converts an existing hard error (craft §2: refuse empty stage model) into a soft default. Spec-owned decision, not a facet addition. |

## 4. Done criteria

- `$defs.appTypeProfile.model` exists as an open string; `additionalProperties: false` preserved;
  a manifest setting `model` validates and one setting an unknown sibling key still fails.
- `AppTypeProfile.Model` + `ModelRef()` exist; absence round-trips as absence (no `"model"` key
  emitted for an unset profile) — the PR #535 absent-optional class of defect is not reintroduced.
- A layer merge test proves: org value inherited when the higher layer omits `model`; higher layer's
  explicit value overrides; a `model`-only diff leaves topology/lenses/graph_backend intact.
- `workflow app-types --json` carries `model`; a fanned-out bundle carries `verification.model`.
