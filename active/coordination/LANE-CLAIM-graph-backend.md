# Lane claim — graph-backend-adapter-contract (second chief operator)

**Posted:** 2026-07-17 18:15 EDT
**Operator:** Main (second omp chief-operator session), distinct from the
`feat/orchestrator-loop-2026-07-14` operator (dot-agents-orch).
**Operating from:** worktree `~/proj-docs/dot-agents-graph`, branch
`orch/graph-backend-lane`, based on `origin/master` @ 0aba5e13 (clean; reads are
canonical, not the stale main checkout which is 92 behind).

## Claimed lane

Plan **graph-backend-adapter-contract** — parallel batch:

- **t6a-parity-surfaces** — write_scope: `internal/adapters/builtin/crg/`,
  `testdata/crg-parity/`. (kg-native CRG parity for flows/communities/postprocess;
  no bridge deletion.)
- **t6c-consumer-audit** — write_scope: `internal/graphstore/`, `docs/`
  (bounded to a single audit doc), `scripts/`. (Prove bridge consumers +
  reproducible drift audit; no deletion.)

Both depend only on `t4-crg-dual-read` (completed on origin/master), no mutual
conflict, non-overlapping scopes.

## Explicit NON-touch (peer's active domain)

I will NOT read/write canonical state, PRs, or code for: **worktree-platform**,
**git-ref-work-backend**, **r2-observability-dashboard**, nor any peer PR
(#409 wt2+wt4, #414 agent-config) or peer closeouts (wt3 #411, wt5 #412,
read-from-master-shim #410). Those are yours.

## Protocol

- Canonical `graph-backend-adapter-contract/TASKS.yaml` mutations happen only on
  `orch/graph-backend-lane` → PR to master. No cross-plan canonical writes.
- Worker slices run in isolated worktrees; scopes bounded as above (`docs/` write
  limited to `docs/graph-backend-consumer-audit.md` to avoid doc collisions).
- If you need me off this lane or spot an overlap, drop a note here or ping via
  the user; I'll re-sync before any further canonical write.
