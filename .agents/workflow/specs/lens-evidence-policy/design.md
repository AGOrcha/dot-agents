# Spec: Lens Evidence Policy

**Status:** accepted (graduated from project-local proposal
`.agents/proposals/lens-evidence-policy-renderings.md`, user-accepted
2026-05-28 with three inline addenda). This document is the canonical
contract; the proposal is preserved for historical context and for
its empirical renderings across the dot-agents / payout / ResumeAgent
project shapes.

**Companion spec (co-designed):** `[[layered-pr-fanout]]`. Lens dispatch
defined here happens INSIDE the `awaiting_agent_review` substatus
defined by that spec. The two specs are introduced as a pair — the
4 additive `verification.evidence_policy` fields land in the same
change set as the layered-fanout implementation.

**Related specs:**
- `[[layered-pr-fanout]]` — owner of the `awaiting_review` /
  `awaiting_agent_review` substatus and slot semantics; references
  this spec for the concurrency policy used during agent review.
- `[[thermo-nuclear-lens-evaluation]]` — admission of the thermo-nuclear
  lens to the default `lens_set` is governed by this spec's decisions
  on default set and routing.

**Related plans:**
- `evidence-policy-schema-cleanup` (PR #148) — F1/F2/F3 cleanup of the
  EXISTING evidence_policy schema (`classification_required` surfacing,
  `primary_chain_max` split, `sandbox_mutations` adversarial-lens
  active-probing wiring). That work is a prerequisite for the additive
  fields this spec introduces and is not duplicated here.

**Related follow-up tasks:**
- `lens-failure-telemetry` — wire `workflow verify record --kind review`
  to emit per-lens accept/reject + reason so a future revision of this
  spec can be data-driven (user addendum, see §10).

---

## 1. Problem & Goals

### 1.1 Problem

The layered-pr-fanout spec introduces an `awaiting_agent_review`
substatus during which lens reviewers (architecture-standards,
acceptance-invariants, adversarial, optionally thermo-nuclear) are
dispatched against a PR. That spec deliberately leaves the lens
concurrency model (parallel vs sequenced, project-tunable vs global)
unresolved — it is one of three open §6.6 sub-questions that the
parent proposal raised.

Lens dispatch has real cost (token spend, wall-clock latency, maintainer
cognitive load when reconciling multiple verdicts) and real value
(false-negative escape coverage). The trade-off between those is not
constant across projects: a small single-language CLI, a polyglot
payment service, and a prompt-driven AI agent each have different
lens-relevance profiles and different failure-mode distributions.

Without a locked policy, every project ends up either (a) overpaying
on small low-risk PRs by always running every lens, or (b) underpaying
on high-risk PRs by trimming the lens set globally to keep costs down.

### 1.2 Goals

1. **Default parallel.** Wall-clock minimization — the user-visible win
   that motivates the layered-fanout spec — is preserved by running
   lenses concurrently inside `awaiting_agent_review` whenever no
   project-specific policy says otherwise.
2. **Opt-in sequencing.** Projects whose lenses are expensive or
   coupled can declare a sequencing policy per-bundle without forcing
   that cost on projects that do not need it.
3. **Project-shape fit.** The schema is expressive enough to encode
   gated cascades (architecture-first for dot-agents,
   adversarial-first for payout), tier-based parallel-then-gate
   (cheap-three + expensive escalation for hot-velocity services), and
   per-`app_type` routing (drop architecture-standards on prompt PRs,
   restrict adversarial-only on cicd-pipeline diffs).
4. **Additive, non-breaking.** All policy fields are optional. A
   bundle with no `lens_*` configuration behaves exactly as the
   layered-fanout spec's default ("run all default lenses in parallel
   inside `awaiting_agent_review`").
5. **Cleanly composable with the verifier path.** The verifier
   sequence and lens dispatch share `verification.evidence_policy`
   but do not share retry budgets or accept/reject semantics — see
   F2 in the layered-fanout spec §8 and the prerequisite work in
   `evidence-policy-schema-cleanup`.

---

## 2. Decisions

Every decision below is locked. Rationale + alternatives-rejected are
inline. The decision set corresponds to §5 of the renderings proposal,
re-stated as imperative spec requirements.

### 2.1 Default lens dispatch is parallel

When a bundle's `verification.evidence_policy` does not set
`lens_concurrency`, all lenses in the resolved lens set run
concurrently inside `awaiting_agent_review`. Wall-clock is
`max(t_lens_i)`, not `sum(t_lens_i)`.

- **Rationale:** the layered-fanout spec exists to remove serialization
  bottlenecks; sequencing lenses by default would re-introduce one.
  Most PRs have no findings, in which case parallel and sequential
  produce the same verdict but parallel returns sooner.
- **Rejected:** forced sequential dispatch (defeats the parallelism
  goal); per-task dynamic policy keyed on diff size (premature; no
  telemetry exists yet to ground the heuristic).

### 2.2 Sequencing is opt-in via `lens_concurrency`

The bundle field `verification.evidence_policy.lens_concurrency`
accepts `parallel | gated | tiered`. Default when unset: `parallel`.

- `parallel` — concurrent dispatch (§2.1).
- `gated` — sequential, honoring `lens_chain[].on_fail` /
  `on_pass` semantics; the dispatcher short-circuits on the first
  failing lens whose entry declares `on_fail: short_circuit`.
- `tiered` — concurrent within a tier, gated between tiers, honoring
  the `lens_tier_gate` schema.

- **Rationale:** three modes is the minimum that covers the three
  rendered strategies (default parallel, gated cascade,
  parallel-then-gate). Two would force one of the renderings into a
  contortion; four would invite YAGNI fields.
- **Rejected:** `sequential` as a separate value distinct from `gated`
  with `on_fail: continue` everywhere — collapsed into `gated` to
  keep the schema small.

### 2.3 Four additive `evidence_policy` fields

The following fields are introduced under
`verification.evidence_policy` in the same change set as the
layered-fanout implementation:

- `lens_set: [<lens-name>, ...]` — explicit lens set for this bundle.
  When unset, the default is
  `[architecture-standards, acceptance-invariants, adversarial]`
  (the layered-fanout spec's default lens set, thermo-nuclear
  excluded per its own admission spec).
- `lens_concurrency: parallel | gated | tiered` — see §2.2.
- `lens_chain` — honored only when `lens_concurrency = gated`.
- `lens_tier_gate` — honored only when `lens_concurrency = tiered`.
- `lens_routing.by_app_type` — per-`app_type` override of `lens_set`.

All four fields are optional. A bundle with none of them set
behaves exactly as today's default (per the layered-fanout spec
§5.3) — parallel dispatch of the default lens set.

- **Rationale:** keeping every new field optional preserves additivity
  (no migration of existing bundles). Naming them under the existing
  `evidence_policy` block reuses the established surface rather
  than introducing a parallel `lens_policy` block (which would
  duplicate the verifier/lens split awkwardly).
- **Rejected:** a new top-level `lens_policy` block in the bundle;
  rejected because `evidence_policy` already governs verifier
  evidence semantics, and lens dispatch IS evidence semantics for
  the subjective half of verification.

### 2.4 Sequencing fields are ignored when concurrency is parallel

`lens_chain`, `lens_tier_gate`, and `lens_routing` are honored only
under the matching `lens_concurrency` mode. A bundle that sets
`lens_chain` together with `lens_concurrency: parallel` is treated
as misconfigured but not fatal — the dispatcher logs a warning and
runs parallel dispatch.

- **Rationale:** prevents the failure mode where a project ships a
  `lens_chain` and silently expects gated semantics without setting
  the concurrency mode.
- **Rejected:** treating the combination as a hard error; rejected
  because the override at runtime (overlay, flag) frequently sets
  one field without the other, and a hard error breaks ergonomics.

### 2.5 Default lens set excludes thermo-nuclear

Thermo-nuclear lens membership in `lens_set` is governed by
`[[thermo-nuclear-lens-evaluation]]`, not by this spec. Until that
spec's admission criteria are met, the default `lens_set` is the
three-lens set above. Projects MAY opt thermo-nuclear in via an
explicit `lens_set` enumeration, an `app_type`-routed entry, or a
`lens_tier_gate.tier2`/`tier3` placement.

- **Rationale:** consistent with the renderings §5 "what does NOT
  change" clause; keeps the admission decision in the spec that owns
  it rather than smuggling it through this one.

### 2.6 Routing is per-`app_type`, scoped to the bundle

`lens_routing.by_app_type` overrides `lens_set` when the bundle's
`app_type` matches a key. If no key matches, `lens_set` is used as-is.

- **Rationale:** `app_type` is already on every bundle and is the
  natural key for lens-relevance routing.
- **Rejected:** routing by diff-content heuristics (path globs, file
  extension); rejected as too clever and too easy to fool with
  cross-cutting refactors.
- **Deferred:** promotion of `lens_routing` to a project-level
  `.agentsrc.json` key. See §9.

---

## 3. Schema

The four additive fields under `verification.evidence_policy`. None
exist in `commands/workflow/types.go` today; all are introduced by
this spec's implementation phase.

```yaml
verification:
  evidence_policy:
    # --- existing fields (governed by the evidence-policy-schema-cleanup plan) ---
    require_negative_coverage: true | false
    classification_required:   true | false   # F1 — surfaced or removed per cleanup
    sandbox_mutations:         true | false   # F3 — adversarial lens active-probing extension per cleanup
    verifier_chain_max:        <int>          # F2 — split out of primary_chain_max per cleanup

    # --- additive fields introduced by this spec ---
    lens_set:         [<lens-name>, ...]
    lens_concurrency: parallel | gated | tiered          # default: parallel
    lens_chain:                                          # honored only when lens_concurrency = gated
      - lens: <lens-name>
        on_fail: short_circuit | record
        on_pass: continue
    lens_tier_gate:                                      # honored only when lens_concurrency = tiered
      tier1: [<lens-name>, ...]
      tier2: [<lens-name>, ...]
      tier3: [<lens-name>, ...]
      tier_promotion: any_finding_above_LOW | tier1_all_pass | <other policy>
    lens_routing:                                        # per-app-type override of lens_set
      by_app_type:
        <app-type>: [<lens-name>, ...]
```

### 3.1 Validation rules

- Every lens name in `lens_set`, `lens_chain[].lens`,
  `lens_tier_gate.tier{1,2,3}`, and `lens_routing.by_app_type.*`
  must be a registered lens. Unknown lens names are a hard error at
  bundle resolution time.
- A given lens may appear in at most one tier of `lens_tier_gate`.
- `lens_tier_gate.tier_promotion` is an enum; unrecognized values are
  a hard error.
- `lens_chain` entries SHOULD be unique by `lens`; duplicates are a
  warning but the first entry wins.
- `lens_routing.by_app_type` keys MUST be drawn from the project's
  registered `app_type` set.

### 3.2 Interactions with existing fields

- `require_negative_coverage = false` interacts with the
  acceptance-invariants lens: when false, the lens escalates
  "test asserts shape not intent" findings from INFO to BLOCKER to
  compensate for the missing negative-coverage signal. (This is a
  semantic extension; it does not introduce a new field.)
- `sandbox_mutations = true` interacts with the adversarial lens
  per the layered-fanout spec §8 F3 (active-probing review inside
  the sandbox). The interaction is owned by the
  `evidence-policy-schema-cleanup` plan; this spec records that the
  combined behavior must continue to work under any
  `lens_concurrency` mode.
- `verifier_chain_max` (post-cleanup) governs only the verifier
  sequence; a separate per-lens retry budget MAY land later but
  is deferred (§9).

---

## 4. Strategies Catalog

Three named strategies, each expressible under the schema. The
renderings proposal §2 instantiates each across dot-agents, payout,
and ResumeAgent; this spec captures the strategy shape without
restating the per-project YAML.

### 4.1 Gated cascade

`lens_concurrency: gated` with an ordered `lens_chain` whose
high-signal lens runs first and short-circuits on failure. Saves
~50–66% of lens compute on failing-fast PRs at the cost of
sequential wall-clock and ordering-sensitive false negatives.

Best fit: projects whose failure modes are concentrated in one lens
(e.g., dot-agents → architecture-standards first; payout →
adversarial first).

### 4.2 Parallel-then-gate (tiered)

`lens_concurrency: tiered` with cheap lenses in `tier1` (run in
parallel) and expensive lenses in `tier2`/`tier3` (run only if the
tier-promotion policy fires). Keeps the wall-clock minimum on the
cheap tier and defers expensive lens spend (e.g., thermo-nuclear)
to PRs that earned it.

Best fit: hot-velocity services where most PRs are small and
expensive lens compute on every PR is wasteful (e.g., payout, dot-agents
once thermo-nuclear is admitted).

### 4.3 App-type-routed parallel

`lens_concurrency: parallel` with `lens_routing.by_app_type`
trimming the lens set for app_types that have nothing meaningful
for some lenses to say (e.g., docs PR → acceptance-invariants only;
prompt PR → adversarial + acceptance-invariants, no
architecture-standards). Keeps parallel dispatch's wall-clock win
while reducing per-PR noise.

Best fit: polyglot repos (payout) and prompt-heavy projects
(ResumeAgent).

---

## 5. Trade-off Matrix

Reproduced from renderings §4. Used for project-shape guidance and
for the rationale of §2.1 (default parallel).

| Axis | Default parallel | Gated cascade (4.1) | Parallel-then-gate (4.2) | App-type routing (4.3) |
|---|---|---|---|---|
| Compute / PR (lens-tokens) | 3–4× (all lenses always) | 1× to 3–4× depending on early-exit rate (typical: ~1.6× avg) | 2–3× (cheap tier always; expensive only on signal) | 1× to N× depending on app_type — usually 1–3× |
| Wall-clock latency | min — single lens duration | max — N× sequential | min for tier1; +1 extra step if escalated | min — typically smaller lens set |
| False-negative escape rate | low (everything reviewed) | medium — incorrect short-circuit if lens ordering hides a downstream finding | low — all cheap lenses always run; only expensive tier may be skipped | medium-high if routing is wrong (excluded lens would have caught it) |
| Maintainer cognitive load (per PR) | high — must reconcile up to 4 verdicts | low–medium — failures localized to failing lens | medium — clear tier split | low — fewer lenses report |
| Failure-mode discovery latency | best — all classes surfaced immediately | worst — each cascade step adds turnaround on rework | good — cheap surfaces fast, expensive deferred not skipped | good per-PR but BAD across PR-type drift (routing rules ossify) |
| Config surface cost | zero | medium — `lens_chain` per project | medium — `lens_tier_gate` per project | high — `lens_routing.by_app_type` matrix to maintain |
| Composability with layered-fanout §6 | clean — one phase, all parallel within | clean — N sub-phases inside `awaiting_agent_review` | clean — tier-1 sub-phase + conditional tier-2/3 sub-phases | clean — resolver picks lens-set first, then parallel-runs |

**Key insight:** sequencing is a compute-saver at the cost of
discovery-latency when lenses are coupled. For the typical no-findings
PR, parallel always wins. For the rejection PR, gated saves real
money. Without lens-failure-rate telemetry to weight the two, default
parallel is the lowest-regret choice.

---

## 6. App-Shape Guidance

Reproduced from renderings Appendix B as advisory (not normative)
guidance for project owners choosing a starting strategy.

| Project shape | Recommended starting strategy | Why |
|---|---|---|
| Small single-language CLI / lib | Default parallel (no config) | Cheap to run all lenses; per-lens signal density is high |
| Polyglot monorepo with security-sensitive subprojects | App-type routing (§4.3) per-project; gated cascade (§4.1) for high-risk subset | Lens relevance varies sharply by app_type |
| AI / prompt-heavy project | App-type routing dropping architecture-standards on `agent-prompt`; adversarial-first cascade on prompts | Architecture lens has little to say about prompt diffs; adversarial = prompt-injection lens is load-bearing |
| Hot-velocity service with mostly small PRs | Parallel-then-gate (§4.2) with thermo-nuclear as the gated tier | Keeps wall-clock minimal; saves expensive lens for diffs that earned it |
| Slow-velocity, large-PR project | Gated cascade (§4.1) | Sequencing cost hidden under PR rarity; cascade catches deep stack issues early |

Project owners SHOULD revisit their starting strategy once
lens-failure telemetry (see §10) provides empirical failure rates.

---

## 7. Interactions

### 7.1 With layered-pr-fanout

Lens dispatch defined here runs during the
`awaiting_agent_review` substatus owned by `[[layered-pr-fanout]]`.
Slot semantics (the substatus holds the worker slot; bounce-back to
`in_progress` on lens reject) are inherited from that spec — this
spec does not redefine them.

The lens-gate dispatcher transitions
`awaiting_agent_review → awaiting_owner_review` based on the verdict
aggregation rules implied by `lens_concurrency`:

- `parallel` — all lenses must accept; any reject transitions to
  `in_progress`.
- `gated` — the first lens with `on_fail: short_circuit` that
  rejects transitions to `in_progress`; lenses with
  `on_fail: record` accumulate findings without short-circuiting.
- `tiered` — within a tier, all-accept advances; between tiers, the
  `tier_promotion` policy decides whether to run the next tier.

### 7.2 With evidence-policy-schema-cleanup (PR #148)

PR #148 lands the prerequisite cleanup of the existing
`evidence_policy` fields (F1/F2/F3 in the layered-fanout spec §8):

- F1 — `classification_required` surfaced or removed.
- F2 — `primary_chain_max` split into `verifier_chain_max`; this
  spec uses the post-split naming throughout §3.
- F3 — `sandbox_mutations` extended to drive adversarial-lens
  active-probing; this spec records that the extension must
  continue to work under all three `lens_concurrency` modes.

This spec's implementation MUST land on top of the cleanup; the
additive fields described in §3 are not safe to introduce while
the existing fields are mid-refactor.

### 7.3 With thermo-nuclear-lens-evaluation

Thermo-nuclear admission to the default `lens_set` is out of scope
here. This spec ensures the lens-routing and tier-gate fields are
expressive enough to opt thermo-nuclear in on a per-project or
per-app_type basis once its admission spec accepts it.

### 7.4 With workflow-orchestrator-daemon (proposal)

If the orchestrator-daemon proposal lands, the daemon executes the
lens dispatch governed by this spec. The daemon-vs-CLI split does
not change the schema or the dispatch semantics — only the
execution surface.

---

## 8. Done Criteria (Verifiable)

A planner working from this spec MUST satisfy each of these to
consider the lens-evidence-policy work complete:

1. **Schema introduced.** `verification.evidence_policy` accepts the
   four new fields per §3; bundle JSON-schema validates them; unknown
   lens names are rejected at bundle resolution time.
2. **Default parallel.** A bundle with no `lens_*` fields produces
   parallel dispatch of the default lens set (the three-lens set
   from §2.5) inside `awaiting_agent_review`.
3. **Gated cascade honors `on_fail`.** A bundle with
   `lens_concurrency: gated` and a `lens_chain` containing at least
   one `on_fail: short_circuit` entry: when that entry rejects, no
   downstream lenses in the chain are dispatched; the task
   transitions to `in_progress`.
4. **Tiered promotion honors policy.** A bundle with
   `lens_concurrency: tiered` and `tier_promotion: tier1_all_pass`:
   tier2 is dispatched only when every tier1 lens accepts.
5. **App-type routing overrides `lens_set`.** A bundle with both
   `lens_set` and `lens_routing.by_app_type` set, where the bundle's
   `app_type` matches a routing key: the routed list (not `lens_set`)
   is dispatched.
6. **Misconfigured combinations warn, do not fail.** A bundle with
   `lens_chain` and `lens_concurrency: parallel` runs parallel
   dispatch and emits a warning; the run is not aborted.
7. **No regression on the layered-fanout default path.** Bundles
   produced by `da workflow fanout` without any `lens_*` flags
   continue to dispatch the default lens set in parallel, identical
   to the layered-fanout spec §5.3 default behavior.

---

## 9. Deferred

Explicitly out of scope for this spec; tracked as follow-up:

- **Project-level `lens_routing` in `.agentsrc.json`.** Routing
  tends to be stable across PRs in a project, so promoting it from
  per-bundle to project-level config is a real ergonomics win.
  Deferred until field stability is observed (renderings §5
  follow-up #3).
- **Auto-derived `lens_set` from `app_type`.** Today the project
  owner must enumerate the lens set explicitly (or set
  `lens_routing.by_app_type`). A future revision MAY derive a
  default lens set per registered `app_type`.
- **Per-lens retry budget.** Distinct from `verifier_chain_max`;
  not introduced here. If lens dispatch grows a retry pattern (e.g.,
  for transient infra failures), add `lens_chain_max` then — do not
  overload an existing field.
- **`sandbox_mutations` adversarial-lens wiring under all
  concurrency modes.** The F3 extension lands in
  `evidence-policy-schema-cleanup`; verifying it under `gated` and
  `tiered` modes is a follow-up integration test, not a schema
  change.
- **Thermo-nuclear admission to the default `lens_set`.** Governed
  by `[[thermo-nuclear-lens-evaluation]]`.

---

## 10. Open Questions

Open questions that this spec records but does not resolve. Each is
either tracked as a follow-up task or scheduled for a future spec
revision.

1. **Empirical lens-failure rates.** Without per-lens accept/reject
   telemetry across the three reference projects (dot-agents,
   payout, ResumeAgent), the §5 trade-off matrix is qualitative. The
   `lens-failure-telemetry` task (user addendum, 2026-05-28) wires
   `workflow verify record --kind review` to emit per-lens verdicts
   so a future spec revision can ground the recommended starting
   strategies in data.
2. **`lens_set` default routing.** Should `lens_set` (when unset)
   default to the three-lens baseline regardless of `app_type`, or
   should it default-route via `app_type` once a project registers
   its `app_type` set? Today the spec picks the former (simpler,
   less magic); the §9 deferred item revisits this.
3. **Cross-lens finding aggregation under `gated`.** When a `gated`
   chain runs lenses with mixed `on_fail` semantics
   (`short_circuit` and `record`), the surface for surfacing the
   `record`-mode findings in the maintainer's review queue is not
   yet specified. Resolved at implementation time, captured in the
   merge-back for that work.

---

## 11. Relationship to Follow-ups

User addenda from the source proposal (2026-05-28), restated as
follow-up tracking:

- **Lens-failure telemetry.** Tracked as the `lens-failure-telemetry`
  task. The proposal §5 reason #5 user addendum
  ("lets ensure we wire up telemetry then") is satisfied when that
  task lands `workflow verify record --kind review` emission of
  per-lens verdicts + reasons.
- **`primary_chain_max` split.** DONE via PR #148
  (`evidence-policy-schema-cleanup` F2). This spec uses the
  post-split naming (`verifier_chain_max`) in §3.
- **`sandbox_mutations` adversarial-lens active-probing.** DONE for
  the lens content via PR #148 F3 (semantic extension; no new
  field). This spec flags the cross-mode integration test as a §9
  deferred item.

---

## 12. Relationship to Other Specs

- `[[layered-pr-fanout]]` (co-designed; PR #149) — owns the
  `awaiting_review` / `awaiting_agent_review` substatus; this spec
  owns the concurrency policy used during that substatus. Introduced
  as a pair; the layered-fanout impl change set introduces the
  schema fields described here.
- `[[workflow-orchestrator-daemon]]` (proposal) — execution surface
  for the lens dispatch; orthogonal to this spec.
- `[[thermo-nuclear-lens-evaluation]]` (task / future spec) — owns
  thermo-nuclear admission to the default `lens_set`.
- `[[graph-backend-adapter-contract]]` — unaffected; lens dispatch
  does not depend on graph adapter surface.
- `[[config-distribution-model]]` — owner of the future
  `.agentsrc.json` `lens_policy` key if/when the §9 promotion lands.
