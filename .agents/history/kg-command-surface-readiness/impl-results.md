## 1. kg-freshness-audit

Recorded the audit in `docs/research/kg-freshness-audit.md`.

Key findings:

- `kg health` can report `healthy` while the code graph is still absent, so it is not a valid readiness gate for code-graph-backed planner work.
- `kg code-status` reflects repo-local CRG state, not KG home readiness; on a clean checkout it returns `0/0/0` with `Last updated: never` until a build runs.
- `kg code-status --json` still rendered prose during the audit, so the machine-readable freshness surface is incomplete.
- `kg build` failed in two operationally distinct ways during reproduction:
  - sandbox-only Python semaphore permission failure
  - real `database is locked` failure when a concurrent `kg update` held the repo graph DB open
- on a clean checkout, `kg build` succeeded, but its output counts did not match the immediately-following `kg code-status` counts, so `code-status` should be treated as the persisted source of truth until that mismatch is resolved.
- `kg update` succeeded on the clean checkout but reported `1 files updated, 0 nodes, 0 edges` on a clean tree, which is not yet trustworthy enough as a planner-facing freshness summary.
