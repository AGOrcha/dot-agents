# Full-loop pipeline craft — transferable methodology

Source for the `pipeline-architect` skill (`pipeline-architect-skill` task) and the config→materialized
pipeline transformer (`platform-projection-layer` task). This distills **what the OMP full-loop
exercise actually encoded** and **what the cross-corpus evidence base proved holds**, as reusable
craft. It is not a description of one script; it is the set of invariants any emitted pipeline must
satisfy on any harness.

Every claim is anchored. Handles: synthesis themes **T-\*** (`evidence/synthesis/synthesis-report.md`),
numbered outcomes **O1–O14** (`evidence/synthesis/actionable-outcomes.md`), Pareto hypotheses **H1–H6**
+ contrasts **C1–C6** (`evidence/pareto/historical-hypotheses.md`), prior findings **DA/EFF/BEH-\***
(`evidence/prior/prior-findings-index.md`), evidence `item_id`s (`evidence/items/*/items.yaml`), and
repo paths as `file:line`. The consumed-outcomes map is the JSON block at the end.

Each section closes with **Architect rules** — imperative, checkable statements the skill
operationalizes. Every rule traces to ≥1 anchor in brackets.

---

## 1. Deterministic loop skeleton

The full loop is a **two-owner split**: `da` owns selection, slots, fanout, and lifecycle state; the
agent harness (OMP here) owns only agent execution (`bin/tests/omp-full-loop:2-3`). Nothing about
*which* task runs, *how many* run, or *what transition* a result triggers is left to model prose.

**The wave algorithm (as implemented, `bin/tests/omp-full-loop`).** One iteration = one wave:

1. **Slots.** `da --json workflow slots` yields `available`/`occupied`; `available == 0` ⇒ quiescent
   break, never a spin (`:241-247`). Occupancy is a typed predicate, not a guess:
   `countsAgainstParallelTasks` holds a slot for exactly `in_progress` + `awaiting_agent_review`;
   everything else (incl. `awaiting_owner_review`, `blocked-on:*`) frees it
   (`commands/workflow/eligible_accounting.go:45-57`). The budget defaults to
   `NumCPU − reserve`, not a fixed 7 (`eligible_accounting.go:277-279`).
2. **Eligible.** `da --json workflow eligible --limit <available>` yields `eligible_tasks`,
   `max_batch`, `conflict_graph`, `total_eligible` (`commands/workflow/plan_task.go:1180-1187`);
   `total_eligible == 0` ⇒ break (`:249-254`). Dependency satisfaction is a typed rule
   (`completed` or `awaiting_owner_review` satisfies; `in_progress` does **not**),
   so downstream velocity decouples from merge latency (`eligible_accounting.go:72-74`).
3. **max_batch (conflict-free selection).** The driver intersects `eligible_tasks` with the
   `max_batch` set — the largest write-scope-disjoint subset, greedy by order
   (`select_wave`, `:120-142`; `computeWriteScopeConflicts`, `plan_task.go:1213-1231`). Two tasks
   conflict iff any write_scope path is a prefix of the other (`plan_task.go:1197-1211`). The loop
   thus **never** dispatches two workers into overlapping files.
4. **Fanout waves.** Each selected task is fanned out (`da workflow fanout`, `:171-174`,
   `delegation.go:1279`), its bundle resolved, and its inner pipeline launched in its **own process
   group** as a background job (`:293-305`).
5. **Barrier.** The driver `wait`s on every inner pid before proceeding — a hard wave barrier; a
   failed inner marks `wave_failed` but does not skip reconciliation (`:307-313`).
6. **Reconcile.** After the barrier, one serialized reconcile pass runs and MUST write `RECONCILED`
   or the driver aborts (`:315-326`). Only then does the next wave begin.

**What determinism bought (vs the prose-driven loop).** The priors record the exact drift the
skeleton removes: planning had to evolve from prose plans to **execution contracts** with locked
decisions, required reads, verification targets, and stop conditions (DA-C1) precisely because prose
plans drifted; the workflow had to add a **completed-plan audit** with an explicit
`completed-with-doc-drift` verdict class (DA-C2) because "status said done" was not auditable; and the
meta-loop became an explicit operating model with orchestrator-managed cross-plan state (DA-C3).
Locally, "canonical state wins over stale checkpoints" is a high-confidence cross-harness theme
(**T-d4**: `cc-workflow-cli-drives-taskstate`, `cursor-orchestrator-readback`; DA-C1/DA-C2). The
deterministic skeleton is the mechanization of that lesson: selection/slots/fanout/lifecycle are
computed by `da` from canonical `PLAN.yaml`/`TASKS.yaml`, so the model cannot invent a task, exceed
the slot budget, or self-report a transition. The test pins this: exactly 2 fanouts, 2 inner runs, 1
reconcile, zero conflicting-task dispatch, a `RECONCILED` sentinel, and no wave-2 once quiescent
(`bin/tests/omp-full-loop_test.sh:137-144`).

**Architect rules.**
- Compute selection, slot budget, and transitions from canonical `da` state; NEVER let an agent
  choose its own next task or declare its own status. [T-d4, DA-C1/DA-C2/DA-C3, `bin/tests/omp-full-loop:2-3`]
- Gate every dispatch on `max_batch`: dispatch only write-scope-disjoint tasks in one wave.
  [`plan_task.go:1197-1231`, `bin/tests/omp-full-loop:120-142`]
- Treat `available == 0` and `total_eligible == 0` as clean quiescent stops, never busy-waits.
  [`bin/tests/omp-full-loop:241-254`]
- Use the typed slot predicates: a slot is held only by `in_progress`/`awaiting_agent_review`; a dep
  is satisfied by `completed`/`awaiting_owner_review`, not `in_progress`.
  [`eligible_accounting.go:45-57,72-74`]
- Enforce a wave barrier: `wait` on all inner pipelines, then run exactly one serialized reconcile
  that MUST emit `RECONCILED` before the next wave; abort if it does not. [`bin/tests/omp-full-loop:307-326`]
- Bound the run with an explicit `--max-waves`; the frontier/live protocol requires it.
  [`bin/tests/omp-full-loop:234-239`, `pareto-measurement-rubric.md:51`]

---

## 2. Stage / profile / model routing

Routing is **typed config**, not prompt text. `StageProfiles` is a two-level map — `stage`
(`executor | verifier | reviewer | orchestrator`) → `slug` → `StageProfile`
(`internal/config/agentsrc.go:308-317`). Each `StageProfile` carries a `Label`, a concrete `Model`,
an open-ended `ModelFamily`, a base-first ordered `PromptFiles` composition, and an optional
`PreconditionPolicy` (`agentsrc.go:654-677`). The same type serves all four stages, so the agentic
stages are uniform composable primitives — one routing surface, four consumers. `ModelFamily` is
"intentionally open-ended; diversity requires inequality, not a closed vendor list"
(`agentsrc.go:666-669`) — the design already anticipates cross-family gates against models the code
has never heard of. Legacy `verifier_profiles`/`reviewer_profiles`/`app_type_verifier_map` keys fold
into `stage_profiles` (new key wins, legacy never re-emitted, `agentsrc.go:1145-1170`), so a single
canonical routing model has replaced the scattered pre-config-v2 keys.

**resolve-prompt is the projection surface.** `da workflow resolve-prompt --kind <stage> --slug <slug>`
returns a `composedPromptView` = `{kind, slug, matched, model, model_family, entries[]}`
(`commands/workflow/profile_prompt.go:34-41,199-244`). The `model`/`model_family` come straight from
the profile (`decodeProfileModelRoute`, `:97-113`), and `entries` is the base-first, scope-resolved
`prompt_files` composition with per-file precedence: absolute → repo-local `.agents/...` →
repo-local `.agents/prompts/` → shared-home `prompts/` → unresolved (`resolvePromptRef`, `:130-171`).
Repo-local committed files win over the shared-home starter, so a project overrides the product base
by dropping a same-named file under `.agents/prompts/`. This is the seam every dispatcher calls so the
worker, the orchestrator, and the emitted swarm YAML all resolve **the same merged prompt**
(`:28-33`). The full-loop's `profile_resolve` stage drives exactly this: `da config relevance` for
topology/lenses, then `da --json workflow resolve-prompt` per verifier/lens slug, and it **refuses a
matched stage with empty model or model_family** (`profile-driven.swarm.yaml:12-24`). This is O1/O9's
Layer-2 requirement in miniature — the projection is generated from the IR, not authored.

**RULE-7 cross-family gate binding.** The blocking adversarial review MUST run on a different model
family than the executor; same family both sides ⇒ review invalid (**RULE 7**,
`falsification-review-rubric.md:23-25`). The full-loop binds diversity to the **named**
`cross-harness-adversarial` lens, never to a numeric slot index: `profile_resolve` partitions that
slug into `cross_family_lens` and requires `cross_family_lens.family ≠ executor/default.family`
(`profile-driven.swarm.yaml:17-20`); `review_cross_family` requires `slug=cross-harness-adversarial`,
`family=gpt`, family-differs-from-executor, and rejects on any BLOCKER/HIGH (`:103-113`). This is O6 —
a `review-gate` stage kind that projects to each harness's native gate while satisfying the
cross-family rule (**T-d2**; `cx-craft-autoreview-*`, `cursor-sonar-mcp-discipline`,
`cc-mcp-quality-gates-inloop`; DA-C10/DA-G3).

**Architect rules.**
- Express every stage as a typed `StageProfile` with an explicit `model` AND `model_family`; refuse to
  emit or dispatch a matched stage whose model or model_family is empty.
  [`agentsrc.go:654-677`, `profile-driven.swarm.yaml:12-24`]
- Resolve every stage prompt through `resolve-prompt` (base-first, scope-merged); NEVER inline
  duplicate prompt prose into the projection. [`profile_prompt.go:28-41,130-171`]
- Preserve prompt precedence: repo-local `.agents/prompts/` overrides the shared-home starter for the
  same filename. [`profile_prompt.go:130-171`]
- Bind cross-family diversity to the named `cross-harness-adversarial` lens and assert
  `reviewer.family ≠ executor.family`; NEVER bind it to a numeric reviewer slot or assumed list order.
  [RULE 7 `falsification-review-rubric.md:23-25`, `profile-driven.swarm.yaml:17-20,103-113`]
- Keep `model_family` open-ended (identity comparison, no closed vendor allowlist), so a new tier is a
  config edit, not a code change. [`agentsrc.go:666-669`, O6]

---

## 3. Lifecycle + recovery contracts

**Delegation lifecycle.** A task moves `fanout → bundle → worker → merge-back → closeout`, all
`da`-owned. Fanout materializes a contract + a base-resolved bundle
(`delegation.go:1279`, `base_resolution.go:18-25` layers a downstream task onto its dependency's open
PR branch); the worker writes only inside the bundle's authoritative write_scope; the parent authors a
schema-valid merge-back and runs closeout (`runWorkflowMergeBack` `:1455-1517`,
`runWorkflowDelegationCloseout` `:2266-2327`). This is the cross-harness delegation craft
(**T-d3**; `cursor-loop-worker-harness`, `cc-wave-parallel-delegation-agent`, `cx-mech-spawn-*`;
DA-C4/DA-C5) and its durability lesson: merge-back survives late worker failure — the parent wrote it
after confirming commit + verification when the worker env became inaccessible (DA-C5/DA-L4).

**Fold-back re-entry is bounded.** The inner pipeline's `target_count` is a hard iteration ceiling
(`profile-driven.swarm.yaml:6`, = 3). A retryable verifier/lens rejection re-enters the **executor
inside the same active delegation** — it does NOT fan the task out again
(`reconcile.swarm.yaml:25-34`). `FOLD-BACK` is the *terminal* result after that bounded budget is
exhausted: reconcile records each item via `da workflow fold-back create` (task-scoped or
plan-scoped), persists a failed merge-back, closes out `decision=reject`, and the canonical task
becomes `blocked` with its slot freed; a later explicit unblock/replan creates a fresh delegation
(`reconcile.swarm.yaml:25-34`). Bounded re-entry is why the loop converges instead of looping forever.

**Crash / stale-lock / fanout-refusal reconciliation.** Every failure mode routes back through
reconcile, never to abandonment (`reconcile.swarm.yaml:36-43`):
- **Crash / non-zero / missing inner exit** ⇒ recoverable lifecycle failure: record an idempotent
  fold-back with the exit/logs, persist the failed artifact, close out `reject`, free the slot. Never
  claim success, never leave an orphaned `in_progress` delegation (`reconcile.swarm.yaml:36-43`).
- **Stale driver lock** ⇒ `acquire_driver_lock` checks the recorded pid with `kill -0`; a dead owner's
  lock is recovered, a live owner's is refused (`bin/tests/omp-full-loop:68-85`).
- **Incomplete prior wave** ⇒ on startup `recover_incomplete_waves` reconciles any wave lacking
  `RECONCILED` — but refuses if a live OMP pid still owns a coord dir (`:206-232`).
- **Fanout refusal** ⇒ a failed `da workflow fanout` writes an explicit `GATE.md` FOLD-BACK for that
  task so an earlier successful sibling delegation is never stranded (`:282-290`).

These four are pinned by dedicated test scenarios: `failure` (crash still reconciles,
`omp-full-loop_test.sh:147-168`), `recovery` (stale lock + orphan wave recovered before new selection,
`:170-193`), and `fanout-failure` (one inner still runs, wave still reconciles, `:195-212`).

**SIGHUP co-termination lesson (T-b2).** Two OMP mega-sessions were killed ~2s apart with pending tool
calls — a single terminal/tmux restart taking the whole tree down (**T-b2**;
`omp-session-exit-distribution`). The driver encodes the fix: `set -m` gives each inner pipeline its
own process group so `Ctrl-C`/`TERM` co-terminates OMP *and* every agent it spawned, not just the
wrapper (`bin/tests/omp-full-loop:6-8`); the `EXIT` trap `cleanup_driver` TERMs each job's process
group and releases the lock (`:86-96`). This is O7's resilience mandate — sessions die three ways
(rate-limit walls **T-b1**, OS-signal co-termination **T-b2**, mid-turn cutoffs **T-b3**) and the
runtime must checkpoint before signal-class kills and treat each as resumable, not fatal.

**Architect rules.**
- Bound inner re-entry with `target_count`; a retryable rejection re-enters the executor inside the
  same delegation and NEVER re-fans an active task. [`profile-driven.swarm.yaml:6`, `reconcile.swarm.yaml:25-34`]
- Route crash / non-zero / missing-exit through reconcile as a recoverable lifecycle failure: record a
  fold-back, close out `reject`, free the slot — never claim success or orphan an `in_progress`
  delegation. [`reconcile.swarm.yaml:36-43`, DA-C5/DA-L4]
- Make the driver lock pid-aware: recover a dead owner's lock, refuse a live owner's, and reconcile any
  wave missing `RECONCILED` on startup unless a live pid still owns it.
  [`bin/tests/omp-full-loop:68-85,206-232`]
- On fanout refusal, write an explicit FOLD-BACK for that task so sibling delegations are not stranded.
  [`bin/tests/omp-full-loop:282-290`]
- Give each spawned pipeline its own process group and trap signals to co-terminate the whole tree;
  checkpoint before signal-class kills. [T-b2, O7, `bin/tests/omp-full-loop:6-8,86-96`]
- Cover crash, stale-lock, and fanout-refusal with explicit recovery tests before shipping a runtime.
  [`bin/tests/omp-full-loop_test.sh:147-212`]

---

## 4. Verification / review spine

**The spine as implemented.** The inner pipeline is a strict sequence: executor → up to 7 verifier
slots (each gated on the prior's PASS) → up to 4 routine review lenses (each gated on prior + all
verifiers passed, read-only) → the blocking cross-family lens → the evidence gate
(`profile-driven.swarm.yaml:26-127`). Cardinality is capped (verifiers ≤7, routine lenses ≤4) and
over-cardinality is a `BLOCKED` refusal (`:20-24`). Verifiers and reviewers NEVER mutate canonical
workflow state; only the `gate` stage pushes the owner-held PR, polls the app_type delivery gate, and
authors the merge-back draft (`:115-127`). Verification-then-review-then-gate is the ordering, and
each stage's route must equal its own declared model/family (`:42-46,78-86`).

**What transcripts prove is ACTUALLY used, cross-harness.** The evidence separates real discipline
from prescribed-but-dead prose:
- **Verification is real where tools persist (T-d1, high).** 0/38 codex edit-sessions skipped **both**
  git and tests; 25 ran both. Cursor workers ground on `workflow tasks && git status` and check
  `ReadLints` (`cx-craft-verify-01..08`, `cursor-git-workflow-grounding`,
  `cursor-readlints-verification`, `cc-mcp-quality-gates-inloop`; DA-G2/EFF-W4/BEH-M2). ⇒ O5: an
  in-session verification stage is non-optional per app_type.
- **Structured review verdicts are wired in-loop (T-d2, high).** `codex-auto-review` emits structured
  `{risk_level, user_authorization, outcome, rationale}` verdicts before escalation; CC uses
  SonarQube + code-review-graph MCP; cursor uses SonarQube MCP + `ReadLints`
  (`cx-craft-autoreview-01..08`, `cx-craft-sonar-*`, `cursor-sonar-mcp-discipline`; DA-C10/DA-G3). ⇒
  O6: a `review-gate` stage kind that binds to each harness's native gate. Negative controls confirm
  this generalizes to advisory/one-off use, not just loop-workers (`cursor-negctrl-refactor-for-sonar`,
  `negative-control-analysis.md` point 4).
- **Falsification-first is the review contract, not affirmative render.** ≥2 pre-registered
  falsifiable hypotheses, each *executed* (`refuted-the-work|survived|inconclusive`) not argued; null
  results first-class; a zero-refutation review is returned as not-performed
  (`falsification-review-rubric.md:5-25`). This is the methodology delta over the priors' affirmative
  case-study render (`methodology-deltas.md:155-177`).

**Prescribed-but-unused / unverifiable.** Cursor persists **no** `tool_result` (T-b5, high): tool
outcomes, exit codes, and errors are unrecoverable, visible only as narration — the ApplyPatch failure
control still exposed the failure only in prose (`cursor-gap-no-tool-results`,
`cursor-applypatch-context-failure`; `negative-control-analysis.md` point 6). So a review/verifier
signal that leans on cursor self-reported `## Result`/`verification` text is unverifiable: the coarse
substring scorer would rate it `high` while nothing in-transcript corroborates the work — OH-E3
**confirmed unsafe** ⇒ O11 (require an anchor + a real `tool_result`/verifier record). Correction- and
companion-attribution (OH-B3) is prescribed by the priors but untested locally — carried as
review-debt, not a live signal.

**Architect rules.**
- Emit an in-session verification stage on every implementing pipeline, per app_type; a verifier PASS
  gates the next stage, and no verifier/reviewer mutates canonical state. [T-d1, O5,
  `profile-driven.swarm.yaml:42-46,78-86`]
- Cap stage cardinality (verifiers ≤7, routine lenses ≤4) and refuse (BLOCK) on overflow rather than
  silently truncating. [`profile-driven.swarm.yaml:20-24`]
- Require structured review verdicts (risk/outcome/rationale) that bind to the target harness's native
  gate, not free-text "LGTM". [T-d2, O6, `cx-craft-autoreview-*`, `falsification-review-rubric.md:42-46`]
- Make review falsification-first: a verdict with zero executed refutation hypotheses is
  not-performed. [`falsification-review-rubric.md:5-25`]
- NEVER accept self-reported completion as a verification signal on a harness without persisted
  `tool_result`; require an anchor + real tool/verifier record. [T-b5, OH-E3, O11,
  `cursor-gap-no-tool-results`]

---

## 5. Cost mechanics

**Cache-read dominance is the load-bearing fact (T-c1 / H1 / H2, high, corpus-wide).** Re-sent cached
context is 89–99% of token volume in **every** telemetry-bearing harness: OMP `cacheRead` is 96–98% of
tokens and 62–69% of `$` across the four mega-sessions (`omp-cost-totals-*`,
`cost-cacheread-dominates-context`); codex median cached 88–89%; one CC turn recorded
`input=2 / cache_read=28,175` (`historical-hypotheses.md:10-21`). Productive work — output + reasoning
— is a *tiny slice*: codex median 12,070 tok = **1.26%** of total (max 5.14%), OMP ≈ 2–4%
(`historical-hypotheses.md:15-17`). The negative controls show this is **structural to long agent
context, not an artifact of the loop or the sampler** (`cx-cost-negctrl-01..12` show cache/input
63–98%, mean ~85%; `negative-control-analysis.md` point 1).

**Productive-token accounting (O3).** Raw `total_tokens` overstates cost ~50× on codex. Token-volume
and token-cost axes MUST be computed on the *productive* figure (output + reasoning + non-cached
input), reporting raw + cache-adjusted (O3; `pareto-measurement-rubric.md:17-18`). Dollar attribution
is gappy and route-dependent — only OMP + copilot record cost, CC/codex are token-only, cursor records
nothing, and some OMP provider routes bill `$0` (gpt-5.4 turns $0.00 vs anthropic/codex billed) — so
any cross-harness `$` comparison reconstructs CC/codex from `tokens×published-rate` flagged
`[INFERENCE]` and never silently mixes with recorded OMP cost (T-c2, O4;
`omp-cost-cursor-routed-zero-dollar`, `pareto-measurement-rubric.md:18`).

**Fixed per-request tax (T-c4 / H4).** Tool defs + system prompt are large and volume-fixed: 67% of a
trivial copilot task's 24,342-token context was tool definitions (`copilot-tooldef-fixed-overhead`),
and this floor is task-complexity-independent (`negative-control-analysis.md` point 2).

**Design rules the numbers force (the falsifiable priors):**
- **Context reuse gates volume, not the model (H1).** At a fixed snapshot an executor swap moves token
  volume ≤ ~4% (productive fraction ≤4%; cache volume set by context size). *To move volume you must
  change the pipeline (compaction / fewer re-reads), not the model.* (`historical-hypotheses.md:84-92`)
- **$ savings are cache-read-rate-bound (H2).** The outcome-addressable `$` swing (input+output+
  cacheWrite) is ≤ ~38% of total `$`; the remaining ≥62% re-prices purely by the swapped tier's
  cache-read rate ratio (`historical-hypotheses.md:94-103`).
- **Stage granularity trades against fixed tax (H4).** Cheap-tier fractional savings *rise with task
  length*; the shortest tasks yield the least because the fixed tool-def block is model-priced but
  volume-fixed. Don't route a trivial task through a heavy tool-def context (H4, O13;
  `historical-hypotheses.md:116-123`).
- **Accuracy risk is localized to the productive fraction (H5).** A weaker model degrades accuracy
  only in proportion to a task's productive share; low-productive stages (context-shuffling, review)
  are near-zero-risk cheap-route targets (`historical-hypotheses.md:125-136`).
- **The review/verifier stage is the highest-leverage cheap route (H6).** Review turns carry very low
  productive tokens (220–720) — a classification, not a generation, workload — so routing review to a
  cheap tier saves at near-zero accuracy risk, provided the cross-family gate is preserved (RULE 7)
  (`historical-hypotheses.md:138-147`).

All effect sizes are hypothesis-only from observational data; a frontier is a **live** paired-contrast
artifact (`pareto-measurement-rubric.md:42-53`), never claimed from history.

**Architect rules.**
- Normalize every token-cost/volume axis on productive tokens (output+reasoning+non-cached input);
  report raw + cache-adjusted. NEVER price a stage on raw `total_tokens`. [T-c1, O3,
  `pareto-measurement-rubric.md:17-18`]
- Reconstruct any missing-`$` harness from `tokens×published-rate`, flag it `[INFERENCE]`, and never
  mix it silently with recorded cost; treat a `$0` provider route as suspect. [T-c2, O4,
  `omp-cost-cursor-routed-zero-dollar`]
- To reduce token volume, change the pipeline (compaction, fewer re-reads), not the executor model.
  [H1, `historical-hypotheses.md:84-92`]
- Prefer coarse stage granularity on short tasks: don't route a trivial task through a full tool-def
  context; cheap-tier savings scale with task length. [H4, T-c4, O13,
  `copilot-tooldef-fixed-overhead`]
- Route cheap tiers at low-productive stages first (review/verify), holding the executor at baseline,
  and keep the cross-family gate. [H5, H6, C6, `historical-hypotheses.md:125-147`]
- NEVER assert a cost/efficiency frontier from historical rows; require CI-backed paired live
  contrasts. [`pareto-measurement-rubric.md:42-53`]

---

## 6. Per-harness capability matrix

A projection MUST be **per-harness** because the loop mechanism and telemetry differ radically
(T-a1/T-a2/T-e1/T-e2, O1/O2; `synthesis-report.md:48-54`). Four archetypes:

| harness | archetype | drives `da workflow`? | orchestration primitive | telemetry axes it can feed |
|---|---|---|---|---|
| **omp** | `da`-CLI-native | **yes (heavy)** | prescribed-skill-driven | tokens, `$` (USD), wallclock (~80%), model (`model_change`), `tool_result` |
| **claude-code** | `da`-CLI-native | **yes (heavy)** | prescribed-skill-driven (`Task`) | tokens, wallclock (partial `durationMs`), model, `tool_result`; **no `$`** |
| **codex** | artifact-reader / native-orchestration | **never** (reads `.agents/workflow/*` as context) | own primitives (`multi_agent_v1`, full-auto `approval_policy=never`) | tokens (89.6%), model, `tool_result` (`function_call_output`); **no `$`** |
| **cursor** | contract-native, no CLI | native loop-worker/orchestrator **without** the CLI | native `Task` | **none** of tokens/`$`/wallclock/model/timestamps; **0 `tool_result`** |
| **copilot** | minimal | n/a (smoke) | — | tokens, credits (not USD), wallclock, model |

Source: `synthesis-report.md:48-54` capability rows, corroborated by every cost/gap item.

**Consequences the projection layer must encode:**
- **A single emitted projection cannot serve all four (O1).** OMP + CC drive the CLI directly; codex
  never does — it reads workflow artifacts and orchestrates via its own primitives; cursor runs the
  loop-worker/orchestrator contract natively with no CLI (T-a1; `cc-workflow-cli-drives-taskstate`,
  `cx-mech-loopbridge-01..08`, `cursor-loop-worker-harness`). File the transformer requirement as a
  proposal parallel to `omp-platform-handling.md` (O9).
- **The Pareto cell must carry a harness-capability mask (O2).** A cell is `model_family × task_class
  × cache_regime × retry_regime`; **hard-exclude cursor from tokens/cost/wallclock/model axes** — it
  records none, and the gap is a format property invariant to workflow-vs-advisory use (T-b5,
  `negative-control-analysis.md` point 6). Never score a cell on an axis its harness cannot supply.
- **Mechanism/orchestration findings are workflow-scoped, cost findings are corpus-wide.** Cost/
  resilience outcomes (O2/O3/O4/O7/O13) generalize past the workflow sample; mechanism/orchestration
  projection (O1/O9) MUST be gated on "is this a workflow session?" and never applied to advisory chat
  (`negative-control-analysis.md` points 5–6, bottom line).
- **Record bridge origin (O12).** CC→codex is a real recurring bridge (12 codex sessions with
  `originator='Claude Code'`); attribute bridged cost/outcome back to the spawning orchestrator so
  roll-ups don't double-count (`cx-mech-origin-cc-01..03`).

**Architect rules.**
- Emit a distinct per-harness loop projection from one profile IR; NEVER assume one swarm shape serves
  every harness. [O1, T-a1/T-e2, `synthesis-report.md:48-54`]
- Attach a telemetry-capability mask to every Pareto cell and hard-exclude a harness from any axis it
  cannot record — cursor from tokens/`$`/wallclock/model unconditionally. [O2, T-e1, T-b5]
- Gate mechanism/orchestration projection on "is this a workflow session?"; apply cost/resilience
  rules corpus-wide. [`negative-control-analysis.md` points 5–6]
- For codex, project a *no-CLI, artifact-reading* driver (it orchestrates via its own primitives), not
  a `da workflow` caller. [T-a1, O1, `cx-mech-loopbridge-01..08`]
- Record `origin_harness` on bridged work so cross-harness cost roll-ups don't mis-attribute. [O12,
  `cx-mech-origin-cc-01..03`]

---

## 7. Anti-patterns

Anchored failure modes an emitted pipeline must design against:

- **Hand-editing emitted swarm YAML (O1 gap).** The full-loop swarm projections are hand-written today
  (`.agents/workflow/runtime/full-loop/profile-driven.swarm.yaml`, `reconcile.swarm.yaml`) — the named
  Layer-2 gap (O1; `transcript-analysis-and-pipeline-craft.plan.md:28-32`). Once they are emitted from
  the IR, a hand-edit is drift: it diverges the running pipeline from `stage_profiles` and defeats the
  single-source routing of §2. Regenerate from the IR; treat the YAML as a build artifact, not a
  source. [O1]
- **Rate-limit walls mid-loop (T-b1).** Codex drove 14 windows ≥90%; OMP hit a `usage_limit_reached`
  zero-output turn (`cx-fail-ratelimit-01..14`, `omp-usagelimit-zero-output-failure`). It appears even
  in scratchpad/review control sessions (`cx-cost-negctrl-09`, `negative-control-analysis.md` point 3).
  A loop that treats a rate-limit wall as a hard crash strands slots; treat it as a first-class,
  resumable stop condition (T-b1, O7). [T-b1, O7]
- **Unverifiable self-reported completion (cursor T-b5 / OH-E3).** Cursor emits `## Result`/
  `verification` narration with **zero** `tool_result`; a coarse substring scorer keys on those words
  and rates the work `high` while nothing corroborates it — OH-E3 confirmed unsafe
  (`cursor-completion-reports`, `cursor-gap-no-tool-results`). Never gate an advance on
  self-report; require an anchor + real tool/verifier record (O11). [T-b5, OH-E3, O11]
- **Stale-status drift.** "Status said done" is not auditable: the priors had to add a
  `completed-with-doc-drift` verdict class and an evidence-precedence audit (DA-C2), and canonical
  state must win over stale checkpoints (T-d4). The reconcile stage counters this by re-reading task/
  delegation/PR/slots/eligible state before **every** mutation and re-running `slots`+`eligible` after
  every canonical write, blocking the next wave on any ambiguous state
  (`reconcile.swarm.yaml:12-16,51-56`). Never advance on cached status. [DA-C2, T-d4,
  `reconcile.swarm.yaml:51-56`]
- **Numeric-index diversity binding.** Binding the cross-family gate to "reviewer slot 4" or an
  assumed lens order breaks silently when the list changes; bind to the named
  `cross-harness-adversarial` slug and assert family inequality (§2). [RULE 7,
  `profile-driven.swarm.yaml:17-20`, `full-loop-orchestration-runtime.plan.md:25`]
- **Cross-scope worker dispatch.** Dispatching workers into overlapping write_scope shatters the
  disjoint-slice invariant; the `max_batch` conflict computation and the asserting-test-scope REFUSE
  gate exist to prevent it (`plan_task.go:1197-1231`, `delegation.go:2339-2359`). Never widen a
  bundle's scope across packages to satisfy a stray test. [`delegation.go:2339-2359`]

**Architect rules.**
- Treat emitted swarm/pipeline YAML as a build artifact; regenerate from the IR and refuse hand-edits.
  [O1, `transcript-analysis-and-pipeline-craft.plan.md:28-32`]
- Make rate-limit and usage-limit walls first-class resumable stop conditions, not crashes. [T-b1, O7,
  `omp-usagelimit-zero-output-failure`]
- Never gate a transition on self-reported completion; require an anchor + real tool/verifier record.
  [T-b5, OH-E3, O11]
- Re-read canonical state before every mutation and re-check slots/eligible after every write; block on
  ambiguity; never advance on cached status. [DA-C2, T-d4, `reconcile.swarm.yaml:51-56`]
- Bind diversity gates to named slugs with asserted family inequality, never to numeric slot order.
  [RULE 7, `profile-driven.swarm.yaml:17-20`]
- Enforce write-scope disjointness at dispatch (`max_batch`) and refuse cross-package scope widening.
  [`plan_task.go:1197-1231`, `delegation.go:2339-2359`]

---

## Consumed-outcomes map

| section | primary synthesis outcomes | supporting themes / hypotheses / priors |
|---|---|---|
| 1. Deterministic loop skeleton | O1 | T-a1, T-a2, T-d4; DA-C1, DA-C2, DA-C3 |
| 2. Stage/profile/model routing | O1, O6, O9 | T-a1, T-a2, T-a3, T-d2; RULE 7 |
| 3. Lifecycle + recovery contracts | O7, O8 | T-a2, T-b1, T-b2, T-b3, T-d3; DA-C4, DA-C5, DA-L4 |
| 4. Verification/review spine | O5, O6, O11 | T-d1, T-d2, T-b5; OH-E3; DA-G2, DA-G3, DA-C10 |
| 5. Cost mechanics | O2, O3, O4, O13 | T-c1, T-c2, T-c4; H1, H2, H4, H5, H6 |
| 6. Per-harness capability matrix | O1, O2, O9, O12 | T-a1, T-e1, T-e2, T-b5 |
| 7. Anti-patterns | O1, O7, O11 | T-b1, T-b5, T-d4; OH-E3; DA-C2; RULE 7 |
