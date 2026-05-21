# Delegation: gcc1 — define & publish the Store contract (KEYSTONE, review-gated)

- plan/task: graphstore-concurrency-contract / gcc1-pin-store-contract
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/gcc-contract`
  (branch `gcc-contract` off master efb19756)
- spec: `.agents/workflow/specs/graphstore-concurrency-contract/design.md`
  (decision LOCKED: C-Hybrid — contract + Path A now, Path B later as a
  transparent provider swap; provider owns pooling/serialization)
- status: delegated
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/gcc-contract`

## Why this is special

This is THE keystone. `di-refactor-rollout` (×6 packages) and the cg6b
95% coverage tail BOTH bind to the exact `Deps`/Store shape pinned here.
Getting it wrong propagates ×N. So this task is **contract-definition +
review gate ONLY** — not the full rollout.

## Goal

Produce a stable, published `Store` interface and the documented
concurrency-ownership + Deps-boundary semantics, as a REVIEWABLE
artifact. Define the contract; have `Deps` hold a contract-typed handle.

## Scope (strict — this task is deliberately narrow)

- Write scope: `internal/graphstore/` ONLY.
- DEFINE the `Store` interface (the published contract surface its
  callers need: the graph read/write/bounds/timeout operations actually
  used — derive from current concrete store usage, do not over-specify).
- DOCUMENT the contract: godoc on the interface + a concise
  `internal/graphstore/CONTRACT.md` stating the provider guarantees —
  bounds enforcement, request timeout, and the concurrency-ownership
  rule (provider owns pooling/serialization; the Deps singleton is only
  a holder of the contract-typed handle, NOT the concurrency story).
- Have `Deps` expose a contract-typed handle field/accessor.
- DO NOT: implement Path A internals (that is gcc2), refactor all
  callers (that is gcc3), or change graph behavior. Existing concrete
  store keeps working; the interface is additive. No behavior change.

## Verify

`go build ./...` clean; `go test ./internal/graphstore/ -count=1`
green; `go vet ./...` clean; `gofmt -l .` empty. The interface must be
satisfied by the existing concrete store (compile-time assertion
`var _ Store = (*concreteType)(nil)`).

## Closeout — REVIEW GATE

Commit on `gcc-contract`, push, open PR to master titled
`feat(gcc1): publish graphstore Store contract + pin Deps boundary`.
Body: the interface surface + rationale for what is in/out of it, the
concurrency-ownership statement, how Deps holds the handle, and an
explicit note that gcc2 (Path A impl) and gcc3 (bind all callers) are
DEFERRED pending review of this contract. DO NOT merge — STOP for user
review. Final message: the interface signature (verbatim), the
guarantees doc summary, and the explicit ask for user sign-off on the
contract shape before gcc2/gcc3.
