# Ideation report: KG-informed resolutions for the 3 "design-blocked" specs

**Method:** `kg-ideate` flow (kg-brief evidence-gathering → ideate) run read-only against
`origin/master` (@ `8d18e9ef`). For each spec: extract the blocking open questions, gather
grounding evidence (shipped code, adjacent specs/plans, history, lessons — cited by file:line),
then recommend a resolution per question with rejected alternatives, classify owner-ruling vs
code-resolvable, and state what ratifying it unblocks. **No spec was edited; this is a
report for owner ratification only.**

Generated: 2026-07-04. Source refs are paths on `origin/master` (@ `8d18e9ef`).

---

## Headline meta-finding

**None of the three specs is actually design-blocked.** The KG-grounded evidence shows all
three are **stale, not blocked**: each has either already shipped-and-archived, or (external
sources) is a reference fork whose delegatees shipped. The "design-blocked" framing is an
artifact of spec status/footers that never got reconciled after the work landed. The real
recurring blocker is **coherence drift** (spec headers, footers, and dead cross-links that
contradict merged/archived reality), plus a small residue of genuinely-deferred **owner
rulings** — none of which block anything currently buildable.

| Spec | True status | Genuine owner rulings (non-blocking) | Recommended action |
|---|---|---|---|
| graphstore-concurrency-contract | Shipped + **archived** (plan gcc1–gcc5, PRs #30/#34/#57/#66) | Path B daemon (defer on load) | Reconcile status → **archive-as-done** |
| planner-evidence-backed-write-scope | Shipped + **archived** (5 delegated tasks, 2026-04-21) | Q3 auto-fold-back policy; Q6 field-hoisting | Coherence fix; optional small follow-ons |
| external-agent-sources | **Reference fork**, no owning plan; delegatees shipped | Q1 FIPS MIP-vs-CMVP; Q2 public registry host | Refresh stale pointers; keep as reference doc |

**Bottom line for the owner:** there are **no design decisions gating plannability** here. There
are **two spec coherence fixes** (graphstore + planner-evidence, both code-resolvable) and one
**reference-doc pointer refresh** (external-agent-sources). The only true product/policy calls
left are external-agent's Q1 (FIPS build variant) and Q2 (public registry host) — both already
have safe locked interim defaults, so nothing waits on them.

---

## 1. graphstore-concurrency-contract

**Spec:** `.agents/workflow/specs/graphstore-concurrency-contract/design.md`
**Task-flagged as:** blocked, few explicit OQ markers — "determine what blocks it."

### What blocks it (confirmed)
A **status contradiction**, not an open design decision. The spec header still says
`Status: draft / open-decision` (`design.md:3`) and the footer still says *"No code changed.
Pivotal fork (A/B/C) is a maintainer decision before this graduates to a plan"* (`design.md:124-126`).
**Both statements are false.** The pivotal A/B/C fork was decided — `design.md:53`
`DECISION (maintainer, 2026-05-17): C — Hybrid` — a canonical plan was created, implemented
across 5 tasks, and **archived**: `.agents/history/graphstore-concurrency-contract/PLAN.yaml`
(`status: archived`), `impl-results.md` records gcc1–gcc5 completed with merged PRs
#30/#34/#57/#66. The "blocker to graduating to a plan" is illusory — it already graduated,
shipped, and archived.

### Evidence — shipped code vs the spec's Done-criteria (`design.md:89-97`)
- **"One published `Store` contract; all callers + `Deps` bind to it"** → **MET.**
  `internal/graphstore/store.go:231-237` defines `Store` composing the five roles
  (`CodeGraphReader`/`CodeGraphWriter`/`KGNoteStore`/`NoteSymbolLinkStore`/`Closer`, `:170-222`);
  compile-time `var _ Role = (*SQLiteStore)(nil)`/`(*PostgresStore)(nil)` assertions at
  `store.go:246-261`; DI boundary `Handle` at `store.go:276-301`; callers bind via
  `commands/workflow/deps.go:35` (`Store graphstore.Handle`).
- **"Bounds enforced + uniform across native/CRG; request timeout; regression tests prove hard
  cap + cross-path parity"** → **MET.** Single chokepoint `bounds.go:30-53`
  (`hardMaxNodes=5000`, `hardMaxDepth=12`, `hardSearchLimit=2000`, `requestTimeout=30s`);
  CRG routes through the *same* helpers (`crg.go:694` `normalizeTraversalBounds`;
  `crg.go:143,147,154` `requestContext`+`exec.CommandContext`). Hard cap proven by
  `bounds_enforcement_test.go:26-120` and `regression_contract_test.go:46-129`
  (`TestRegression_HardCap_NeverOvershoots_NativeBFS`,
  `TestRegression_CrossPathParity_ClampsAreTheSameNumbers`); cross-provider timeout uniformity
  by AST test `regression_contract_test.go:165-282`.
- **"Concurrency model documented; chosen strategy implemented behind unchanged contract"** →
  **MET.** `internal/graphstore/CONTRACT.md:104-149` documents ownership (handle
  single-goroutine; cross-process serialization is the provider's job) + Path A guidance.
  Path A implemented: lazy open `lazy.go:25-27,255-260`; SQLite WAL + `busy_timeout=5000`
  (`sqlite.go:99,130`) — note the pool is now `SetMaxOpenConns(512)` (`sqlite.go:94`), which
  **supersedes** the spec Problem's stale `SetMaxOpenConns(1)` text (`design.md:12`); Postgres
  via `pgxpool` (`postgres.go:11,17,23-24`).
- **"maxNodes Low-1 closed via the contract; di-refactor OD-1 closed"** → **MET.** Low-1:
  `regression_contract_test.go:39-68` (`TestRegression_MaxNodes_Low1_ClosedViaContract`). OD-1:
  `.agents/history/di-refactor-rollout/OPEN-DECISIONS.md:32-46` "RESOLUTION (2026-05-17): defer
  to the graphstore contract spec… accept & document *with teeth*"; rationale landed at
  `commands/workflow/deps.go:18-25` ("Store is a contract-typed handle whose provider owns
  concurrency… See internal/graphstore/CONTRACT.md 'Deps boundary'").
- **O7 executor-tier vs Store-seam** → single-sourced. `graph-backend-adapter-contract/
  open-questions-resolutions.md:790-825` resolves O7 (executor sits *above* `Store`, cross-ref
  only). The spec's O7 note (`design.md:99-113`) is exactly that pointer-up — re-specifies
  nothing (correct per lesson `single-source-of-truth-across-specs-and-plans`).

### Open items + resolutions
| Item | Resolution | Rejected alt | Class | Unblocks |
|---|---|---|---|---|
| Stale header (`:3`) + footer (`:124-126`) | Reconcile to reality: decision C ratified, plan implemented + archived (Path A shipped, Path B deferred) | Re-open the A/B/C fork as undecided — contradicts the merged plan + archived history | **Code-resolvable** (fact reconciliation) | Spec can be **archived-as-done** instead of masquerading as design-blocked |
| Bulk-export bound exemption (`ReadNodes`/`ReadEdges` keep `0=all`, unclamped) — flagged `CONTRACT.md:200-217` | Ratify the already-documented exemption (full-graph sync is legitimately unbounded; timeout still applies), or route a streaming/paged export to adapter-contract | Silently clamp export at `hardSearchLimit=2000` — truncates legitimate full-graph warm-link sync (`CONTRACT.md:204-209`) | **Code-resolvable** (code already made the safe choice; one-line ratification) | A tiny doc/follow-up, not a Path A replan |
| Path B persistent daemon (promote `MCPServer.Serve`, `mcp_server.go:109`, into a pool-owning service + warm CRG worker) | Keep deferred per decision C until measured load justifies; contract is deliberately Path-B-ready (`CONTRACT.md:219-228`) | Build the daemon now — premature complexity (lifecycle/autostart/socket-security) absent load | **Owner-ruling** (needs measured-load judgment) | Nothing now — correctly parked as future work |

**What this unblocks:** **Archive-as-done.** No new plan is warranted for Path A. Action =
status reconciliation folding in two explicit residual notes (bulk-export exemption; Path B
owner-gated). Cosmetic housekeeping: archived `TASKS.yaml:97` shows gcc5 `in_progress`
(closeout captured mid-flight) while `PLAN.yaml` `status: archived` + `impl-results.md` confirm
completion.

---

## 2. planner-evidence-backed-write-scope

**Spec:** `.agents/workflow/specs/planner-evidence-backed-write-scope/design.md`
**Blocking OQs:** §11 Q1–Q6 + the §3.4/§7.3/§9-Phase2 operational precondition.

### What blocks it (confirmed)
**Not design-blocked — already implemented and merged.** The canonical plan was fully executed
and archived: `.agents/history/planner-evidence-backed-write-scope/delegate-merge-back-archive/
2026-04-21/` (5 delegated tasks: `sidecar-schema`, `derive-scope-command`, `check-scope-command`,
`sidecar-manual-experiment`, `fanout-evidence-integration`). §9 Phases 1–3 all shipped. The
`specs/` `design.md` is stale relative to its own shipped plan.

### Operational precondition (the headline gate) — **MET, code-resolvable**
- The "dangling dependency" is **not missing — it completed and moved to history**:
  `.agents/history/graph-bridge-command-readiness/PLAN.yaml` (`status: completed`), with
  `impl-results.md`; sibling `kg-command-surface-readiness` also `completed`. The spec's link
  `../../plans/graph-bridge-command-readiness/PLAN.yaml` (`design.md:7`) is just **dead doc
  drift** (target archived), not an open blocker.
- All named query surfaces are shipped + wired: `da kg bridge query --intent
  symbol_lookup|callers_of|callees_of|tests_for|impact_radius` (`commands/kg/bridge.go:31-41,
  623-643`; wired `commands/kg/cmd.go:132-160`); `da kg impact` (`commands/kg/cmd.go:259-268`);
  `da workflow graph query --intent plan_context|decision_lookup|workflow_memory|contradictions`
  (`commands/workflow/graph.go:33-38,474-515`).
- "Dependable" is *operationalized, not assumed*: `GraphBridgeHealth` (`graph.go:169-183`)
  derives `CodeLaneReady`/`ContextLaneReady` from warm-store counts with `setLaneReadyStatus`
  → healthy/partial/degraded (`graph.go:241-254`); `derive-scope` reads `adapter.Health()` and
  **degrades honestly to `confidence:low`** rather than blocking (`plan_task.go:208,236-248`).
  So the spec's "blocked until dependable" gate was resolved by making the tool degrade
  gracefully instead of hard-gate.
- Still-active `graph-backend-adapter-contract` (7/10 tasks; "bridge decommissioning") is a
  backend generalization (CRG→kg-native), **not** a re-block — the query-intent contract is
  stable.

### Per open question (§11)
| Q | Resolution | Rejected alt | Class | Unblocks |
|---|---|---|---|---|
| **Q1** sidecar vs `TASKS.yaml.evidence_ref` | **Sidecar-only via deterministic path.** `CanonicalTask` (`types.go:120-131`) has no `evidence_ref`; path computed from `(plan_id,task_id)` by `deriveScopeEvidencePath` (`plan_task.go:125-127`) — no stored ref needed | Add `evidence_ref` field (§3.3 "don't change TASKS.yaml schema in first slice"; unnecessary given deterministic path) — though per `additive-state-fields` lesson it'd be clean if a consumer must know evidence exists without a filesystem stat | **Code-resolvable** (already decided) | Nothing further |
| **Q2** confidence scoring | **Heuristic from lane-readiness + query coverage:** `deriveScopeConfidence(mode,codeReady,contextReady,hasScopeInputs,queryCount)` → low/medium, never "high" (`plan_task.go:307-326`). Optional upgrade: fold in the ready-made `computeSparsityScore` 0–100 per-query score (`bridge.go:748-768`, `resp.SparsityScore`), currently unused by the sidecar | Manual (unauditable); pure-coverage (can't distinguish code vs research mode) | **Code-resolvable** (shipped; sparsity upgrade optional) | Nothing blocking; sparsity fold-in is a small follow-on |
| **Q3** auto fold-back on excluded-path-proven-necessary | **Recommend opt-in `--fold-back` flag on check-scope, not always-auto.** Detection already done: `classifyCheckScopeFiles` flags `touchedExcluded` and exits 1 (`plan_task.go:2577-2613,2645`); `da workflow fold-back create` is a one-shot (`commands/workflow/cmd.go:945-960`). An intentional exclusion later proven necessary is often a legitimate scope correction, not a design gap — always-auto = noise | Always-auto fold-back on every scope escape — noisy, conflates correction with gap | **Owner-ruling** (policy: scope-escape → tracked fold-back vs silent correction) | Review/closeout automation once ruled |
| **Q4** shell/runtime weakly-graphed files | **Mode routing + `excluded_paths` + `open_gaps`, no forced precision.** `deriveScopeMode` (`plan_task.go:280-305`) classes non-Go as doc/research; `deriveScopeWarningsForMode` (`plan_task.go:161-172`) **skips** scope-lane graph queries for non-code modes, recording the skip in `open_gaps` instead of inventing edges. `sidecar-manual-experiment` merge-back confirmed "graph queries add little for research tasks" | Force graph edges onto shell paths — false precision | **Code-resolvable** (implemented) | Nothing further |
| **Q5** `tests_for` into TDD gate vs planner-only | **Keep decoupled / planner-only.** Existing TDD gate is the pre-verifier fanout gate `delegation.go:953-994` — pure filesystem glob (Go non-test file in scope needs a sibling `*_test.go`), `--skip-tdd-gate` escape (`cmd.go:905`), **no graph dependency** (works when KG cold). `derive-scope`'s scope-lane runs symbol_lookup+callers_of+impact_radius but does **not yet wire `tests_for`** (`plan_task.go:342-354`) despite the intent existing — optional enrichment | Couple the must-pass gate to graph `tests_for` — makes a reliable gate depend on warm-store freshness (fragile) | Mostly **code-resolvable** (keep decoupled); light owner call on enrichment | Nothing blocking; `tests_for` enrichment is a small follow-on |
| **Q6** `SLICES.yaml` carries contract fields vs sidecar-only | **Keep the split.** `CanonicalSlice` (`types.go:141-150`) already carries lightweight contract fields (`VerificationFocus`, its own `WriteScope`, `Summary`); rich fields (`decision_locks`, `required_reads`, `stop_conditions`, `provides/consumes`, `excluded_paths`) stay in the sidecar `ScopeEvidence` (`plan_task.go:36-58`) to avoid bloating the hot-path struct every command parses | Hoist all contract fields into SLICES — schema churn on the engine struct + all consumers | **Owner-ruling** (how much contract belongs in engine-parsed YAML), low urgency | Marginal; not blocking |

**What this unblocks:** Nothing is genuinely design-blocked. Precondition met; Q1/Q2/Q4
resolved in shipped code; Q3/Q5/Q6 are deferrable enforcement/hoisting policy calls, none
blocking. Action = **coherence fix, not new design**: repoint the dead
`graph-bridge-command-readiness` link to `.agents/history/…`, mark §9 Phases 1–3 done,
downgrade §11 to "resolved in impl / deferred owner-rulings." Crisp optional follow-ons (both
primitives already shipped): wire `tests_for` into derive-scope's scope-lane; fold
`SparsityScore` into `deriveScopeConfidence`.

---

## 3. external-agent-sources

**Spec:** `.agents/workflow/specs/external-agent-sources/design.md`
**Blocking OQs:** §13 Q1 (FIPS), Q2 (public default registry) + implicit TBDs Q3 (CLI surface,
§3/§6), Q4 (staged-profile type mapping + provenance, §5.1).

### Framing (the key finding)
**The plannable work is not in this fork — it lives in its delegatees, which are largely
shipped.** The spec's own status line calls it *"design artifact (not an implementation
commitment in loop-agent-pipeline)"*; it has **no owning plan** (no `plans/external-agent-sources/`,
no `plans/config-distribution-model/`). It is a standalone reference fork of
`loop-agent-pipeline/decisions.1.md` D6/D6.a. Everything it delegates to
**config-distribution-model** is specified and shipped in 0.4.0/0.4.1: `sources`/`extends`/
`packages` field surface, reference syntax (§5), unified `units` lock + `inputs_digest` (§7/§15),
per-source caching (§8), audit taxonomy (§9), `da config explain` (§10/§13.1) — with concrete
files `internal/config/lock_units.go`, `ensure_resolved.go`, `commands/config/explain.go`
(config-distribution-model §15.8). The adjacent OCI-wire plan **`extends-oci-relax` is
`status: complete`** (all 3 tasks completed) — OCI is now a first-class source for both layers
and artifacts (`internal/config/fetcher_oci_layer.go`, media type
`application/vnd.dot-agents.config-layer.v1+json`). Per lesson
`single-source-of-truth-across-specs-and-plans`, the correct end-state is: this fork **stays a
reference doc** for the transport/auth/OCI-wire/FIPS/audit-base details.

### Per open question
| Q | Resolution (+ interim default) | Rejected alt | Class | Unblocks |
|---|---|---|---|---|
| **Q1** FIPS: MIP vs CMVP-validated | **Escalate to the persona's CMMC assessor.** Interim default already locked and needs no decision to proceed: single binary, `GOFIPS140=inprocess` + runtime `GODEBUG=fips140=on`; BoringCrypto variant stays persona-gated + unbuilt (§7; decisions.1.md:190-194 "80% confidence 'MIP is fine'"). Grep for `GOFIPS140|fips140|boringcrypto` hit **only** the fork's own lineage doc — FIPS is purely aspirational, zero implementation | Pre-build the BoringCrypto/CGO variant now — wasteful (CGO cross-compile, 2nd artifact) for a variant 80%-likely never needed | **Owner-ruling** (needs assessor answer) — but **non-blocking** | Only gates whether a 2nd build variant ever ships in v2 |
| **Q2** public default registry host | **Product ruling; recommend interim (c) no default / explicit config only** (v1.5 is BYO-registry by design — §6; no default-registry consumer code exists). D6 (decisions.1.md:287,597) leans (a) GHCR under a `dot-agents` org as the eventual pick | (b) owned infra — ongoing cost/ops, no v1.5 demand; committing to (a) now — premature, binds a hosting identity before any `da packages publish` path exists | **Owner-ruling** (product/infra) — **non-blocking** | Only the v2 zero-infra public consume/publish UX |
| **Q3** CLI / `sources`/transport surface (§3/§6 "TBD") | **Mostly stale — consume side shipped.** Sources declared in `.agentsrc.json` `sources[]`, resolved via `EnsureResolved`; `da config` subtree shipped as `explain/sync/lint/verify/relevance/migrate` (config-distribution-model §13.1, verified 2026-06-25; `commands/config/`); OCI-as-source shipped (`extends-oci-relax` complete; `fetcher_oci_layer.go`); per-transport verbs replaced by one `--scope`/`--source` flag (`commands/internal/cmdutil/source_routing.go`). **Only publish** (`da packages publish …`) is unbuilt — no `commands/packages/` dir; §13.2 marked "Superseded by §15 (D3) — not shipped"; publish is v2-deferred | Treat all of §3/§6 as open — wrong; only publish is open | **Code-resolvable** | Repoint §3/§6 "TBD" at config-distribution-model §13 (shipped consume surface); mark only *publish* as remaining v2 fork-owned |
| **Q4** staged-profile type mapping + provenance (§5.1) | **Mapping DONE; only artifact-side digest-provenance remains (gated on the unbuilt OCI-artifact resolver, not this fork).** The "canonical staged-profile-dispatch plan" §5.1 demanded exists and is **`status: completed`**: `stage-profile-and-routing-consolidation` (6/6 tasks) — shipped the Tier-1-vs-Tier-2 assignment as `execution_profile.by_app_type.<type>.topology` + `stage_profiles.<stage>.<slug>` (`internal/config/execution_profile.go`); `da config explain` already emits per-field provenance + a `digest` (`commands/config/explain.go:431,519-533`) | Leave §5.1 unresolved — the type-mapping ambiguity ("does a profile become an OCI package") was closed by the completed consolidation plan | **Code-resolvable** | Downgrade §5.1 to a pointer to the completed plan + config-explain digest surface; artifact digest-provenance readback follows the v2 artifact resolver |

**What this unblocks:** Only **Q1 (FIPS)** and **Q2 (registry host)** are genuine owner/policy
rulings — and both are **non-blocking** (safe locked interim defaults; nothing buildable waits
on them). **Q3/Q4 are code-resolvable and mostly already resolved** by config-distribution-model
§13 + the completed `extends-oci-relax` and `stage-profile-and-routing-consolidation` plans —
the fix is to update this fork's stale "TBD" pointers, not to plan new work. **Nothing here is
a standalone plannable unit:** the consume/source/OCI-wire/config-explain surface is shipped;
the only remaining fork-owned work (OCI-artifact **publish**, package **signing/attestation**,
the FIPS **build variant**) is explicitly v2 roadmap. Recommendation: keep this a **reference
fork**, refresh §3/§6/§5.1 pointers to the shipped delegatees, carry Q1/Q2 as owner-facing v2
gates.

---

## Consolidated owner-ruling list (what actually needs a human decision)

Everything else is code-resolvable coherence/pointer housekeeping. These are the only genuine
product/policy calls — **all non-blocking** (each has a safe default already in place):

1. **external-agent Q1 — FIPS MIP-state vs CMVP-validated.** Needs the insurance persona's
   CMMC assessor. Default holds: single binary `GOFIPS140=inprocess`; BoringCrypto variant
   unbuilt. Escalate only on a real regulated-customer engagement.
2. **external-agent Q2 — public default registry host** (GHCR `dot-agents` org vs owned infra
   vs no-default). Default: no-default / BYO until a v2 publish path exists. D6 leans GHCR.
3. **graphstore — Path B persistent daemon: build now or keep deferred?** Recommend keep
   deferred per decision C until measured multi-agent load justifies it (contract is already
   Path-B-ready). Owner confirms the "not yet" and the trigger metric.
4. **planner-evidence Q3 — scope-escape policy:** should check-scope auto-create a tracked
   fold-back, or is a silent scope correction acceptable? Recommend opt-in `--fold-back` flag.
5. **planner-evidence Q6 (low urgency) — how much execution-contract belongs in engine-parsed
   `SLICES.yaml` vs the sidecar?** Recommend keep the split as shipped.

## Recommended code-resolvable actions (no owner ruling needed)

- **graphstore:** reconcile `design.md:3` header + `:124-126` footer to "decision C ratified,
  plan implemented + archived"; add residual notes for bulk-export exemption + Path B defer →
  then **archive-as-done**. (Do not re-open the A/B/C fork.)
- **planner-evidence:** repoint the dead `graph-bridge-command-readiness` link to
  `.agents/history/…`; mark §9 Phases 1–3 shipped; downgrade §11 to "resolved in impl /
  deferred owner-rulings." Optional small follow-ons: wire `tests_for` into derive-scope's
  scope-lane; fold `SparsityScore` into `deriveScopeConfidence`.
- **external-agent-sources:** refresh §3/§6/§5.1 "TBD" pointers to config-distribution-model
  §13 + the completed `extends-oci-relax` / `stage-profile-and-routing-consolidation` plans;
  keep the doc as a reference fork; carry Q1/Q2 as owner-facing v2 gates.

*These edits are recommendations for owner ratification; this ideation run made no spec
changes.*
