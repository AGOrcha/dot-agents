# Prototype: WorkStore lease + per-field conflict (KG-as-SOT, OQ1/OQ2)

Validates two open questions from
`.agents/workflow/specs/work-tracking-storage-abstraction/design.md` §6 and replays the
wave-engine **5×p1c re-dispatch storm** (the spec's done-criterion-#2 regression).

- **OQ1 — atomic claim/lease:** a scout LEASES an eligible task before dispatching, so a
  concurrent wave sees it claimed and backs off. Two mechanisms prototyped + compared:
  **TTL lease** and **compare-and-set (CAS)**.
- **OQ2 — per-field conflict:** `status` = **backend-wins**, content (`write_scope`,
  `notes`, `depends_on`) = **local-wins**, via a field-ownership table + a 3-way merge.

Self-contained: own `go.mod`, so repo-root `go test ./...` does **not** recurse in.

```
go test -race ./...     # the proofs (the deliverable)
go run ./cmd/demo       # naive storm vs lease/CAS, + TTL dead-holder reclaim
```

## The proofs (`*_test.go`)

| Proof | What it asserts | Drives |
|---|---|---|
| `TestHClaim_ExactlyOneWinner{TTL,CAS}` | 16 scouts race 1 task, 1200 iters under `-race` → exactly 1 dispatch | `ScoutTTL`/`ScoutCAS` (the real entry point) |
| `TestHClaim_NaiveProducesDoubleDispatch` | **negative control** — naive no-lease path DOES double-dispatch | `ScoutNaive` |
| `TestHStorm_NoReDispatch_5xp1c` | 5 scouts race `p1c`, 1500 iters: ttl/cas → exactly-once; naive → reproduces storm | all three |
| `TestHTtl_DeadHolderReclaimableUnderTTL` | dead holder's lease expires → task reclaimable, no deadlock | `ScoutTTL` |
| `TestHTtl_CASWedgesOnDeadHolder` | **failure mode** — CAS (no expiry) wedges forever on a dead holder | `ScoutCAS` |
| `TestHConflict_*` | backend-status + local-content merge; mutation-sensitive to ownership flip | `Merge` |

## Fidelity self-audit

**(a) Faithful to the real task schema.** `Status` is the exact `CanonicalTask` enum from
`schemas/workflow-tasks.schema.json` (`pending|in_progress|blocked|completed|cancelled`) —
not a 2-state toy. `claimed` maps to `in_progress` + a live lease (the canonical schema has
no `claimed` state). Content fields are the real ones: `WriteScope []string`, `Notes`,
`DependsOn []string`, `Owner`. The 5×p1c scenario is the real shape: N concurrent scouts
(one per wave) evaluating the **same** eligible task.

**(b) Negative-control outcome per hypothesis** (numbers from a representative `-race` run):

| Hypothesis | Broken impl | Correct impl |
|---|---|---|
| H-claim (16 scouts) | naive: double-dispatch in **63/1200** races | ttl & cas: **0** double-dispatch, 1200/1200 exactly-once |
| H-storm (5×p1c) | naive: re-dispatch in **178/1500** races | ttl & cas: **0**, 1500/1500 exactly-once |
| H-ttl (dead holder) | CAS: wedged permanently (0 reclaims) | TTL: reclaims after expiry, no deadlock |
| H-conflict | flip ownership table → outcome inverts (test fails) | backend-status + local-content kept |

Mutation checks on the **production path** all falsify the right proof: removing the
`ClaimTTL` lock → H-claim/H-storm double-dispatch; flipping `status` ownership → H-conflict
fails; making the lease never expire → H-ttl reclaim fails.

**(c) Could this still give a FALSE pass?** Considered and mitigated:
- *Weak positive test* → addressed by the negative control: the naive path provably fails
  the same invariant, so passing isn't free.
- *Fixed scheduling hiding a race* → each scout adds randomized `runtime.Gosched()` jitter
  and all release from one barrier; 1200–1500 iterations under `-race`.
- *Tautological merge test* (compares a value to itself) → the conflict tests use an
  independent `base`/`local`/`backend` three-way and a mutation check that inverts the
  ownership table and asserts the outcome flips.
- *Single-process mutex ≠ distributed atomicity* — see limitation below. This is the one
  place the model is NOT faithful to a real distributed backend, and it is called out, not
  hidden.

## Findings (informs the spec)

**Does the lease model PREVENT the re-dispatch storm? Yes.** Both TTL and CAS reduce
double-dispatch from a measured 5–15% of races to **0/2700 races** combined. The naive
read-eligible-then-dispatch path (the exact 5×p1c bug) reproduces the storm; the atomic
claim eliminates it. Done-criterion #2 holds *in-process*.

**TTL-lease vs compare-and-set — recommend TTL lease (CAS as the claim primitive under it).**
- **CAS alone is unsafe for liveness.** It correctly prevents double-claim, but a holder
  that dies mid-work wedges the task forever (`TestHTtl_CASWedgesOnDeadHolder`) — no
  self-healing. In an orchestration system where workers crash/timeout, that is a worse
  failure than re-dispatch.
- **TTL alone needs a CAS-like guard for renewal** (renew only if you still hold it).
- **Recommendation:** a **TTL lease whose acquire/renew/release transitions are CAS-guarded
  on `version`**. TTL gives crash recovery; CAS-on-version gives the atomic acquire and a
  safe renewal. They are complementary, not either/or — the prototype shows each in
  isolation so the contrast is visible.

**What the spec UNDER-SPECIFIES:**
1. **Lease TTL value + renewal.** §6 names "TTL lease?" but not the value or whether a live
   worker renews. A task can outlive any fixed TTL → the holder MUST renew (heartbeat),
   and renewal must be CAS-guarded. Unspecified today.
2. **Lease expiry MID-WORK.** The dangerous case: TTL fires while wave-1 is still working,
   wave-2 reclaims, then wave-1 finishes → **two PRs after all**, the storm via a different
   door. The spec must define: fencing tokens (reject a completion from an expired lease),
   or "expiry only re-dispatches, never lets the original complete." The prototype models
   expiry→reclaim but not fencing — flagged as the #1 gap.
3. **Field-ownership beyond status/content.** `status=backend / content=local` is too coarse:
   - `depends_on` — modeled local-wins, but a backend that learns a *new* cross-plan dep
     should arguably win. Ownership may be **directional** (add vs remove), not per-field.
   - `owner` (assignee) vs lease-owner — two different "owner" concepts; the spec conflates
     them. The prototype separates `Task.Owner` (workflow assignee) from `Lease.Owner`.
   - `version` is backend-only and must never be merged from local — implicit, should be explicit.
4. **Interaction with the committed-YAML snapshot (D1′).** The committed YAML is "a periodic
   snapshot, never the authority." But a hand-edited committed YAML re-ingested while a lease
   is live is a third writer the 2-way merge doesn't cover — needs a **3-way** merge against
   the snapshot as base (the prototype's `Merge` already takes a `base`; the spec's
   reconciliation story doesn't name where `base` comes from).

**Cases wanting a different rule than backend-status/local-content:**
- **Terminal transitions** (`completed`, `cancelled`) should be **idempotent + first-writer**,
  not backend-wins: a backend that flips `completed → in_progress` over a worker that just
  finished would resurrect dead work. Backend-wins is right for *forward* coordination
  transitions, not for clobbering terminal states.
- **`blocked`** set by either side should arguably be a **union/OR** (block if either thinks
  it's blocked), not last-writer.

## Limitations (honest scope)

- **In-process mutex, not a distributed backend.** Atomicity here is a `sync.Mutex`; a real
  `kg`/CF-DO backend needs a transactional CAS or a DO single-threaded actor. The prototype
  proves the *semantics* (exactly-once claim, TTL recovery, per-field merge), not the
  distributed implementation. The mutex is the in-process *analogue* of an atomic backend
  txn — faithful to the algorithm, not the transport.
- **No fencing tokens** — the mid-work-expiry hazard (finding #2) is identified but not fixed.
- **No network partition / clock-skew modeling** — TTL assumes a shared clock; a real
  backend needs server-authoritative time.
