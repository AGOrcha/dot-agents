# Old orchestrator DMA artifact reorganization results

Completed 2026-07-12.

- Emptied the file contents of `.agents/active/delegation-bundles`, `delegation`, `merge-back`, `verification`, and `reviews`.
- Archived each stale DMA set under its owning plan and task using `delegate-merge-back-archive/<date>/<task-id>/`.
- Standardized task contents as `bundle.yaml`, `delegation.yaml`, `merge-back.md`, and `verification/*`.
- Preserved stale delegation variants that differed from canonical copies as `delegation.stale-active.yaml`.
- Dissolved `salvaged-active-artifacts-2026-07-10`: exact duplicates were byte-compared before removal, recovered bundles were attached to canonical task archives, and the salvage explanation was retained under `proj-mega-salvage-audit`.
- Moved plan-level review decisions for `f2-platform-scanner-tests`, `p3-branch-session-finder`, and `p4-delete-legacy-shell-tree` into their task archive `verification/` directories.

Validation:

- Active DMA file count: 0.
- Salvage-bucket file count: 0.
- All YAML files in newly created 2026-07-12 task archives parse successfully with `yq`.
- A repository-wide historical YAML parse also exposed two unrelated pre-existing malformed artifacts under `kg-command-surface-readiness`; they were not changed.
- KG self-review context degraded gracefully because this old worktree has no `./bin/da`.
