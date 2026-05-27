# graphstore-concurrency-contract — impl results

Plan: graphstore-concurrency-contract (Path A)
Spec: `.agents/workflow/specs/graphstore-concurrency-contract/design.md`
Archived: 2026-05-26 by gcc5-verify-close-unblock

## What shipped

Per locked decision C-Hybrid, the plan published the stable `graphstore.Store`
contract + Path A (lazy/cheap ephemeral, bounded, request-timeout-uniform),
pinned the Deps boundary, and rebound all callers behind it.

| Task | Status | PR | Outcome |
|---|---|---|---|
| gcc1-pin-store-contract | completed | merged | Published `Store` + segregated role interfaces (`CodeGraphReader/Writer`, `KGNoteStore`, …) + `internal/graphstore/CONTRACT.md`. KEYSTONE (review-gated). |
| gcc2-path-a-impl | completed | #34 | Path A: lazy ephemeral SQLite, real shutdown w/ reaper, pool sized for agent fleets, Windows SQLite hang resolved. |
| gcc3-bind-callers | completed | #57 | All graphstore callers + `commands/workflow/deps.go` Deps holder bound to the contract-typed handle (not concrete backends). |
| gcc4-regression-close-od1 | completed | #66 | Postgres reads/execs routed through `requestContext` (gcc2-deferred). Cross-provider timeout uniformity + maxNodes hard-cap regression suite. Closes maxNodes Low-1 and di-refactor OD-1 (rationale comment landed in deps.go). |
| gcc5-verify-close-unblock | completed | n/a (workflow) | Verified done-criteria; archived plan; downstream notified. |

## Done criteria — proof

- **One published contract.** `internal/graphstore/store.go` defines `Store` + roles. `CONTRACT.md` documents provider guarantees (bounds, timeout, concurrency-ownership).
- **All callers + Deps bound.** Grep confirms `graphstore.Store` / `graphstore.Handle` is the only graphstore-typed handle across `commands/` and `internal/`. `commands/workflow/deps.go:35` holds the contract-typed `Store graphstore.Handle`.
- **Bounds + timeout uniform native/CRG.** gcc4 added cross-provider regression (`regression_contract_test.go`, `regression_contract_pg_test.go`) proving hard-cap, native/CRG parity, and request-timeout honored across SQLite and Postgres providers (Postgres parity test skips cleanly when Docker absent; in-package regressions run unconditionally).
- **maxNodes Low-1 closed.** Enforced via the contract chokepoint — no per-caller maxNodes leakage path remains.
- **di-refactor OD-1 closed.** Rationale comment in `commands/workflow/deps.go` documents the package-singleton as a holder of a contract-typed handle whose provider owns concurrency.
- **`go test ./... -count=1` green.** Verified 2026-05-26 (every package OK).
- **Coverage gate: no new gcc-attributable allowlist entries.** gcc4 merge (`07f0dd75`) added 3 files (`postgres.go` + 2 test files) and zero allowlist entries. The pre-existing `internal/graphstore/{crg,postgres,sqlite}.go` allowlist entries are fenced and owned by the cg6b loop (per cg6b notes: "internal/graphstore/* stays FENCED").

## Downstream unblocks

- **seam-interface-di-migration** (active; absorbed the original di-refactor-rollout per the 2026-05-19 maintainer ruling): Deps boundary is now pinned at the `graphstore.Handle` contract (gcc1+gcc3). The migration can propagate the func-var → interface-DI pattern without re-litigating the graphstore seam shape.
- **coverage-gate-per-file / cg6b-ratchet-loop**: the 95% tail may now adopt the pinned contract-bound seam pattern (depend on the narrowest role — `CodeGraphReader`, `KGNoteStore`, etc. — rather than the full `Store`) for its `[defensive-unreachable]` files. The graphstore allowlist entries that have been fenced (`crg.go`, `postgres.go`, `sqlite.go`) are now eligible to enter cg6b batches.

## Per-task narrative

- gcc1 was review-gated because the contract shape propagates xN downstream; the segregated-role design (5 cohesive interfaces composed into `Store`) was the load-bearing decision and survived review.
- gcc2 surfaced a Windows SQLite hang that the lazy/cheap ephemeral model resolved; the reaper-tracked shutdown pattern is the path B onramp.
- gcc3 was the "propagation gcc1 was review-gated to protect" — it landed cleanly because gcc1 got the shape right.
- gcc4 absorbed the postgres `context.Background()` cleanup that gcc2 deliberately deferred, plus authored the cross-provider parity regression that proves the chokepoint.

## Surprises

None substantive. The plan stayed within its locked decision; no replans, no scope creep into Path B. The Postgres parity test gating on Docker-availability is the only nuance — in-package regressions carry the load-bearing proof.

## References

- Spec: `.agents/workflow/specs/graphstore-concurrency-contract/design.md`
- Contract doc: `internal/graphstore/CONTRACT.md`
- Per-task merge-back (only one extant in active/): `.agents/active/merge-back/gcc4-regression-close-od1.md`
- PRs: #34 (gcc2), #57 (gcc3), #66 (gcc4)
