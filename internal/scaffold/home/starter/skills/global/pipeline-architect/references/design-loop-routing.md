# Deep dive — deterministic loop, routing, and lifecycle (§1–§3)

Operational depth behind `instructions/design-pipeline.md`. This is the model- and
registry-agnostic statement of *what an emitted pipeline must do and why* for the loop
skeleton, stage routing, and lifecycle/recovery contracts. It names no model, vendor, or
closed capability registry, and carries no plan-scoped evidence anchors. Source of truth:
`docs/full-loop-pipeline-craft.md` §1–§3.

---

## 1. Deterministic loop skeleton (§1)

The full loop is a **two-owner split**: the workflow engine owns selection, slots, fanout,
and lifecycle state; the agent harness owns only agent execution. Nothing about *which* task
runs, *how many* run, or *what transition* a result triggers is left to model prose. Every one
of those decisions is a typed computation over canonical plan/task state, not a narrated
judgement.

### The wave algorithm — one iteration is one wave

1. **Slots.** Query the engine for available/occupied slots. `available == 0` is a **quiescent
   break**, never a spin. Occupancy is a typed predicate, not a guess: a slot is held for
   exactly the *in-progress* and *awaiting-agent-review* states; every other state
   (awaiting-owner-review, blocked-on-anything) frees the slot. The slot budget defaults to a
   machine-derived value — available parallelism minus a reserve — not a hard-coded constant,
   so the loop scales to the host instead of pinning an arbitrary number.
2. **Eligible.** Ask the engine for the eligible task set, the max-batch set, the conflict
   graph, and the total-eligible count. `total_eligible == 0` is a break. Dependency
   satisfaction is a typed rule: a dependency is satisfied by a *completed* or
   *awaiting-owner-review* upstream, and **not** by an *in-progress* one. That single rule
   decouples downstream velocity from merge latency — a child can start the moment its parent
   is in owner review, without waiting for the merge.
3. **Conflict-free selection.** Intersect the eligible set with the max-batch set: the largest
   write-scope-disjoint subset, greedy by declared order. Two tasks conflict iff any
   write-scope path of one is a prefix of the other. The loop therefore **never** dispatches
   two workers into overlapping files in the same wave — the disjoint-slice invariant is
   computed, not trusted.
4. **Fanout.** Each selected task is fanned out, its bundle resolved (a downstream task layers
   onto its dependency's open PR branch), and its inner pipeline launched in its **own process
   group** as a background job.
5. **Barrier.** The driver waits on every inner process before proceeding — a hard wave
   barrier. A failed inner marks the wave failed but does **not** skip reconciliation; failure
   is a routed outcome, not an escape.
6. **Reconcile.** After the barrier, exactly one serialized reconcile pass runs and MUST emit a
   `RECONCILED` sentinel or the driver aborts. Only then does the next wave begin.

### Why determinism

Prose plans drift. "Status said done" is not auditable, and a model left to choose its own next
task or declare its own status will invent tasks, exceed the slot budget, or self-report a
transition that never happened. The deterministic skeleton mechanizes the lesson that
**canonical state wins over stale checkpoints**: selection, slots, fanout, and lifecycle are
computed by the engine from canonical plan/task state, so the model cannot override them.
Planning correspondingly hardens from prose plans into **execution contracts** with locked
decisions, required reads, verification targets, and stop conditions — the plan stops being a
suggestion and becomes the dispatch input.

### Rules

- Compute selection, slot budget, and transitions from canonical engine state; NEVER let an
  agent choose its own next task or declare its own status.
- Gate every dispatch on the max-batch set: dispatch only write-scope-disjoint tasks per wave.
- Treat `available == 0` and `total_eligible == 0` as clean quiescent stops, never busy-waits.
- Use the typed predicates: a slot is held only by in-progress / awaiting-agent-review; a
  dependency is satisfied by completed / awaiting-owner-review, not in-progress.
- Enforce a wave barrier: wait on all inner pipelines, then run exactly one serialized
  reconcile that MUST emit `RECONCILED` before the next wave; abort if it does not.
- Bound the run with an explicit max-waves ceiling; the live protocol requires it.

---

## 2. Stage, profile, and model routing (§2)

Routing is **typed config**, not prompt text. Stage profiles form a two-level map:
stage (executor / verifier / reviewer / orchestrator) → slug → profile. Each profile carries a
label, a concrete model, an **open-ended** model family, a base-first ordered prompt-file
composition, and an optional precondition policy. The same profile type serves all four stages,
so the agentic stages are uniform composable primitives — **one routing surface, four
consumers**. Any legacy per-stage profile keys fold into the single canonical routing map (new
key wins, legacy never re-emitted), so there is exactly one place a route lives.

**The model family is intentionally open-ended.** Diversity requires *inequality*, not a closed
vendor list. A new tier is a config edit, not a code change, and cross-family gates work against
families the code has never heard of — the comparison is identity inequality, never membership
in an allowlist.

### Prompt resolution is the projection surface

A single `resolve-prompt` seam returns, per stage+slug: the matched flag, the model, the model
family, and the base-first, scope-resolved prompt-file composition. Per-file precedence is
fixed and total:

1. absolute path
2. repo-local project scope
3. repo-local prompts scope
4. shared-home starter
5. unresolved (error)

Repo-local committed files win over the shared-home starter, so a project overrides the product
base simply by dropping a same-named file into its prompts scope — no fork, no code change.
Every dispatcher — the worker, the orchestrator, and the emitted swarm projection — calls this
one seam, so all consumers resolve the **same merged prompt**. A resolve stage that finds a
matched stage with an empty model or empty model family MUST refuse: the projection is generated
from the routing IR, never authored inline, so an empty route is a build error, not a runtime
surprise.

### Cross-family gate binding

The blocking adversarial review MUST run on a different model family than the executor; same
family on both sides makes the review invalid (the reviewer shares the executor's blind spots).
Bind that diversity to a **named** adversarial lens, never to a numeric slot index or an assumed
list order: partition the named lens out of the resolved set and assert its family differs from
the executor/default family. A numeric binding breaks silently the moment the lens list is
reordered; a named binding fails loudly if the lens is missing. The review then projects to each
harness's native gate while still satisfying the cross-family rule (see the verification/review
deep dive).

### Rules

- Express every stage as a typed profile with an explicit model AND model family; refuse to
  emit or dispatch a matched stage whose model or model family is empty.
- Resolve every stage prompt through the single resolve seam (base-first, scope-merged); NEVER
  inline duplicate prompt prose into the projection.
- Preserve prompt precedence: repo-local prompts override the shared-home starter for the same
  filename.
- Bind cross-family diversity to the named adversarial lens and assert
  `reviewer.family != executor.family`; NEVER bind it to a numeric reviewer slot or list order.
- Keep the model family open-ended (identity comparison, no closed vendor allowlist).

---

## 3. Lifecycle and recovery contracts (§3)

### Delegation lifecycle

A task moves `fanout → bundle → worker → merge-back → closeout`, all engine-owned. Fanout
materializes a contract plus a base-resolved bundle (a downstream task is layered onto its
dependency's open PR branch); the worker writes only inside the bundle's authoritative
write-scope; the parent authors a schema-valid merge-back and runs closeout. The durability
lesson is load-bearing: **the merge-back survives late worker failure** — the parent can author
it after confirming commit and verification even when the worker environment has become
inaccessible. The record of what happened does not depend on the worker still being alive.

### Fold-back re-entry is bounded

The inner pipeline's target count is a **hard iteration ceiling**. A retryable verifier/lens
rejection re-enters the **executor inside the same active delegation** — it does NOT fan the
task out again. Terminal fold-back is the result *after* that bounded budget is exhausted:
reconcile records each item, persists a failed merge-back, closes out with a reject decision,
and the canonical task becomes blocked with its slot freed; a later explicit unblock/replan
creates a fresh delegation. Bounded re-entry is exactly why the loop converges instead of
looping forever — there is no unbounded retry path.

### Failure reconciliation — every mode routes back through reconcile

- **Crash / non-zero / missing inner exit** ⇒ recoverable lifecycle failure: record an
  idempotent fold-back with the exit code and logs, persist the failed artifact, close out
  reject, free the slot. Never claim success; never leave an orphaned in-progress delegation.
- **Stale driver lock** ⇒ the lock is pid-aware: a dead owner's lock is recovered, a live
  owner's is refused. Recovery never races a live driver.
- **Incomplete prior wave** ⇒ on startup, reconcile any wave lacking a `RECONCILED` sentinel —
  but refuse if a live driver pid still owns its coordination directory.
- **Fanout refusal** ⇒ a failed fanout writes an explicit fold-back for that task so an earlier
  successful sibling delegation is never stranded.

The unifying invariant: **there is no abandonment path**. Every failure is a routed, idempotent
outcome that frees its slot and leaves a durable record.

### Signal co-termination

A single terminal/tmux restart can take a whole process tree down with pending tool calls. The
driver encodes the fix: each inner pipeline gets its **own process group** so an interrupt
co-terminates the driver *and* every agent it spawned, not just the wrapper; an exit trap
terminates each job's process group and releases the lock. Sessions die several ways —
rate-limit walls, OS-signal co-termination, mid-turn cutoffs — and the runtime must checkpoint
before signal-class kills and treat each as **resumable, not fatal**.

### Rules

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
