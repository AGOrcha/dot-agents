# Loop Orchestration: Slots + N-Plan Driving + Per-Task Pipeline Composition

> Scope: how `da`'s **existing** machinery — workflow **slots** / in-flight worker
> lifecycle (`layered-pr-fanout`), **N-plan cross-plan driving** (`workflow-parallel-orchestration`),
> and **staged per-task pipeline** (`loop-agent-pipeline`) — composes into the profile-driven
> inner-loop swarm. This is a design-integration map, NOT a reinvention: every mechanism below
> already exists in spec + `commands/workflow/**`.
>
> Companion digests in this directory (reference, do not re-derive):
> - `da-workflow.md` — exhaustive `da workflow` CLI/engine reference (cited as **[daw §N]**).
> - `experiences-lessons.md` — the state-drift lessons that make RULE 10 load-bearing.
> - `loop-metaloop-foldback.md` (companion, `MetaLoopFoldbackDoc`) — owns proposal/observation
>   production + fold-back routing (its §1.4 classification/`routed_to`, §2 routing into running
>   tasks/plans); this digest only touches fold-back at the composition boundary (§3.1).
>
> Sources cited `path:line`. Spec §§ are relative to the named `design.md`/`decisions.md`.
> **Installed binary is `da 0.4.2`**; the swarm BUILDS `da` from source in the slice worktree
> and uses that HEAD binary (`CONVENTIONS.md:33-35`), so HEAD-only surfaces (`slots`, `eligible`
> conflict detection, layered base-resolution) ARE available at runtime. Binary-vs-repo
> divergences flagged inline.

---

## 1. Workflow Slots — in-flight worker/task budget

### 1.1 `max_parallel_tasks` — the capacity-derived clamp

The slot budget is **capacity-derived when unset**, not a fixed constant:
`clamp(NumCPU - 2, 2, 16)` — reserve 2 cores for orchestrator+OS, floor 2 (small machines
still parallelize), ceiling 16 (large machines do not oversubscribe). An explicit preference
override may exceed the auto-ceiling up to **32** (a typo guard, not a capacity policy). This
supersedes the former fixed default of 7.

- Spec: `layered-pr-fanout/design.md:224-237` (§2.9); `:562-577` (§5.2 concurrency knobs table).
- Code: `commands/workflow/eligible_accounting.go:277-279` (`defaultMaxParallelTasks = clampParallelTasks(runtime.NumCPU() - parallelTasksCoreReserve)`); constants `parallelTasksCoreReserve = 2`, `minMaxParallelTasks = 2`, `maxMaxParallelTasks = 16` at `:260-271`; `clampParallelTasks` at `:284-292`.
- Override ceiling: `commands/workflow/prefs.go:84-88` (`maxConfigurableParallelTasks = 32`), validated in `applyMaxParallelWorkers` (`prefs.go:306-313`, range `1..32`).

**⚠ Naming divergence (spec vs binary) — swarm-critical.** The spec renames the concept
`max_parallel_workers → max_parallel_tasks` (§2.9, §5.2). But the **on-disk preference key is
still `execution.max_parallel_workers`** in both 0.4.2 and HEAD code (`prefs.go:78`,
`preferenceKeyExecutionMaxParallelWorkers = "execution.max_parallel_workers"`), backed by the Go
field `WorkflowExecutionPrefs.MaxParallelWorkers` (`prefs.go:54`). So the CLI/pref surface the
swarm actually sets is `max_parallel_workers`; `max_parallel_tasks` is the spec/ledger *display*
name (`slots` output labels it `max_parallel_tasks`, `eligible_accounting.go:174`).

**⚠ One pref field, two different unset-defaults.** The same `MaxParallelWorkers` pref field
feeds two distinct resolvers with **different fallbacks when the pref is absent**:
- Slot budget (`resolveMaxParallelTasks`, `eligible_accounting.go:253-258`): unset →
  `defaultMaxParallelTasks()` = the capacity clamp above.
- Eligible batch limit (`resolveEligibleEffectiveLimit`, `plan_task.go:1393-1399`): unset →
  **`1`** (serialized). `--limit N` overrides per-call.

So on a machine with the pref unset, `slots` reports (e.g.) a budget of 14 while
`eligible` returns a max_batch capped at 1. The swarm MUST set
`execution.max_parallel_workers` explicitly to get parallel eligible batches — and that same
value then also raises the slot ceiling. (`workflow-parallel-orchestration/design.md:315-318`
Q2 left the eligible default at 1 deliberately; LPF §2.9 governs the *slot* default.)

Older WPO text (`workflow-parallel-orchestration/design.md:116-129`, §3.5) describes range
`1–8` / default `1` — **superseded** by LPF §2.9 (`1..32`, capacity-derived). Trust LPF for the
slot budget.

### 1.2 Per-status slot accounting (which statuses hold / free a slot)

Slot accounting is **per task, gated on status** (and on the `awaiting_review` sub-status).
Table (LPF §2.8 `design.md:197-222`; enforced in `eligible_accounting.go:39-57`):

| Status | Counts against `max_parallel_tasks`? | Predicate |
|---|---|---|
| `pending` | No | — |
| `in_progress` | **Yes (holds)** | `countsAgainstParallelTasks` |
| `awaiting_agent_review` | **Yes (holds)** — lens dispatch is bounded, can bounce back to `in_progress` in minutes | `countsAgainstParallelTasks` |
| `awaiting_owner_review` | **No (frees)** — human-review latency is unbounded; slot reclaimed for next layer | `freesSlot` |
| `blocked-on:<ref>` | **No (frees)** — no active compute; tracked in a *separate* `blocked` bucket (§3.4.3) | `freesSlot` + `isBlockedOnStatus` |
| `blocked` / `completed` / `cancelled` | No | terminal bucket |

- `countsAgainstParallelTasks(s) = (s == in_progress || s == awaiting_agent_review)` — `eligible_accounting.go:45-47`.
- `freesSlot(s) = (s == awaiting_owner_review || isBlockedOnStatus(s))` — `:55-57`; `blocked-on:` prefix at `:29-37`.
- Rationale (§2.8): `awaiting_agent_review` holds because the lens budget is bounded and rework re-enters `in_progress` quickly; `awaiting_owner_review` frees because owner latency is unbounded and the slot must be reclaimable for the next layer. Freeing on `awaiting_agent_review` would let the pool oversubscribe when lens dispatch is slow (rejected alternative).

### 1.3 Slot lifespan across the in-flight worker lifecycle

A slot is occupied from **impl-start until terminal**, with the `awaiting_owner_review`
exception (LPF §5.1 `design.md:549-560`). Full lifespan, mapped to the state machine
(`design.md:283-296` §3.1, edges §3.2 `:300-312`):

```
pending                         (free)
  → in_progress                 ── ACQUIRE slot ──►  worker implements
      → awaiting_agent_review    HOLDS slot          verifier green + PR open (§2.3); lens reviewers run
          → awaiting_owner_review FREES slot          lens accept; next layer can start here
              → completed        (free)              maintainer merges → downstream fast-forwards (§7.1)
              → in_progress      ── RE-ACQUIRE ──►    maintainer "request changes" bounce (§7.4, §2.8:211-213)
              → blocked          (free)              maintainer closes PR → cascade-block downstream (§2.6, §7.2)
          → in_progress          HOLDS (already held) lens reject / verifier bounce (§3.2:307, §6.2)
```

- The **bounce-back re-acquires** rule: `awaiting_owner_review → in_progress` re-acquires a slot, or queues if at `max_parallel_tasks` capacity (§2.8 `design.md:211-213`; §5.4 `:618-627`; §7.4 `:696-701`; state edge `design.md:309`).
- `awaiting_agent_review → in_progress` (lens reject) does **not** re-acquire — the task already holds the slot (edge note `design.md:307`).
- Force-rebase (SHA1 set-diff, §2.7) regresses the PR-holding task to `in_progress` and enqueues downstream `rebase-only` iterations (§7.3 `design.md:687-694`).
- Transition ownership is single-dispatcher (`transitions.go:34-42`, [daw §4]): `ownerVerifier` owns `in_progress → awaiting_agent_review`; `ownerLensGate` owns the agent-review edges; `ownerPollDetector` owns all owner-review-side edges + force-rebase, driven by `event.pr.*` envelopes (NOT a `gh` shell-out).

### 1.4 The `slots` ledger — how the loop reads the budget

`da workflow slots [--plan <ids>] [--json]` renders the live ledger
(`runWorkflowSlots`, `eligible_accounting.go:294-316`; command tree [daw §1.2]):

- Buckets: `occupied` (in_progress + awaiting_agent_review), `awaiting_owner` (freed, review backlog), `blocked` (freed; §3.4.3 pathology bucket — **always shown even at 0** so an all-blocked DAG is not read as idle), `pending`, `terminal` (`bucketForStatus`/`SlotLedger`, `eligible_accounting.go:99-152`).
- `Available = MaxParallel - Occupied`, floored at 0 (`computeSlotLedger:157-166`). This is the number of new tasks the loop MAY start right now.
- JSON shape: `{occupied, awaiting_owner, blocked, pending, terminal, max_parallel, available}` (`SlotLedger`, `:102-112`).
- Only **active** plans contribute; a corrupt `TASKS.yaml` aborts the ledger rather than understating occupancy (`planTaskStatuses:217-248`) — the loop must not silently over-dispatch.

**Loop respects the budget** by: read `slots` → start at most `Available` new in-flight tasks
this tick; when `Occupied >= MaxParallel`, queue. Because `awaiting_owner_review` frees the
slot, the maintainer's review queue is **not** the rate limit (§1.2 goal 3, §5.4:624-627) — the
loop keeps `Available` slots busy with the next layer while owner-review tasks wait.

---

## 2. Driving N Plans / all tasks

### 2.1 Cross-plan eligibility (`eligible` / `next`)

`workflow eligible` is the machine-facing, **session-level, cross-plan** counterpart to
`workflow next` (WPO §3.1 `design.md:58-72`). Both call the single source of truth
`selectAllEligibleTasks(projectPath, planFilter)` (`plan_task.go:1899`; [daw §6.1-6.2]); `next`
returns `[0]` after priority sort (WPO §3.2/§3.6), `eligible` returns the full annotated set +
`max_batch`. Both accept `--plan <id>,<id>,...` (comma-sep; unknown id → error).

Eligibility criteria — identical for both (WPO §3.3 `design.md:84-92`;
`collectEligibleTasksForPlan`, `plan_task.go:1977-2014`):
- Plan `status == "active"`,
- Task `status ∈ {pending, in_progress}`,
- No incomplete dependencies (per §2.2 predicate),
- Task id ∉ active-delegation set (`loadActiveDelegationTaskSet`: a `pending`/`active`
  delegation contract whose `ParentTaskID` matches excludes the task).

`eligible --json` shape: `{eligible_tasks[], max_batch[], conflict_graph, total_eligible,
max_parallel, draft_plans[]}`; each task carries `write_scope`, `conflicts_with`, `priority`,
`has_evidence`, `evidence_confidence`, `write_scope_declared` ([daw §6.6]; WPO §4.1
`design.md:218-236`).

### 2.2 Dep gating — `depSatisfiesDownstream` across plans

The **load-bearing layered change**: an upstream satisfies a downstream dep when it reaches
`completed` **OR `awaiting_owner_review`** — i.e. downstream unblocks the moment the upstream PR
is open, green, and lens-accepted, **before** the maintainer merges (LPF §1.2 goal 1, §2.4,
§6 verifier/lens split):

```go
// commands/workflow/eligible_accounting.go:72-74
func depSatisfiesDownstream(s string) bool {
    return s == TaskStatusCompleted || s == TaskStatusAwaitingOwnerReview
}
```

`in_progress` and `awaiting_agent_review` do **not** satisfy (impl/lens may still bounce);
`pending`, `blocked`, `blocked-on:*`, `cancelled` do not satisfy — **no block-on-block cascade**
(§3.4.6 `design.md:392-401`: a dep in `blocked-on:*` leaves the dependent `pending`, no implicit
cascade).

**Cross-plan resolution** (`incompleteCanonicalDependenciesCrossplan` / `crossPlanDepIncomplete`,
`plan_task.go:1786-1834`; [daw §6.3]; WPO §4.3 `design.md:263-267`):
- Bare dep id (no `/`) → looked up in the **local** plan's `statusByID`.
- `<plan>/<task>` → `loadCanonicalTasks(refPlan)`, falling back to
  `.agents/history/<refPlan>/TASKS.yaml` (a dep on an *archived* plan's completed task still
  resolves).
- Unresolvable cross-plan plan/task → treated as **incomplete** (gates) + a warning is emitted.
- **Silent trap:** a bare same-plan dep id that does not exist yields `statusByID[dep] == ""` →
  never satisfies → permanently gated **with no warning** (only cross-plan misses warn). Typo'd
  same-plan deps silently wedge dependents ([daw §6.4.3]).

### 2.3 Wave / layer construction

Two complementary surfaces:
- **Static** wave shape: `da workflow plan schedule <plan>` — Kahn BFS topological wave
  schedule ([daw §1.2:75]; WPO §8 `design.md:358-359`). Design-time companion.
- **Runtime** wave: `eligible`'s `max_batch` = the largest greedy subset of eligible tasks with
  **zero pairwise write-scope conflicts**, ordered by priority (WPO §3.4 `design.md:96-114`;
  `computeWriteScopeConflicts` + `max_batch`, [daw §6.6]). One `max_batch` = one wave.

**Parallel-wave trigger** (WPO §3.7 `design.md:139-151`): launch a wave iff
`max_batch length > 1` **AND** no active delegation bundles exist for the scoped plan set. The
loop waits for **all** active delegations to close before re-querying `eligible` and launching
the next wave — because a running worker may create intermediate files outside its declared
scope, so a new wave launched over open delegations risks conflicts the static graph can't see.
(Automatic wave advancement is orchestrator-driven, not a CLI command — WPO §5 `design.md:300-301`.)

### 2.4 Base-branch / lineage resolution for downstream off in-flight PRs

Because a downstream task can go eligible while its upstream is still `awaiting_owner_review`
(un-merged), its branch must be cut from the upstream's **PR head branch**, not `master`. This
is what `da workflow fanout` computes (LPF §4 `design.md:418-451`; `fanoutResolveBase`,
[daw §7.1 step 3]):

```
inputs: task_id, depends_on (may include "<plan>/<task>"), write_scope, in_flight_tasks{status,pr_branch,pr_number}
1. all deps ∈ {completed, awaiting_owner_review} AND merged to master  → base = "master"
2. exactly one dep ∈ awaiting_review (any sub-status)                  → base = that dep's pr_branch
3. multiple deps in awaiting_review on distinct branches:
     v1: REFUSE, surface the conflict set, require --base-branch       (§2.5)
     v2 (future): lineage query via graph-backend adapter → propose base or refuse
```

Bundle records the decision under `scope` (additive, backward-compatible, `design.md:453-466`):
`base_branch`, `base_pr`, `base_task`. Omitted `base_branch` → worker defaults to `master`
(today's behavior). `base`/`ready` are domain-agnostic seams (git is adapter #1); `--base-branch`
is the git adapter's spelling of the generic `--base-ref` (§2.10 `design.md:239-275`, §4.4
`design.md:476-543`). Fanout flags include `--base-branch` ([daw §7.1]).

Multi-dep refusal is a **non-zero exit** the loop must handle: pass `--base-branch` explicitly or
sequence the deps (§4.1 step 4b; Done Criteria §9.4 `design.md:753-756`).

### 2.5 Cascade-block on rejection

When a maintainer **closes a PR without merging**, the upstream task → `blocked` with a
"base rejected" fold-back artifact, and every downstream task currently in `awaiting_review` or
`in_progress` that branched off that PR **cascade-blocks** (LPF §2.6 `design.md:163-183`;
§7.2 `:679-685`; state edge `* → blocked (cascade)` `design.md:312`, owner = poll detector,
precondition = downstream was branched off the rejected PR). Unblock is **explicit only**:
replan onto a new base OR re-open the upstream PR — no cascade auto-lifts. Silent auto-rebase
onto `master` is a rejected alternative (would drop the upstream contract changes downstream
depended on). Lens rejection does **not** cascade unless the rework changes the public contract
(§6.2 `design.md:659-660`).

---

## 3. Composition with the profile-driven per-task pipeline

Each ready task, once selected across all active plans, is driven through **its `app_type`
profile's staged pipeline** — the `loop-agent-pipeline` staged runtime, realized in
`profile-driven.swarm.yaml`. The two layers compose along a **shared slot budget**.

### 3.1 The per-task staged pipeline (one slot's worth of work)

`loop-agent-pipeline` stages (plan-iter.2 §5.1 `design.md:549-556`):
`impl → verify → review-agent → review-owner → done`. In the swarm YAML
(`profile-driven.swarm.yaml`):

- `profile_resolve` — reads the task's `app_type` (`da config relevance --filter topology/lenses`),
  emits `COORD/profile.json {app_type, verifier_sequence[], lens_set[], lens_concurrency,
  fold_back_feedback?}`; verifies `depends_on` complete in TASKS.yaml (RULE 10: `eligible`
  advisory); carries a prior iteration's `GATE.md` reject feedback forward (yaml:22-34).
- `executor` — implements `write_scope`, addresses `fold_back_feedback` first, focused tests,
  commits (no AI trailer) (yaml:36-45). ⇒ maps to task `in_progress` (**holds a slot**).
- `verify_1..3` — fixed 3 verifier slots run `verifier_sequence[0..2]` (`resolve-prompt --kind
  verifier --slug <it>`) or SKIP; each gated on the prior (yaml:47-49). Terminal green ⇒
  verifier owns `in_progress → awaiting_agent_review` (LPF §6.1 `design.md:636-646`; still
  **holds a slot**).
- `review_1..4` — fixed 4 lens slots run `lens_set[0..3]` (`resolve-prompt --kind reviewer`),
  read-only, gated by `lens_concurrency` (yaml:51-58); slot 4 is a **different model** (gpt-5.4)
  for cross-harness adversarial. Lens accept ⇒ lens-gate owns `awaiting_agent_review →
  awaiting_owner_review` (LPF §6.2 `design.md:648-660`; **frees the slot**).
- `gate` — if impl DONE ∧ all present verifiers PASS ∧ all present lenses APPROVE: push, open PR,
  poll CI to the app_type's gate, author merge-back, write terminal `COORD/READY.md`; else write
  `COORD/GATE.md FOLD-BACK` with per-blocker reasons (executor reads next iteration). NEVER
  merges (owner-held), NEVER mutates the board (yaml:60-72). ⇒ the `awaiting_owner_review`
  boundary; `READY` = ready for owner merge, `FOLD-BACK` = `in_progress` bounce.

Fold-back → executor is `mode: pipeline` with bounded `target_count: 3` (yaml:5-8,17-18):
iteration k reads k-1's gate verdict; on FOLD-BACK the executor re-enters with reject feedback;
on READY later iterations no-op. This is the §3.1 `awaiting_*_review → in_progress` bounce
realized as a bounded fix-loop. (Deep fold-back *artifact* routing — proposals/observations,
`fold-back create/update`, slug identity per decisions.1 §D2/D2.a — is the companion
`MetaLoopFoldbackDoc` digest's territory.)

### 3.2 Shared slot budget across the two layers

Two orthogonal concurrency knobs (LPF §5.2 `design.md:562-577`):
- `max_parallel_tasks` (pref `execution.max_parallel_workers`) — **how many tasks hold a slot
  concurrently**. This is the swarm-wide budget: N in-flight tasks = N slots.
- `max_parallel_within_task` — **how many stages run concurrently inside one task**; unset →
  derived from the bundle's `evidence_policy`. Lens dispatch is **parallel by default** (§5.3
  `design.md:579-606`): all lenses in `lens_set` run concurrently inside `awaiting_agent_review`;
  sequencing is opt-in via `evidence_policy.lens_concurrency = parallel|sequential|gated` +
  `lens_chain` / `lens_tier_gate` / `lens_routing`.

So the composition is a **two-level budget**: the N-plan loop (§2) picks up to `Available`
(`slots`) ready tasks across all active plans and fans each out; each task's profile pipeline
(§3.1) then runs its verifier/lens stages under `max_parallel_within_task`. A task occupies its
outer slot from `executor`-start through `awaiting_agent_review`, and **frees it at
`awaiting_owner_review`** — at which point (a) its downstream deps unblock (§2.2) and (b) the
freed slot lets the next layer's task start (§5.4 `design.md:624-627`). The per-task pipeline's
`gate=READY` is exactly the event that both frees a slot and satisfies downstream deps.

---

## Pipeline integration

Concretely, how `profile-driven.swarm.yaml` (driven by `Main`) honors da's machinery. **The
swarm builds `da` from source in the slice worktree and uses that HEAD binary**
(`CONVENTIONS.md:33-35`) — `slots`, `eligible` conflict detection, and layered base-resolution
are all present at runtime (the installed 0.4.2 binary lacks `fanout --with-tests` /
`--skip-asserting-test-gate` per [daw §0]; `da run`, `kg build/update` are broken under 0.4.2
per `CONVENTIONS.md:38-39`; `--dry-run` is NOT side-effect-free in 0.4.2, `:40`).

**1. Honor the slot budget (concurrent in-flight tasks).**
- Set the budget once: `da workflow prefs set-local execution.max_parallel_workers <N>`
  (`prefs.go:306-313`; 1..32). This raises BOTH the slot ceiling and the `eligible` batch limit
  (§1.1). Unset ⇒ `slots` uses capacity clamp `clamp(NumCPU-2,2,16)` but `eligible` caps batch at 1.
- Each tick: `da --json workflow slots --plan <scope>` → read `available` (`= max_parallel -
  occupied`, `eligible_accounting.go:162-165`). Start at most `available` new in-flight tasks;
  when `occupied >= max_parallel`, queue. `occupied` counts `in_progress` +
  `awaiting_agent_review` only (`:45-47`); `awaiting_owner_review` and `blocked-on:*` have freed
  their slots (`:55-57`), so owner-review backlog never starves the next layer. Watch the
  `blocked` bucket (always rendered, `renderSlotLedger:177`) for the all-blocked pathology.

**2. Pick the next ready task across all active plans (when quiescent).**
- When no delegations are in flight for the scope: `da --json workflow eligible --plan <scope>
  --limit <N>` → use `eligible_tasks[]` + `max_batch[]`. `max_batch` is the parallel-fanout
  candidate wave; the parallel trigger is `max_batch>1 ∧ no active bundles` (WPO §3.7). For each
  task, export `TASK=<plan>/<task>` and launch a `profile-driven.swarm.yaml` pipeline instance
  (`profile_resolve` reads `$TASK`, yaml:10,22-34).
- **RULE 10 (advisory-only eligibility + serialized board writes):** `eligible`/`next`
  dep-gating is **advisory, not authoritative** — statuses drift (already-shipped PRs resurface;
  worktree-copy stale reads; silent missing-dep gates) per [daw §6.4] and
  `experiences-lessons.md` (`verify-task-status-vs-pr-history`, `reconcile-task-status-on-pr-merge`,
  `stale-local-checkout-mass-drift`). The loop MUST maintain its **own DAG** from `depends_on`
  (qualify cross-plan deps as `<plan>/<task>`), re-read canonical state fresh each wave, and
  **workers NEVER mutate the board** (`da workflow advance/closeout/merge-back/task update`) — the
  0.4.2 store races on concurrent mutation (`CONVENTIONS.md:41-43`). Workers only PRODUCE
  artifacts (COORD signals, merge-back drafts); `Main` reconciles the board **after** owner merge,
  serially. `profile_resolve` re-checks `depends_on` complete in TASKS.yaml itself rather than
  trusting `eligible` (yaml:30-31).

**3. Layer waves.**
- One `max_batch` = one wave. Launch the wave, then **wait for every pipeline instance in it to
  reach a terminal `COORD/READY.md` or `COORD/GATE.md FOLD-BACK`** before re-querying `eligible`
  (WPO §3.7 — no second wave over open delegations). Downstream tasks whose only unmet dep just
  hit `awaiting_owner_review` become eligible on the next poll (§2.2; Done Criteria §9.1
  `design.md:737-741`) AND a slot is now free for them (§9.5 `design.md:758-762`).
- For a downstream task branched off an in-flight PR: `da workflow fanout --plan <P>
  --task <T>` automatically resolves a single dependency's PR head and records
  `scope.base_branch/base_pr/base_task` in the bundle. There is no `--base-task` CLI flag.
  If multiple deps sit in `awaiting_review` on distinct branches, fanout **refuses non-zero** →
  the loop passes `--base-branch` explicitly or sequences (§2.5, §9.4). On upstream PR close,
  cascade-block downstream and replan (§2.6, §7.2).

**4. Manage worker lifecycle.**
- Lifecycle the loop drives (production path, [daw §7]):
  `fanout → worker (impl+verify+review via profile stages) → merge-back → delegation gate
  (read-only readback) → delegation closeout`. In the swarm, the `gate` stage produces the
  merge-back + READY/FOLD-BACK artifacts and owner-holds the merge (RULE 4, yaml:60-72,
  `CONVENTIONS.md:49-51`); `Main` runs `delegation closeout --decision accept|reject` after the
  owner merges (accept → task `completed`; reject → task `blocked` + note, [daw §7.5]).
- **write_scope-conflict constraint (two enforcement points):**
  (a) *Selection-time* — `eligible` annotates `conflicts_with` and excludes conflicting tasks
  from `max_batch`; conflict is **directory-aware prefix** (`commands/workflow` conflicts with
  `commands/workflow/x.go` but NOT `commands/workflow_test/`; normalize with trailing `/`, WPO
  §4.2 `design.md:240-244`). Empty `write_scope` ⇒ no pairwise conflict but flagged
  `[no write_scope declared]` / `write_scope_declared:false` — **serialize such a task alone**
  (WPO §4.2 `design.md:248-254`). So a wave is conflict-free by construction.
  (b) *Fanout-time* — `checkFanoutWriteScopeConflicts` **refuses** a fanout whose `write_scope`
  overlaps a `pending`/`active` delegation, and **fails closed** on a corrupt contract
  ([daw §7.1 step 2], `delegation.go:1166-1195`). This is the hard backstop that stops two live
  workers writing the same paths even if the loop mis-sequences a wave.
  In the swarm, code lives in per-slice worktrees off `origin/master` with **exactly one writer
  per worktree** (`CONVENTIONS.md:28-30`, RULE 3); coordination lives on the parallel
  `refs/agents/state` lineage, committed via CAS `git update-ref` (yaml:71-72) — so even the
  coordination channel serializes writes.
- **Slot transitions the loop must honor:** a task holds its slot through
  `in_progress`/`awaiting_agent_review` and frees it at `awaiting_owner_review` (§1.2); a
  `request-changes` / lens-reject / force-rebase bounce re-acquires or queues (§1.3, §7.3-7.4).
  The loop keeps freed slots busy with the next layer, decoupling implementation velocity from
  the owner's merge cadence (§1.4, §5.4).
