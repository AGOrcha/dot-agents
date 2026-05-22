# Lessons index

One line per lesson. See each `LESSON.md` for the full pattern, root
cause, rule, and how-to-apply.

- [test-file-naming](test-file-naming/LESSON.md) — test files mirror
  source/feature; iteration-numbered grab-bag test files
  (`coverage_pushN`, `ci_driftN`, …) are forbidden.
- [subagent-out-of-workspace-access](subagent-out-of-workspace-access/LESSON.md) — spawned subagents are sandboxed to the workspace root; pre-authorize external dirs or read inline.
- [stale-local-master-ref](stale-local-master-ref/LESSON.md) — audits must verify against origin/<branch>, not the stale local ref (caused a 3-way cascade).
- [concurrent-workers-one-worktree](concurrent-workers-one-worktree/LESSON.md) — one active writer per worktree; workers must not self-arm CI waiters (orchestrator polls + relays).
- [prefer-interface-di-over-funcvar-seams](prefer-interface-di-over-funcvar-seams/LESSON.md) — interface-DI is the preferred test seam; func-var `var osX = os.X` is legacy; prevalence ≠ sanctioned (audits must state descriptive vs prescriptive).
- [match-ci-test-flags-locally](match-ci-test-flags-locally/LESSON.md) — verify concurrency changes with CI's exact flags (`-race -count=1`) before push; lazy WaitGroup.Add needs mutex+closed ordering vs Wait.
