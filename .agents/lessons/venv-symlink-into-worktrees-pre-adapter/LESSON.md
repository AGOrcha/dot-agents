# Symlink .venv into every worktree (until graph-backend-adapter-contract lands)

**Captured:** 2026-05-28
**Expires:** when `graph-backend-adapter-contract` plan lands (replaces CRG-specific Python bridge with adapter pattern)

## The rule

Every new worktree created under `.agents/worktrees/<name>` must have the repo's `.venv` symlinked at its root before any pre-push hook runs:

```bash
git worktree add .agents/worktrees/<name> -b feature/<name> origin/master
ln -s /Users/nikashp/Documents/dot-agents/.venv .agents/worktrees/<name>/.venv
```

Bake this into every worker bootstrap prompt that involves `git worktree add`.

## Why this exists

The pre-push `coverage-gate` mandate runs `go test ./...` which transitively exercises `internal/graphstore/crg_test.go` → calls `code-review-graph`'s Python bridge. The Python bridge resolves its venv via `.venv/bin/python` relative to the cwd (worktree root). Without the symlink, the bridge fails with `ModuleNotFoundError: code_review_graph` and the test panics, taking the whole `go test` exit code with it. coverage-gate then reports "go test failed (coverage profile not produced)" — looks like a real failure but is purely a worktree-env issue.

The main repo at `/Users/nikashp/Documents/dot-agents` has the venv at `.venv/`. Worktrees don't get it by default since they're separate filesystem checkouts. Symlinking is the cheapest correct fix.

## When this expires

The `graph-backend-adapter-contract` plan (currently draft, 6 pending tasks) replaces the direct CRG Python bridge with an adapter interface supporting multiple backends (none, CRG, gopls-native, etc.). Once landed:
- Worktrees with no CRG backend configured skip CRG tests entirely (no venv needed)
- The Python bridge becomes one of several optional backends, behind a build tag
- Pre-push hooks shouldn't fail because a worker didn't have CRG locally

When that ships, this lesson can be archived. Until then: symlink the venv every time.

## Symptoms when forgotten

Pre-push fails with:
```
[mandate] BLOCKED: go test failed (coverage profile not produced)
```
…digging into the test log shows:
```
=== RUN   TestCRGBridgeFreshBuildRealCRG
--- FAIL: TestCRGBridgeFreshBuildRealCRG (X.XXs)
    crg_test.go:XXX: bridge build failed: ModuleNotFoundError: No module named 'code_review_graph'
```

Recovery options:
1. **Right thing**: stop the push, symlink `.venv`, retry (`git push` re-runs the hook clean)
2. **Workaround**: `SKIP=coverage-gate git push` — bypasses the local hook; server CI runs the real gate. Only use if the symlink isn't possible (main repo venv missing).

Per `[[no-lazy-allowlist-tech-debt]]`: workaround #2 is acceptable specifically because the local failure is env, not code; never use it to bypass a real coverage shortfall.

## Cross-references

- `[[worktree-no-cd]]` — sibling rule: always `git -C <worktree>` after creation
- `[[seam-loop-lock-2026-05-19]]` — broader pre-push hook recovery patterns on macOS
- `graph-backend-adapter-contract` plan (draft) — the future world where this lesson can retire
