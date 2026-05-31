# Lens evidence_policy renderings — concrete examples for §6.6(c)

**Status:** draft research artifact, project-local (`.agents/proposals/`)
**Created:** 2026-05-28
**Parent:** `[[layered-pr-fanout-with-pr-open-status]]` §6.6(c)
**Goal:** Lock the lens-reviewer concurrency model by rendering what evidence_policy-driven sequencing would look like across three real project shapes (dot-agents, payout, ResumeAgent).

---

## 0. Source-of-truth references

- Proposal under decision: `/Users/nikashp/Documents/dot-agents/.agents/proposals/layered-pr-fanout-with-pr-open-status.md` §6.6(c)
- Today's evidence_policy schema (canonical): `commands/workflow/types.go` lines 169-176 — three fields exist on the wire:
  - `require_negative_coverage : *bool`
  - `classification_required : *bool` (defined but no CLI flag today)
  - `sandbox_mutations : *bool`
  - `primary_chain_max : *int` (verifier retry budget)
- Fanout flag plumbing: `commands/workflow/cmd.go:842-844`, `commands/workflow/delegation.go:1450-1473` (`applyFanoutEvidencePolicy`)
- Bundle schema: `delegationBundleYAML.Verification.EvidencePolicy` (types.go:208-218)
- Lens reviewers in scope: architecture-standards, acceptance-invariants, adversarial (each with `tools: Bash, Read, Grep, Glob` — read-only, pass/fail verdict). Lens count stays **3**.
- `thermo-nuclear` is a **MODE of architecture-standards** (`lens_modes.architecture-standards: thermo-nuclear`), NOT a 4th lens — deep maintainability/code-judo, the strictest/harshest mode. Mechanism: `[[lens-template-and-mode-skills]]` (#167 merged); meld decision: `[[thermo-nuclear-lens-evaluation]]` §4.
- Project layouts surveyed:
  - **dot-agents** (`/Users/nikashp/Documents/dot-agents`): Go CLI, single `go.mod`, heavy SonarCloud gating, app_types: `go-cli`, `code-fix`, `design`, `docs`.
  - **payout** (`/Users/nikashp/Documents/payout`): polyglot — Go services (`po-core-api-se`, `po-cluster-manager`, `po-control-plane`, `po-mongodb-management`), TS UIs (`client-ui`, `manager-ui`, `po-web-starter`), Python tooling (`pyproject.toml`), Kubernetes (`kube/`, `swarm-cd`), shared MongoDB lib (`po-mongo-lib`), security-sensitive (Infisical, vault). app_types inferred: `go-api`, `mongo-store`, `ui-e2e`, `infra-k8s`, `cicd-pipeline`.
  - **ResumeAgent** (`/Users/nikashp/Documents/ResumeAgent`): Python + alembic, `src/app`, prompt-driven AI agent. app_types inferred: `agent-prompt`, `api`, `db-migration`.

## 0.1 Proposed schema additions (referenced throughout)

To keep §1-§4 concrete, this doc proposes the following **additive** fields under `verification.evidence_policy` (none of them exist in `types.go` yet — flagged explicitly when used):

```yaml
evidence_policy:
  # --- existing today ---
  require_negative_coverage: true|false
  classification_required: true|false
  sandbox_mutations: true|false
  primary_chain_max: <int>

  # --- proposed additions (NEW — not yet in types.go) ---
  lens_set: [architecture-standards, acceptance-invariants, adversarial]   # 3 lenses; thermo-nuclear is a MODE of architecture-standards, not a member
  lens_modes:                   # per-lens mode selection (see [[lens-template-and-mode-skills]], #167)
    architecture-standards: [standard, thermo-nuclear]   # this lens can run in thermo-nuclear mode
  lens_concurrency: parallel | sequential | gated
  lens_chain:                   # only honored when lens_concurrency = gated
    - lens: architecture-standards
      on_fail: short_circuit    # don't run downstream lenses if this fails
      on_pass: continue
    - lens: acceptance-invariants
      on_fail: short_circuit
      on_pass: continue
    - lens: adversarial
      on_fail: record
      on_pass: continue
  lens_tier_gate:               # cheap-tier → expensive-tier gate
    tier1: [adversarial, acceptance-invariants]
    tier2: [architecture-standards]                            # standard mode
    tier3: [architecture-standards:thermo-nuclear]             # same lens, thermo-nuclear mode — the expensive escalation
    tier_promotion: any_finding_above_LOW   # promote to next tier only if cheap tier flagged something
  lens_routing:                 # app-type-conditioned lens set
    by_app_type:
      ui-e2e:        [acceptance-invariants, adversarial, architecture-standards]
      agent-prompt:  [adversarial, acceptance-invariants]   # adversarial = prompt-injection lens
      infra-k8s:     [adversarial, architecture-standards]  # security-first
      go-cli:        [architecture-standards, acceptance-invariants, adversarial]
```

§5 takes a position on which of these (if any) should actually land.

---

## Section 1: Default parallel structure (baseline for each project)

All three projects run the same lens set in pure parallel. This is the trivially correct case — no `lens_*` fields required.

### dot-agents — default parallel
```yaml
# bundle.verification (excerpt)
app_type: go-cli
verifier_sequence: [unit, pr-ci]
evidence_policy:
  require_negative_coverage: true
  primary_chain_max: 3
# (NO lens_* fields — implicit default = run all three lenses in parallel)
# review_gate dispatcher reads this as:
#   lens_set        = [architecture-standards, acceptance-invariants, adversarial]
#   lens_concurrency = parallel
#   slot semantics  = `awaiting_agent_review` holds slot (§6.6(b))
```

### payout — default parallel
```yaml
app_type: go-api          # one of {go-api, mongo-store, ui-e2e, infra-k8s, cicd-pipeline}
verifier_sequence: [unit, integration, pr-ci]
evidence_policy:
  require_negative_coverage: true
  sandbox_mutations: true     # mongo-store writes need rollback sandboxing
  primary_chain_max: 4
# lens_* defaulted ⇒ parallel dispatch of all 3 lenses
```

### ResumeAgent — default parallel
```yaml
app_type: agent-prompt
verifier_sequence: [unit, prompt-eval, pr-ci]
evidence_policy:
  require_negative_coverage: false   # prompt-eval is non-deterministic; negative-coverage doesn't fit
  primary_chain_max: 2
# lens_* defaulted ⇒ parallel
```

**Wall-clock cost:** max(t_arch, t_accept, t_adv) ≈ 1× a single-lens reviewer pass per PR.
**Compute cost:** 3× sequential cost (all three lenses always run, even when arch-standards would have failed the PR outright).

---

## Section 2: Sequenced structures — the interesting cases

Three sequencing strategies, instantiated per project. All assume the proposed `lens_concurrency` / `lens_chain` / `lens_tier_gate` / `lens_routing` schema in §0.1.

### 2.1 dot-agents

dot-agents is small, single-language, with frequent code-judo refactors. The bottleneck is **structural drift** (file-size sprawl, helper duplication, layering violations) more than security or invariant breakage. Architecture-standards is the highest-signal lens here, and its **thermo-nuclear mode** is a natural escalation.

#### Strategy A — Gated cascade (architecture-first, code-judo-on-clean)
```yaml
app_type: go-cli
evidence_policy:
  primary_chain_max: 3
  lens_concurrency: gated
  lens_chain:
    - lens: architecture-standards
      on_fail: short_circuit
      on_pass: continue
    - lens: acceptance-invariants
      on_fail: short_circuit
      on_pass: continue
    - lens: adversarial
      on_fail: record
      on_pass: continue
    - lens: architecture-standards   # re-run in thermo-nuclear mode as the final escalation
      mode: thermo-nuclear           # MODE of architecture-standards (#167), not a 4th lens
      on_fail: record
      on_pass: continue
```
Rationale: a structurally-broken PR (wrong package, ad-hoc helpers) shouldn't pay for adversarial or the thermo-nuclear mode pass yet — the author will rework. Saves ~50-66% of lens compute on the failing-fast cases.

#### Strategy B — Parallel-then-gate (cheap-three parallel, thermo-nuclear mode only on clean)
```yaml
app_type: go-cli
evidence_policy:
  primary_chain_max: 3
  lens_concurrency: parallel
  lens_tier_gate:
    tier1: [architecture-standards, acceptance-invariants, adversarial]    # all in standard mode
    tier2: [architecture-standards:thermo-nuclear]                         # re-run arch-standards in thermo-nuclear mode
    tier_promotion: tier1_all_pass    # only run thermo-nuclear mode if all 3 cheap lenses pass
```
Rationale: thermo-nuclear mode is the most expensive pass (full-tree code-judo audit, 1k-line rule, file-level decomposition analysis). Running it on PRs that already failed adversarial is wasted spend. Keeps the cheap three on the parallel hot-path.

#### Strategy C — App-type-routed (different lens set for design/docs vs go-cli)
```yaml
# bundle for app_type=docs PR (e.g., editing a .agents/lessons file)
evidence_policy:
  lens_concurrency: parallel
  lens_routing:
    by_app_type:
      docs:    [acceptance-invariants]                          # one lens only
      design:  [architecture-standards, acceptance-invariants]  # no adversarial — design proposals don't ship code
      go-cli:  [architecture-standards, acceptance-invariants, adversarial]   # arch-standards also runs thermo-nuclear mode via lens_modes
```
Rationale: scaffold-time markdown PRs do not need adversarial security review; routing trims the lens set to what's meaningful per app_type.

### 2.2 payout

payout is the highest-risk project: payments + Mongo + Kubernetes + multi-service. Adversarial is the load-bearing lens here, and `sandbox_mutations` + `require_negative_coverage` matter more than on dot-agents.

#### Strategy A — Adversarial-first cascade (security-priority projects)
```yaml
app_type: go-api
evidence_policy:
  require_negative_coverage: true
  sandbox_mutations: true
  primary_chain_max: 4
  lens_concurrency: gated
  lens_chain:
    - lens: adversarial               # cheapest signal for "is this dangerous?" — runs FIRST
      on_fail: short_circuit
      on_pass: continue
    - lens: acceptance-invariants     # payment invariants (idempotency, ordering, double-spend)
      on_fail: short_circuit
      on_pass: continue
    - lens: architecture-standards    # last — pure design polish
      on_fail: record
      on_pass: continue
```
Rationale: if adversarial finds an injection / credential leak / TOCTOU race in payment flow, downstream lenses can't ship the PR anyway. Sequencing matches business-risk priority.

#### Strategy B — Tier-based with diff-size promotion gate
```yaml
app_type: go-api
evidence_policy:
  require_negative_coverage: true
  sandbox_mutations: true
  primary_chain_max: 4
  lens_concurrency: parallel
  lens_tier_gate:
    tier1: [adversarial, acceptance-invariants]
    tier2: [architecture-standards]                            # standard mode
    tier3: [architecture-standards:thermo-nuclear]             # same lens, thermo-nuclear mode
    tier_promotion: any_finding_above_LOW
```
Rationale: in a high-velocity payment service, small surgical PRs (one-line bugfix, dep bump) shouldn't pay for the full thermo-nuclear mode pass. The promotion gate says "if tier1 already flags something, escalate; otherwise stop after tier1."
Practical impact: ~70-80% of small payout PRs would skip tier2+tier3 entirely.

#### Strategy C — App-type routing across the polyglot mix
```yaml
evidence_policy:
  lens_concurrency: parallel
  lens_routing:
    by_app_type:
      go-api:        [adversarial, acceptance-invariants, architecture-standards]
      mongo-store:   [adversarial, acceptance-invariants]               # schema-shape + transaction invariants
      ui-e2e:        [acceptance-invariants, adversarial]               # UX-flow + XSS/CSRF
      infra-k8s:     [adversarial, architecture-standards]              # RBAC + manifest hygiene
      cicd-pipeline: [adversarial]                                       # supply-chain only
```
Rationale: a `kube/` manifest PR doesn't need an acceptance-invariants lens against business intent — it needs RBAC/PSP/secret-mount review (adversarial) and pipeline shape (architecture). Routing reduces lens-noise on infra/CI PRs that the generic-Go lens prompts would over-flag.

### 2.3 ResumeAgent

ResumeAgent is the smallest and most prompt-centric. The interesting failure modes are **prompt-injection / jailbreak** (adversarial) and **agent-output invariants** (acceptance) — architecture matters less because the codebase is shallow.

#### Strategy A — Two-lens parallel only (drop architecture-standards entirely)
```yaml
app_type: agent-prompt
evidence_policy:
  primary_chain_max: 2
  lens_concurrency: parallel
  lens_routing:
    by_app_type:
      agent-prompt:  [adversarial, acceptance-invariants]    # 2 lenses only
      api:           [adversarial, acceptance-invariants, architecture-standards]
      db-migration:  [acceptance-invariants, adversarial]     # alembic — schema invariants + rollback safety
```
Rationale: for `src/app` prompt edits there's nothing meaningful for architecture-standards to say. Skipping it is honesty, not laziness.

#### Strategy B — Prompt-injection-first cascade (adversarial as gate)
```yaml
app_type: agent-prompt
evidence_policy:
  primary_chain_max: 2
  lens_concurrency: gated
  lens_chain:
    - lens: adversarial               # specialized prompt-injection / jailbreak / data-exfil lens
      on_fail: short_circuit
      on_pass: continue
    - lens: acceptance-invariants     # does the agent still satisfy its contract?
      on_fail: short_circuit
      on_pass: continue
```
Rationale: a prompt change that opens a jailbreak path is unshippable; running invariants check on it is wasted spend. Adversarial gates.

#### Strategy C — App-type-routed with thermo-nuclear mode NEVER on prompt PRs
```yaml
evidence_policy:
  lens_concurrency: parallel
  lens_routing:
    by_app_type:
      agent-prompt:  [adversarial, acceptance-invariants]                  # NO architecture-standards at all → no thermo-nuclear mode
      api:           [architecture-standards, acceptance-invariants, adversarial]   # arch-standards runs thermo-nuclear mode via lens_modes
      db-migration:  [acceptance-invariants, adversarial]
  lens_modes:
    architecture-standards: [standard, thermo-nuclear]   # only matters where arch-standards is routed (api), never on agent-prompt
```
Rationale: thermo-nuclear mode's 1k-line + code-judo rubric is meaningless against a `.md` prompt or YAML config file. Since `agent-prompt` routing drops the architecture-standards lens entirely, the thermo-nuclear mode never fires there — this is equivalent to "architecture-standards in standard-only / not-at-all on prompt PRs" (see `[[lens-template-and-mode-skills]]` §6).

---

## Section 3: What evidence_policy fields drive each sequencing decision

| Sequencing decision | Existing fields used | NEW fields required (proposed in §0.1) |
|---|---|---|
| Default parallel | none — implicit | none |
| Gated cascade (Strategy A everywhere) | `primary_chain_max` (per-lens retry budget) | `lens_concurrency: gated`, `lens_chain[].{lens,on_fail,on_pass}` |
| Parallel-then-gate (dot-agents B, payout B) | `primary_chain_max` | `lens_tier_gate.{tier1,tier2,tier3,tier_promotion}` |
| App-type routing (all C strategies) | `app_type` (already on bundle) | `lens_routing.by_app_type` |
| Sandbox-driven adversarial intensity | `sandbox_mutations` (existing) interacts with adversarial: if `true`, adversarial gets extra freedom to actually exercise mutation paths in a sandbox; today this only affects verifier behavior, not lens behavior. Proposed extension: adversarial lens reads `sandbox_mutations` to decide whether to run "active probing" findings vs. read-only review. |
| Negative-coverage as lens trigger | `require_negative_coverage` (existing) today only gates verifier; proposed extension: when `false`, acceptance-invariants lens raises severity of "test asserts shape not intent" findings to BLOCKER (compensating for the missing negative-coverage signal). |
| Per-lens retry budget | NEW — today `primary_chain_max` is single-scoped to verifier. Need either: (i) rename → `verifier_chain_max` and add `lens_chain_max`; or (ii) keep `primary_chain_max` as the bundle-wide chain-budget shared across verifier+lens. Cleaner: option (i). |

**Field overload watch:** today `primary_chain_max` is documented in `cmd.go:844` as "verifier retry budget." Reusing it for lens retry quietly changes semantics. Recommend splitting before any lens-sequencing lands.

**Cross-field interaction not in proposal:** `sandbox_mutations=true` + `lens_concurrency=gated` is a useful combination for payout (adversarial lens gets to actively probe in a sandbox, then gates downstream lenses on what it finds). Worth explicitly testing if this combo is supported.

---

## Section 4: Trade-off matrix (default-parallel vs sequenced)

| Axis | Default parallel | Gated cascade (2.A) | Parallel-then-gate (2.B) | App-type routing (2.C) |
|---|---|---|---|---|
| **Compute / PR (lens-tokens)** | 3× lenses, +1 arch-standards thermo-nuclear-mode pass when enabled (all always) | 1× to 3-4× depending on early-exit rate (typical: ~1.6× avg) | 2-3× (cheap tier always; expensive only on signal) | 1× to N× depending on app_type — usually 1-3× |
| **Wall-clock latency** | min — single lens duration | max — N× sequential | min for tier1; +1 extra step if escalated | min — typically smaller lens set |
| **False-negative escape rate** | low (everything reviewed) | medium — if lens-1 passes a deep issue lens-2 would catch, you might short-circuit incorrectly; lens ordering matters | low — all cheap lenses always run; only the expensive tier-3 may be skipped | medium-high if routing is wrong (excluded lens would have caught it); explicit per-app-type lens choice = explicit per-app-type blind spot |
| **Maintainer cognitive load (per PR)** | high — must reconcile up to 3 lens verdicts (plus arch-standards' thermo-nuclear-mode pass when enabled) | low-medium — failures are localized to the failing lens | medium — clear tier split, easy to see "what tier escalated and why" | low — fewer lenses report, less noise |
| **Failure-mode discovery latency** | best (all classes surfaced immediately) | worst (each cascade step adds a turnaround if upstream fails and is reworked) | good (cheap tier surfaces fast, expensive tier deferred not skipped) | good per-PR but BAD across PR-type drift (routing rules can ossify and miss new risk categories) |
| **Config surface cost** | zero | medium — `lens_chain` per project | medium — `lens_tier_gate` per project | high — `lens_routing.by_app_type` matrix, must be kept in sync with app_type registry |
| **Composability with §6.6(a)+(b)** | clean: lens dispatch is one phase, all parallel within | clean: lens dispatch becomes N sub-phases inside `awaiting_agent_review` | clean: tier-1 is one sub-phase, tier-2/3 are conditional sub-phases | clean: dispatcher just resolves lens-set first, then parallel-runs it |

**Key insight:** sequencing is a **compute-saver** at the cost of **discovery-latency** when lens passes are coupled (e.g., a thermo-nuclear-mode finding sometimes only makes sense in context of architecture-standards' standard-mode finding — but with gated cascade you may run the standard pass and never reach the thermo-nuclear-mode pass; #167's multi-mode synthesize step is the mitigation). For the typical PR (no findings) parallel always wins. For the rejection PR, gated saves real money.

**Empirical question we don't have data for:** what fraction of PRs across the three projects fail at least one lens? If <20%, sequencing saves little. If >60%, sequencing dominates. Without telemetry, defaulting to parallel and offering opt-in sequencing is the lowest-regret move.

---

## Section 5: Recommendation for §6.6(c)

**Lock §6.6(c) to: project-configurable with default = parallel.**

Concrete language proposed for the proposal §6.6(c) resolution:

> **(c) RESOLVED**: Lens reviewers default to **parallel dispatch** (all lenses in the bundle's `lens_set` run concurrently inside `awaiting_agent_review`). Sequencing is opt-in via the bundle's `evidence_policy.lens_concurrency` field (`parallel` | `gated` | future: `tiered`). The `lens_chain`, `lens_tier_gate`, and `lens_routing` fields are introduced as additive bundle schema in the same change set as the layered-fanout spec, but are only honored when `lens_concurrency != parallel`. Projects pick their own strategy per-bundle (typically via `applyFanoutEvidencePolicy` defaults or per-app-type policy in `.agentsrc.json`).

Reasons:
1. **Default-parallel is the lowest-regret default.** Wall-clock minimization is the user-visible win the layered-fanout proposal is built around — sequencing lenses serially partially defeats that.
2. **Opt-in sequencing matches the §6.6(a)+(b) shape.** Those already say "`max_parallel_within_task` derives from evidence_policy when unset" — the same per-bundle override mechanism handles lens sequencing.
3. **Project shape genuinely varies.** dot-agents wants architecture-standards as the early gate; payout wants adversarial first; ResumeAgent wants a 2-lens set with thermo-nuclear excluded. Forcing one global strategy is wrong; forcing per-project config without a sane default is also wrong.
4. **Schema additivity is cheap.** All four proposed fields (`lens_concurrency`, `lens_chain`, `lens_tier_gate`, `lens_routing`) are optional pointers; `applyFanoutEvidencePolicy` already follows that pattern. No breaking change.
5. **Avoids the empirical question.** Without lens-failure telemetry we cannot rank strategies; the recommendation makes parallel the no-config path and lets early adopters surface data via opt-in sequencing. --Nikash addendum 05/28 makes sense lets ensure we wire up telemetry then

### What does NOT change
- Lens count stays **3**: {architecture-standards, acceptance-invariants, adversarial}. `thermo-nuclear` resolved to a **MODE of architecture-standards** (the MELD decision, `[[thermo-nuclear-lens-evaluation]]` §4), wired via `lens_modes.architecture-standards` and the shared lens-template mechanism (`[[lens-template-and-mode-skills]]`, #167) — not a separate 4th lens and no longer an open experiment.
- `awaiting_agent_review` substatus holds the slot (§6.6(b)) regardless of `lens_concurrency`.
- Verifier sequencing already follows its own evidence_policy semantics — unchanged.

### Recommended follow-up work (out of scope for §6.6(c))
- Split `primary_chain_max` into `verifier_chain_max` + `lens_chain_max` before any sequencing lands (semantic overload risk). -- Nikash addendum 05/28 - yes do this
- Add lens-failure telemetry to `workflow verify record --kind review` so a future revisit can be data-driven. -- Nikash addendum 05/28 - yes do this
- Consider promoting `lens_routing.by_app_type` to `.agentsrc.json` (project-level policy) instead of per-bundle, since routing tends to be stable across PRs in a project. 
- Wire `sandbox_mutations` to influence adversarial lens behavior (active probing in sandbox), not just verifier.

---

## Appendix A — How each rendering would be built today

For any project, an orchestrator would set the policy via fanout flags. Today only the three existing fields have flags. New fields would need either:

1. Bundle-edit overlays (`--bundle-overlay path/to/overlay.yaml`) — already exists; works immediately for experimentation.
2. New flags in `cmd.go:842-844` — modest CLI surface growth (~4 new flags).
3. Project-level defaults in `.agentsrc.json` under a new `lens_policy` key — cleanest UX once the schema stabilizes.

The proposed sequence: (1) bundle-overlay for early iteration, (2) flags once two strategies converge, (3) `.agentsrc.json` after a release of field stability.

## Appendix B — Quick lookup: which strategy fits which project shape?

| Project shape | Recommended starting strategy | Why |
|---|---|---|
| Small single-language CLI / lib | Default parallel (no config) | Cheap to run all 3 lenses; signal density per lens is high |
| Polyglot monorepo with security-sensitive subprojects | App-type routing (2.C) per-project; gated cascade for high-risk subset | Lens relevance varies sharply by app_type |
| AI / prompt-heavy project | App-type routing dropping architecture-standards on `agent-prompt`; adversarial-first cascade on prompts | Architecture lens has little to say about prompt diffs; adversarial = prompt-injection lens is the load-bearing one |
| Hot-velocity service with mostly-small PRs | Parallel-then-gate (2.B) with architecture-standards' thermo-nuclear mode as the gated tier | Keeps wall-clock minimal; saves the expensive mode pass for diffs that earned it |
| Slow-velocity, large-PR project (rare merges, big batches) | Gated cascade (2.A) | Sequencing cost is hidden under PR rarity; cascade catches deep stack issues early |
