# Prototype: WorkStore lease + per-field conflict (KG-as-SOT, OQ1/OQ2)

Validates two open questions from
`.agents/workflow/specs/work-tracking-storage-abstraction/design.md` §6 and reproduces +
fixes the wave-engine **5×p1c re-dispatch storm** — modeled **faithfully as worktree
isolation**, which is what the real bug actually was.

- **OQ1 — atomic claim/lease:** a scout LEASES an eligible task in a SHARED store before
  dispatching, so even with a stale local view a concurrent wave's claim is rejected.
  Two mechanisms compared: **TTL lease** and **compare-and-set (CAS)**.
- **OQ2 — per-field conflict:** `status` = **backend-wins**, content (`write_scope`,
  `notes`, `depends_on`) = **local-wins**, via a field-ownership table + a 3-way merge.

Self-contained: own `go.mod`, so repo-root `go test ./...` does **not** recurse in.

```
go test -race ./...     # the proofs (the deliverable)
go run ./cmd/demo       # storm (no shared store) vs lease/CAS; + dead-holder reclaim
```

## The model — two layers (this is the fidelity fix)

A prior version of this prototype raced N goroutines on ONE shared object; a cross-harness
(Codex) fidelity audit correctly found that **the `sync.Mutex` was doing the proof's work**,
not the lease design — and that the real 5×p1c storm was NOT a simultaneous in-process race
but a **worktree-isolation / stale-main-repo loop**. This version is redesigned to be
faithful:

1. **`LocalView` (`localview.go`)** — each scout's PRIVATE, isolated worktree checkout of
   task state. A scout reads eligibility from *its own copy*; a local status write updates
   only *its own copy* and never propagates (spec §1). This isolation is the storm's root.
2. **`Store` (`store.go`)** — the SHARED WorkStore the spec proposes to add. Its claim path
   is the **coordination point**.

- **Negative control = the real bug:** `ScoutNaive(localView)` decides eligibility and
  dispatches off the local view with **no shared store at all**. It cannot be "fixed" by a
  lock because there is nothing to lock — the defect is architectural (no shared SOT).
- **Fix:** `ScoutTTL`/`ScoutCAS(store, localView)` must CLAIM the shared store before
  dispatching. The store grants exactly one.

## The proofs (`*_test.go`)

| Proof | What it asserts | Mechanism under test |
|---|---|---|
| `TestHStorm_NaiveLocalStaleRead_ReproducesStorm` | 5 isolated worktrees, **no shared store** → ALL 5 dispatch (deterministic 5/5, the storm) | `ScoutNaive` |
| `TestHStorm_SharedClaim_PreventsStorm_{TTL,CAS}` | same 5 isolated worktrees + shared store → exactly 1 dispatch; store stats: **attempts=5, grants=1** | `ScoutTTL`/`ScoutCAS` |
| `TestAntiMutex_TTL_SameMutex_GuardDecidesNotLock` | **same mutex**, guard ON grants 1 vs guard OFF grants 5 — proves the claim guard (not the lock) serializes | `ClaimTTL` guard modes |
| `TestAntiMutex_CAS_GuardLayers` | same mutex: no-guard=5, version-only=1, full=1 — the CAS version check is the serializer | `ClaimCAS` guard modes |
| `TestHClaim_ExactlyOneWinner{TTL,CAS}` | 16 isolated worktrees, 1200 iters under `-race` → 1 dispatch, store grants 1 | both |
| `TestHTtl_DeadHolderReclaimableUnderTTL` | dead holder's lease expires → another worktree reclaims, no deadlock | `ScoutTTL` |
| `TestHTtl_CASWedgesOnDeadHolder` | **failure mode** — CAS (no expiry) wedges forever | `ScoutCAS` |
| `TestHConflict_*` | backend-status + local-content merge; mutation-sensitive to ownership flip | `Merge` |
| `TestFinding_*` | re-validates the design findings (terminal/blocked/fencing) against the model | `Merge` + `CompleteFenced` |

### Why the contention is shared-store-mediated, not mutex-trivial (EXECUTABLE)

This is proven by `TestAntiMutex_*` (`antimutex_test.go`), not just described. The store has a
`guard guardMode` flag; **every mode still takes `s.mu`**, so the lock is held constant and
only the claim DECISION varies. The same 5-isolated-worktree wave runs through each mode in
one `-race` run:

- **TTL** (`TestAntiMutex_TTL_SameMutex_GuardDecidesNotLock`): guarded (`guardFull`) grants
  **1**; the same-mutex unguarded store (`guardNone`) grants **5** — the storm leaks straight
  through the lock. Same `sync.Mutex`, opposite outcome ⇒ the serializer is the claim guard,
  not the lock.
- **CAS** (`TestAntiMutex_CAS_GuardLayers`): `guardNone` grants **5**; `guardCASVersion`
  (version check only) grants **1**; `guardFull` grants **1**. The version check is
  load-bearing precisely because isolated worktrees carry stale local versions, so only one
  scout's expected-version matches the store.

Observed in-run: `guarded grants=1, unguarded grants=5` (TTL); `guardNone=5, versionOnly=1,
guardFull=1` (CAS). The test is itself mutation-verified — forcing the unguarded store to
apply the guard anyway makes it grant 1, and the contrast assertion then fails.

Conversely, the negative control (`ScoutNaive`) touches no shared object at all yet still
storms — so the bug and its fix both live at the coordination layer, not in goroutine
scheduling.

## Findings — measured against the FAITHFUL model

| Hypothesis | Negative control (broken) | Fix (lease/CAS) |
|---|---|---|
| **H-storm 5×p1c** (worktree isolation) | naive, no shared store: **5/5 dispatch every run** (deterministic storm) | TTL & CAS: **1 dispatch**, 0 storms across 3000 races; store attempts=5 grants=1 |
| **H-claim** (16 worktrees) | (same architecture → would N-dispatch) | TTL & CAS: **1 dispatch**, store grants 1, 1200 iters each `-race` |
| **H-ttl** (dead holder) | CAS: wedged forever (0 reclaims) | TTL: reclaims after expiry, no deadlock |
| **H-conflict** | flip ownership table → outcome inverts (test fails) | backend-status + local-content kept |

Mutation checks (all on the **production path**, all falsify the right proof): the
anti-mutex contrast is itself an executed test (guard off → 5 grants through the same lock);
flip `status` ownership → H-conflict fails; lease never expires → H-ttl reclaim fails; drop
the version fence → fencing test fails.

## What this DOES and does NOT prove

**DOES prove (in scope):**
- The **worktree-isolation failure mode** — the actual 5×p1c bug — is faithfully reproduced
  (deterministic N-dispatch with isolated views and no shared SOT).
- A **shared claim** (TTL lease or CAS) layered over those same isolated views **prevents**
  the storm: exactly one dispatch, with the contention demonstrably resolved AT THE SHARED
  STORE (attempts=N, grants=1), not by an in-process lock.
- The **coordination-layer design** — claim-before-dispatch, TTL recovery, per-field merge,
  fencing — has the right semantics. **Re-statement of done-criterion #2 for what is tested:**
  *under worktree isolation with a shared WorkStore claim, the same eligible task is
  dispatched at most once.*

**Does NOT prove (explicitly out of scope — needs a real distributed harness):**
- **Distributed/CF-DO backend safety.** Atomicity here is a process-local `sync.Mutex`
  standing in for the *transport* (a real `kg`/CF-DO backend needs a transactional CAS or a
  single-threaded DO actor). This validates the *algorithm*, not a distributed implementation.
- **Distributed CAS contention, clock skew, network partitions, ret[ry] storms.** TTL assumes
  a shared clock; a real backend needs server-authoritative time and partition semantics.
  None of that is modeled. **No distributed claim is made.**
- **The daemon sync loop** (local↔backend reconciliation, spec D4) — not modeled.

## Design findings (re-validated against the model; each has a test)

**Does the lease model prevent the storm? Yes** — for the worktree-isolation model. 5/5
deterministic storm → 0 storms with a shared claim.

**TTL vs CAS — recommend a TTL lease whose acquire/renew/release is CAS-guarded on `version`.**
- CAS alone prevents double-claim but **wedges forever on a dead holder**
  (`TestHTtl_CASWedgesOnDeadHolder`) — no self-healing, worse than re-dispatch for a system
  where workers crash/timeout.
- TTL gives crash recovery; CAS-on-version gives the atomic acquire + a safe renewal. They
  are complementary; the prototype shows each in isolation so the contrast is visible.

**Spec under-specifies (each now backed by a `TestFinding_*`):**
1. **TTL value + renewal** — a long task outlives any fixed TTL; the holder MUST heartbeat-
   renew, CAS-guarded. Unspecified.
2. **Lease expiry MID-WORK** (the #1 hazard) — TTL fires while wave-1 still works, wave-2
   reclaims, wave-1 finishes → two PRs anyway. Fix = **fencing tokens**: `CompleteFenced`
   rejects a completion whose token (claim version) was superseded
   (`TestFinding_LeaseExpiryMidWorkNeedsFencing`, `TestFinding_FenceRejectsStaleTokenSameOwner`).
3. **Field-ownership beyond status/content** — `depends_on` may want directional ownership
   (backend can add a *learned* dep); `owner` (assignee) vs `Lease.Owner` are conflated (the
   prototype separates them); `version` must be backend-only.
4. **Committed-YAML snapshot (D1′)** — a re-ingested hand-edited YAML is a third writer; the
   2-way story needs a **3-way merge** with the snapshot as `base` (the prototype's `Merge`
   already takes a `base`; the spec doesn't name where it comes from).

**Cases wanting a different rule than backend-status/local-content (each has a test):**
- **Terminal states** (`completed`/`cancelled`) → idempotent **first-writer**, not backend-
  wins: plain backend-wins **resurrects finished work**
  (`TestFinding_TerminalStateBackendWinsResurrectsWork`; `firstWriterTerminal` is the fix).
- **`blocked`** → **union/OR** (block if either side is blocked), not last-writer: backend-
  wins can wrongly unblock (`TestFinding_BlockedWantsUnionNotLastWriter`; `blockedUnion` fix).
