# Delegation: gcc2 — implement Path A behind the merged Store contract

- plan/task: graphstore-concurrency-contract / gcc2-path-a-impl
- worktree: .claude/worktrees/gcc2 (branch gcc2-path-a off origin/master f38912ff — includes the merged gcc1 Store/Closer/Handle contract)
- spec: .agents/workflow/specs/graphstore-concurrency-contract/design.md (decision C-Hybrid LOCKED)
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/gcc2`

## Goal (Path A only — Path B/daemon explicitly deferred)
Implement the concurrency/bounds guarantees the gcc1 CONTRACT.md promises, behind the UNCHANGED Store interface:
1. Lazy/cheap ephemeral store open — open the graph store only when a command actually needs it (late open), short-lived read-mostly connections; no daemon, no cross-invocation pool.
2. Enforced bounds — hard, UNIFORM cap across the native and CRG paths (maxNodes/maxDepth/limit are caller-requested ceilings; the provider enforces the real hard cap). Same enforcement on both providers.
3. Request timeout — provider-owned deadline on long traversals; callers do NOT wrap their own.
4. Document SQLite low-write-concurrency in CONTRACT.md; recommend Postgres + external pooler (pgbouncer) for heavy concurrent deploys.

The contract must remain unchanged (transparent future A->B swap). Do NOT bind all callers/Deps here — that is gcc3. Do NOT touch commands/ (internal/graphstore scope).

## Scope
Write scope: internal/graphstore/ only. Behavior change is limited to bound/timeout enforcement + lazy open inside the providers behind the contract.

## Verify
go build ./... clean; go test ./internal/graphstore/ -count=1 (pre-existing CRG-python env failure acceptable); go vet ./...; gofmt -l . empty. Regression-style tests proving the hard cap is enforced and uniform native vs CRG, and the timeout is honored, belong here (gcc4 finalizes cross-path parity but add the core enforcement tests now). Per-file cov gate via real tests / interface seams — NO new coverage-exceptions entry.

## Closeout
Commit on gcc2-path-a, push, open PR to master `feat(gcc2): graphstore Path A — lazy ephemeral + enforced bounds/timeout`. Body: what Path A enforces, that the contract is unchanged, SQLite/Postgres guidance added, verification. DO NOT merge (user gates). Final message: enforcement approach, bound/timeout test evidence, any contract-pressure you hit (if the contract felt wrong, flag it — do not silently change it).
