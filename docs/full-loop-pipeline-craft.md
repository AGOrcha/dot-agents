# Full-loop pipeline craft — operational reference

This is the stable, operational statement of the full-loop pipeline craft: the set of
invariants any emitted pipeline must satisfy on any harness. It is deliberately
**model- and registry-agnostic** — it names no specific model, vendor, or closed capability
registry, and it carries no plan-scoped evidence anchors. It describes *what an emitted
pipeline must do and why*, not one script.

Each section first explains **how it works**, then closes with **Rules** — imperative,
checkable statements a pipeline emitter (the `pipeline-architect` skill and the projection
layer) operationalizes. Treat the rules as the acceptance surface for any generated pipeline.

## Contents

1. [Deterministic loop skeleton](#1-deterministic-loop-skeleton)
2. [Stage, profile, and model routing](#2-stage-profile-and-model-routing)
3. [Lifecycle and recovery contracts](#3-lifecycle-and-recovery-contracts)
4. [Verification and review spine](#4-verification-and-review-spine)
5. [Cost mechanics](#5-cost-mechanics)
6. [Per-harness capability matrix](#6-per-harness-capability-matrix)
7. [Anti-patterns](#7-anti-patterns)
8. [Context budget and compaction](#8-context-budget-and-compaction)

---

## 1. Deterministic loop skeleton

The full loop is a **two-owner split**: the workflow engine owns selection, slots, fanout,
and lifecycle state; the agent harness owns only agent execution. Nothing about *which* task
runs, *how many* run, or *what transition* a result triggers is left to model prose.

**The wave algorithm.** One iteration = one wave:

1. **Slots.** Query available/occupied slots; `available == 0` is a quiescent break, never a
   spin. Occupancy is a typed predicate, not a guess: a slot is held for exactly the
   in-progress and awaiting-agent-review states; everything else (awaiting-owner-review,
   blocked-on-anything) frees it. The slot budget defaults to a machine-derived value
   (available parallelism minus a reserve), not a hard-coded constant.
2. **Eligible.** Ask the engine for the eligible task set, the max batch, the conflict graph,
   and the total-eligible count; `total_eligible == 0` is a break. Dependency satisfaction is
   a typed rule (a dependency is satisfied by *completed* or *awaiting-owner-review*, and
   **not** by *in-progress*), so downstream velocity decouples from merge latency.
3. **Conflict-free selection.** Intersect the eligible set with the max-batch set — the
   largest write-scope-disjoint subset, greedy by order. Two tasks conflict iff any
   write-scope path of one is a prefix of the other. The loop thus **never** dispatches two
   workers into overlapping files.
4. **Fanout waves.** Each selected task is fanned out, its bundle resolved, and its inner
   pipeline launched in its **own process group** as a background job.
5. **Barrier.** The driver waits on every inner process before proceeding — a hard wave
   barrier. A failed inner marks the wave failed but does **not** skip reconciliation.
6. **Reconcile.** After the barrier, exactly one serialized reconcile pass runs and MUST emit
   a `RECONCILED` sentinel or the driver aborts. Only then does the next wave begin.

**Why determinism.** Prose plans drift: "status said done" is not auditable, and a model left
to choose its own next task or declare its own status will invent tasks, exceed the slot
budget, or self-report a transition that never happened. The deterministic skeleton mechanizes
the lesson that **canonical state wins over stale checkpoints** — selection, slots, fanout, and
lifecycle are computed by the engine from canonical plan/task state, so the model cannot
override them. Planning correspondingly hardens from prose plans into *execution contracts*
with locked decisions, required reads, verification targets, and stop conditions.

**Rules.**
- Compute selection, slot budget, and transitions from canonical engine state; NEVER let an
  agent choose its own next task or declare its own status.
- Gate every dispatch on the max-batch set: dispatch only write-scope-disjoint tasks in one wave.
- Treat `available == 0` and `total_eligible == 0` as clean quiescent stops, never busy-waits.
- Use the typed predicates: a slot is held only by in-progress / awaiting-agent-review; a
  dependency is satisfied by completed / awaiting-owner-review, not in-progress.
- Enforce a wave barrier: wait on all inner pipelines, then run exactly one serialized
  reconcile that MUST emit `RECONCILED` before the next wave; abort if it does not.
- Bound the run with an explicit max-waves ceiling; the live protocol requires it.

---

## 2. Stage, profile, and model routing

Routing is **typed config**, not prompt text. Stage profiles form a two-level map — stage
(executor / verifier / reviewer / orchestrator) → slug → profile. Each profile carries a
label, a concrete model, an **open-ended** model family, a base-first ordered prompt-file
composition, and an optional precondition policy. The same profile type serves all four
stages, so the agentic stages are uniform composable primitives — one routing surface, four
consumers. The model family is intentionally open-ended: diversity requires *inequality*, not
a closed vendor list, so a new tier is a config edit, not a code change, and cross-family gates
work against families the code has never heard of. Any legacy per-stage profile keys fold into
the single canonical routing map (new key wins, legacy never re-emitted).

**Prompt resolution is the projection surface.** A single `resolve-prompt` seam returns, per
stage+slug, the matched flag, the model, the model family, and the base-first, scope-resolved
prompt-file composition. Per-file precedence is fixed: absolute → repo-local project scope →
repo-local prompts scope → shared-home starter → unresolved. Repo-local committed files win
over the shared-home starter, so a project overrides the product base by dropping a same-named
file into its prompts scope. Every dispatcher — the worker, the orchestrator, and the emitted
swarm YAML — calls this one seam, so all consumers resolve the **same merged prompt**. A
resolve stage that finds a matched stage with an empty model or empty model family MUST refuse:
the projection is generated from the routing IR, never authored inline, so an empty route is a
build error caught at emit time, not a runtime surprise.

**Cross-family gate binding.** The blocking adversarial review MUST run on a different model
family than the executor; same family on both sides makes the review invalid. Bind that
diversity to a **named** adversarial lens, never to a numeric slot index or an assumed list
order: partition the named lens out, and assert its family differs from the executor/default
family. The review projects to each harness's native gate while satisfying the cross-family
rule (see [§4](#4-verification-and-review-spine)).

**Rules.**
- Express every stage as a typed profile with an explicit model AND model family; refuse to
  emit or dispatch a matched stage whose model or model family is empty.
- Resolve every stage prompt through the single resolve seam (base-first, scope-merged); NEVER
  inline duplicate prompt prose into the projection.
- Preserve prompt precedence: repo-local prompts override the shared-home starter for the same
  filename.
- Bind cross-family diversity to the named adversarial lens and assert
  `reviewer.family != executor.family`; NEVER bind it to a numeric reviewer slot or assumed
  list order.
- Keep the model family open-ended (identity comparison, no closed vendor allowlist), so a new
  tier is a config edit, not a code change.

---

## 3. Lifecycle and recovery contracts

**Delegation lifecycle.** A task moves `fanout → bundle → worker → merge-back → closeout`, all
engine-owned. Fanout materializes a contract plus a base-resolved bundle (a downstream task is
layered onto its dependency's open PR branch); the worker writes only inside the bundle's
authoritative write-scope; the parent authors a schema-valid merge-back and runs closeout. The
durability lesson: **the merge-back survives late worker failure** — the parent can author it
after confirming commit and verification even when the worker environment becomes inaccessible.

**Fold-back re-entry is bounded.** The inner pipeline's target count is a hard iteration
ceiling. A retryable verifier/lens rejection re-enters the **executor inside the same active
delegation** — it does NOT fan the task out again. Terminal fold-back is the result *after*
that bounded budget is exhausted: reconcile records each item, persists a failed merge-back,
closes out with a reject decision, and the canonical task becomes blocked with its slot freed;
a later explicit unblock/replan creates a fresh delegation. Bounded re-entry is why the loop
converges instead of looping forever.

**Failure reconciliation.** Every failure mode routes back through reconcile, never to
abandonment:
- **Crash / non-zero / missing inner exit** ⇒ recoverable lifecycle failure: record an
  idempotent fold-back with the exit code and logs, persist the failed artifact, close out
  reject, free the slot. Never claim success; never leave an orphaned in-progress delegation.
- **Stale driver lock** ⇒ the lock is pid-aware: a dead owner's lock is recovered, a live
  owner's is refused.
- **Incomplete prior wave** ⇒ on startup, reconcile any wave lacking a `RECONCILED` sentinel —
  but refuse if a live driver pid still owns its coordination directory.
- **Fanout refusal** ⇒ a failed fanout writes an explicit fold-back for that task so an earlier
  successful sibling delegation is never stranded.

The unifying invariant across all four modes: **there is no abandonment path**. Every failure is
a routed, idempotent outcome that frees its slot and leaves a durable record, so a later pass can
always reconstruct what happened without the failed worker still being alive.

**Signal co-termination.** A single terminal/tmux restart can take a whole process tree down
with pending tool calls. The driver encodes the fix: each inner pipeline gets its own process
group so an interrupt co-terminates the driver *and* every agent it spawned, not just the
wrapper; an exit trap terminates each job's process group and releases the lock. Sessions die
several ways — rate-limit walls, OS-signal co-termination, mid-turn cutoffs — and the runtime
must checkpoint before signal-class kills and treat each as resumable, not fatal.

**Rules.**
- Bound inner re-entry with a target count; a retryable rejection re-enters the executor inside
  the same delegation and NEVER re-fans an active task.
- Route crash / non-zero / missing-exit through reconcile as a recoverable lifecycle failure:
  record a fold-back, close out reject, free the slot — never claim success or orphan an
  in-progress delegation.
- Make the driver lock pid-aware: recover a dead owner's lock, refuse a live owner's, and
  reconcile any wave missing `RECONCILED` on startup unless a live pid still owns it.
- On fanout refusal, write an explicit fold-back for that task so sibling delegations are not
  stranded.
- Give each spawned pipeline its own process group and trap signals to co-terminate the whole
  tree; checkpoint before signal-class kills.
- Cover crash, stale-lock, and fanout-refusal with explicit recovery tests before shipping a
  runtime.

---

## 4. Verification and review spine

**The spine.** The inner pipeline is a strict sequence: executor → bounded verifier slots (each
gated on the prior's PASS) → bounded routine review lenses (each gated on the prior plus all
verifiers passed, read-only) → the blocking cross-family lens → the evidence gate. Cardinality
is capped, and over-cardinality is an explicit BLOCKED refusal, never a silent truncation.
Verifiers and reviewers NEVER mutate canonical workflow state; only the gate stage pushes the
owner-held PR, polls the delivery gate for the task's app-type, and authors the merge-back
draft. Verification-then-review-then-gate is the ordering, and each stage's route must equal its
own declared model and family.

```
executor
  → bounded verifier slots       (each gated on the prior verifier's PASS)
  → bounded routine review lenses (each gated on prior + all verifiers passed; read-only)
  → the blocking cross-family lens
  → the evidence gate
```

The ordering is load-bearing, not cosmetic: verification proves the change *works* before any
reviewer spends tokens arguing about it, and review completes before the gate spends effort
publishing it. Because each gate is a hard precondition on the next stage, a failure
short-circuits the remaining spend instead of paying for stages that can no longer matter.

**What real discipline looks like.** Separate live discipline from prescribed-but-dead prose:
- **In-session verification is non-optional per app-type.** Where tool outcomes persist,
  workers ground on version-control status plus tests/lints before claiming completion; that
  grounding is a stage, not a suggestion.
- **Review verdicts are structured and wired in-loop.** A review emits a structured verdict
  (risk level, authorization, outcome, rationale) that binds to the target harness's native
  quality gate — not free-text "LGTM". This generalizes to advisory and one-off use, not just
  loop-workers.
- **Falsification-first is the review contract, not affirmative render.** A review states
  pre-registered falsifiable hypotheses, each *executed* (refuted / survived / inconclusive)
  rather than argued; null results are first-class, and a zero-refutation review is returned as
  not-performed.

**Unverifiable signals.** On a harness that persists **no** tool result, tool outcomes, exit
codes, and errors are unrecoverable and visible only as narration. A review or verifier signal
that leans on self-reported result/verification text is therefore unverifiable — a coarse
substring scorer would rate it high while nothing in-transcript corroborates the work. Require
an anchor **plus** a real tool/verifier record; never accept self-report as a verification
signal.

**Rules.**
- Emit an in-session verification stage on every implementing pipeline, per app-type; a
  verifier PASS gates the next stage, and no verifier/reviewer mutates canonical state.
- Cap stage cardinality (bounded verifiers, bounded routine lenses) and refuse (BLOCK) on
  overflow rather than silently truncating.
- Require structured review verdicts (risk / outcome / rationale) that bind to the target
  harness's native gate, not free-text approval.
- Make review falsification-first: a verdict with zero executed refutation hypotheses is
  not-performed.
- NEVER accept self-reported completion as a verification signal on a harness without persisted
  tool results; require an anchor plus a real tool/verifier record.

---

## 5. Cost mechanics

**Cache-read dominance is the load-bearing fact.** In long agent context, re-sent cached
context dominates token volume. The *magnitude* is denominator- and harness-dependent — each
harness reports on its own token schema — so it is **not** a single cross-harness band; never
quote one. Productive work (output plus non-cached input, where reasoning is a subset of output
and is never added again) is a modest slice of the total. This dominance is structural to long
agent context, not an artifact of the loop or the sampler: it shows up in one-off scratchpad and
review sessions too.

**Productive-token accounting.** Raw total-token counts overstate cost because they
double-count cached re-reads (and, on some schemas, reasoning). Compute every token-volume and
token-cost axis on the **productive** figure (output + non-cached input; reasoning ⊆ output),
and report raw alongside cache-adjusted. Dollar attribution is gappy and route-dependent: some
harnesses record cost, some are token-only, some record nothing, and some provider routes bill
zero. Any cross-harness dollar comparison reconstructs a missing-cost harness from
`tokens × published-rate`, flags it as an inference, and never silently mixes reconstructed with
recorded cost; treat a zero-dollar provider route as suspect.

**Fixed per-request tax.** Tool definitions plus the system prompt are large and volume-fixed —
a floor that is independent of task complexity. A trivial task can spend most of its context on
tool definitions alone.

**Design consequences.**
- **Context reuse gates volume, not the model.** At a fixed context snapshot, swapping the
  executor model barely moves token volume (the productive fraction is small; cache volume is
  set by context size). To move volume you must change the *pipeline* — compaction, fewer
  re-reads (see [§8](#8-context-budget-and-compaction)) — not the model.
- **Dollar savings are cache-read-rate bound.** Only a minority of total dollars is
  outcome-addressable by swapping tiers; the majority re-prices purely by the swapped tier's
  cache-read rate. A tier swap is therefore a *re-pricing* of the same volume far more than it is
  a *reduction* of it.
- **Stage granularity trades against the fixed tax.** Cheap-tier fractional savings rise with
  task length; the shortest tasks yield the least because the fixed tool-def block is
  model-priced but volume-fixed. Don't route a trivial task through a heavy tool-def context.
- **Accuracy risk is localized to the generated-output fraction.** A weaker model degrades
  accuracy only in proportion to a task's model-generated output share; low-generation stages
  (context-shuffling, review) are near-zero-risk cheap-route targets even when their
  uncached read volume is high.
- **The review/verify stage is the highest-leverage cheap route.** Review turns generate very
  low output — a classification workload, not a generation workload — so routing review to a
  cheap tier saves at near-zero accuracy risk, provided the cross-family gate
  ([§2](#2-stage-profile-and-model-routing)) is preserved.

**Rules.**
- Normalize every token-cost/volume axis on productive tokens (output + non-cached input;
  reasoning ⊆ output); report raw plus cache-adjusted. NEVER price a stage on raw total tokens.
- Reconstruct any missing-cost harness from `tokens × published-rate`, flag it as an inference,
  and never mix it silently with recorded cost; treat a zero-dollar provider route as suspect.
- To reduce token volume, change the pipeline (compaction, fewer re-reads), not the executor
  model.
- Prefer coarse stage granularity on short tasks: don't route a trivial task through a full
  tool-def context; cheap-tier savings scale with task length.
- Route cheap tiers at low-generation stages first (review/verify), holding the executor at
  baseline; the executor is the LAST stage to cheap-route. Keep the cross-family gate.
- NEVER assert a cost/efficiency frontier from historical rows; require CI-backed paired live
  contrasts.

---

## 6. Per-harness capability matrix

A projection MUST be **per-harness** because the loop mechanism and telemetry differ radically
across harnesses. Describe each target by *archetype and capability*, not by product identity;
the following archetypes recur:

| archetype | drives the workflow CLI? | orchestration primitive | telemetry axes it can feed |
|---|---|---|---|
| CLI-native, full telemetry | yes (heavy) | prescribed-skill-driven | tokens, cost, wallclock, model, tool-result |
| CLI-native, no cost telemetry | yes (heavy) | prescribed-skill-driven | tokens, partial wallclock, model, tool-result; **no cost** |
| artifact-reader / native-orchestration | never (reads workflow artifacts as context) | own primitives (full-auto) | tokens, model, tool-result; **no cost** |
| contract-native, no CLI | runs the loop-worker/orchestrator contract natively, without the CLI | native task spawning | **none** of tokens/cost/wallclock/model; **zero tool-result** |
| minimal / smoke | n/a | — | tokens, credits (not cost), wallclock, model |

The rows are a **capability partition**, not a ranking: two harnesses in the same row can share a
projection shape, but two in different rows cannot — they differ on the most basic axis, whether
they drive the CLI at all.

**Consequences the projection layer must encode:**
- **A single emitted projection cannot serve every archetype.** Some harnesses drive the CLI
  directly; others never do — they read workflow artifacts and orchestrate via their own
  primitives; still others run the loop-worker/orchestrator contract natively with no CLI. File
  the transformer requirement as a proposal parallel to the platform-handling doc.
- **The cost/Pareto cell must carry a telemetry-capability mask.** A cell is
  `model_family × task_class × cache_regime × retry_regime`; **hard-exclude** any harness from
  an axis it cannot record (e.g. a harness that records no tokens/cost/wallclock/model from all
  four), and note the exclusion is a format property, invariant to workflow-vs-advisory use.
  Never score a cell on an axis its harness cannot supply; a masked axis is absent, not zero.
- **Scope findings correctly.** Cost and resilience outcomes generalize past the workflow
  sample; mechanism/orchestration projection MUST be gated on "is this a workflow session?" and
  never applied to advisory chat.
- **Record bridge origin.** When one harness spawns work on another (a real recurring bridge),
  record the originating harness so cross-harness cost roll-ups attribute bridged cost/outcome
  back to the spawning orchestrator and don't double-count.

**Rules.**
- Emit a distinct per-harness loop projection from one profile IR; NEVER assume one swarm shape
  serves every harness.
- Attach a telemetry-capability mask to every Pareto cell and hard-exclude a harness from any
  axis it cannot record.
- Gate mechanism/orchestration projection on "is this a workflow session?"; apply
  cost/resilience rules broadly.
- For an artifact-reading harness, project a no-CLI, artifact-reading driver (it orchestrates
  via its own primitives), not a CLI caller.
- Record the origin harness on bridged work so cross-harness cost roll-ups don't mis-attribute.

---

## 7. Anti-patterns

Failure modes an emitted pipeline must design against:

- **Hand-editing emitted swarm/pipeline YAML.** Once the swarm projections are emitted from the
  routing IR, a hand-edit is drift: it diverges the running pipeline from the stage profiles and
  defeats the single-source routing of [§2](#2-stage-profile-and-model-routing). Regenerate from
  the IR; treat the YAML as a build artifact, not a source.
- **Rate-limit walls mid-loop.** A loop that treats a rate-limit or usage-limit wall as a hard
  crash strands slots. Treat it as a first-class, resumable stop condition; it appears even in
  scratchpad/review sessions.
- **Unverifiable self-reported completion.** Narrated result/verification text with zero
  persisted tool result lets a coarse substring scorer rate work as high while nothing
  corroborates it. Never gate an advance on self-report; require an anchor plus a real
  tool/verifier record ([§4](#4-verification-and-review-spine)).
- **Stale-status drift.** "Status said done" is not auditable. Canonical state must win over
  stale checkpoints: re-read task/delegation/PR/slot/eligible state before **every** mutation,
  re-run slots and eligible after every canonical write, and block the next wave on any
  ambiguous state. Never advance on cached status.
- **Numeric-index diversity binding.** Binding the cross-family gate to "reviewer slot N" or an
  assumed lens order breaks silently when the list changes. Bind to the named adversarial lens
  and assert family inequality ([§2](#2-stage-profile-and-model-routing)).
- **Cross-scope worker dispatch.** Dispatching workers into overlapping write-scope shatters the
  disjoint-slice invariant. Enforce write-scope disjointness at dispatch (the max-batch conflict
  computation) and a refusing test-scope gate. Never widen a bundle's scope across packages to
  satisfy a stray test.

**Rules.**
- Treat emitted swarm/pipeline YAML as a build artifact; regenerate from the IR and refuse
  hand-edits.
- Make rate-limit and usage-limit walls first-class resumable stop conditions, not crashes.
- Never gate a transition on self-reported completion; require an anchor plus a real
  tool/verifier record.
- Re-read canonical state before every mutation and re-check slots/eligible after every write;
  block on ambiguity; never advance on cached status.
- Bind diversity gates to named slugs with asserted family inequality, never to numeric slot
  order.
- Enforce write-scope disjointness at dispatch and refuse cross-package scope widening.

---

## 8. Context budget and compaction

Context windows are not infinite, and the **effective** usable window is far smaller than the
advertised one. This section is grounded in the context-engineering / fold-back research
evaluation ([`../research/articles-evaluation-context-engineering-and-foldback-design.md`](../research/articles-evaluation-context-engineering-and-foldback-design.md)).
The underlying window/degradation figures are secondhand and treated as **design-shape only** —
adopt the *shape*, never hard-code a vendor's numbers.

**How it works.**
- **Budget to a fraction of the advertised window.** Usable working context is on the order of
  ~25–30% of the advertised window; every model degrades as length grows. Treat the advertised
  size as a ceiling, never as a target: size the pipeline's live context to the working budget,
  not the ceiling.
- **Middle-position exposure.** Information placed in the middle of a long context is recalled
  markedly worse than information at the edges (a mid-context "dead zone"). Put load-bearing
  state — the execution contract, locked decisions, the active task — at the **edges**
  (front/system and the recent tail); never bury the contract mid-transcript. Evaluate recall
  with a middle-position ("needle in the middle") probe, not only end-anchored checks.
- **Compact early.** Trigger compaction well before the window fills (compact at a fraction of
  the working budget, not at the wall); a runtime that waits for the wall has already degraded.
- **Structure over reflow; read context as data.** Past a size threshold, prefer raw chunks with
  headers over reflowed "coherent" prose; treat the accumulated context as data to be curated,
  not a transcript to be preserved verbatim.
- **The long-horizon context contract = compaction + external file system + memory.** Durable
  state lives in files and memory, not in the live window; the runtime re-injects a *compact*
  external progress ledger each turn rather than replaying full history.
- **Persistence always pairs with a verification gate.** Never add a persistence instruction
  without a matching verification gate; a persisted claim must be re-checked, not trusted.
- **Enforce in the harness, not the prompt.** Context-budget cap, middle-position exposure, and
  the compaction trigger are deterministic checks the harness enforces before spending
  lens/verifier tokens — a pre-LLM tier. Prompt-level constraints are advisory and do not survive
  optimization pressure; budget and compaction constraints must be harness-enforced.

**Rules.**
- Budget the live context to a fraction of the advertised window; treat the advertised size as a
  ceiling, not a target.
- Place load-bearing state at the context edges; NEVER bury the execution contract mid-transcript.
- Compact early on a deterministic trigger; externalize durable state to files/memory and
  re-inject a compact ledger rather than replaying full history.
- Pair every persistence instruction with a verification gate.
- Enforce the context-budget cap, middle-position exposure eval, and compaction trigger as
  harness-level deterministic checks, not prompt-advisory requests.
- Treat cited window/degradation figures as design-shape only; do NOT hard-code vendor-specific
  window sizes or degradation percentages into an emitted pipeline.
