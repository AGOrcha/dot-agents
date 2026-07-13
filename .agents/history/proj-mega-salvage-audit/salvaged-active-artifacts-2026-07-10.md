# Salvaged active artifacts — 2026-07-10 (resolved 2026-07-12)

These 20 files (16 verification results + 4 delegation bundles) were removed from
`.agents/active/` by commit `0086608e` ("chore(agents): archive completed content
from .agents/active"). That commit **deleted** them but never wrote them to
`.agents/history/` — the archive-to-history half of the operation was skipped
(verified: `git show 0086608e` adds 0 files under `.agents/history/`, deletes 12
under `.agents/active/`). Recovered here from `origin/master` to preserve the
verification/delegation record the "archive" commit was supposed to keep.

The temporary salvage bucket was reconciled on 2026-07-12. Exact duplicate
verification and review files were removed after byte-for-byte comparison with
their canonical history copies. The four recovered delegation bundles were
moved into each owning task's `delegate-merge-back-archive/<date>/<task-id>/bundle.yaml`.

## Task → owning plan (resolved)
- `verification/gcc4-regression-close-od1/`, `gcc5-verify-close-unblock/` + `delegation-bundles/del-gcc5-*` → `graphstore-concurrency-contract`
- `verification/*-pkg/` (agents/kg/skills) → `command-surface-decomposition` (package splits)
- `delegation-bundles/del-t2-config-relevance-resolver`, `del-t4-relevance-recompute` → `config-relevance-profiles`
- `delegation-bundles/del-cg-project-local-overlay` → `config-v2-coherence`
- `verification/f2-platform-scanner-tests/` → platform-scanner work
- `verification/p3-branch-session-finder/`, `p4-delete-legacy-shell-tree/` → command-surface / legacy-shell cleanup
- `verification/adhoc-2026-05-2{5,6,7}*/` → ad-hoc review decisions (no single owning plan)
