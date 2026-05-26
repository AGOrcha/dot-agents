---
plan_id: loop-discipline-stop-hooks
task_id: p1b-canonical-when-values
created_at: 2026-05-26
type: scope-coupling-observation
disposition: accepted-in-task-pr
---

# p1b: coverage_gap{,5}_test.go pinned now-deprecated cross-platform fall-throughs

## Observation

p1b's contract write_scope is `internal/platform/hooks.go`, `internal/platform/hooks_test.go`, `docs/HOOKS.md`. Two pre-existing tests OUTSIDE the write_scope pin assertions about the fall-through behavior of canonical `When` values that p1b promotes from fall-through to mapped:

- `internal/platform/coverage_gap_test.go` — 2 cases asserting `When: "post_tool_use"` falls through on Copilot. p1b R6.2 promotes that value to `postToolUse` on Copilot, so the assertions now fail.
- `internal/platform/coverage_gap5_test.go` — 3 cases asserting the same on `post_tool_use` (Copilot) + `session_end` (Copilot/Cursor).

The fix is mechanical: swap each pinned `When` value for one that still falls through on every platform (e.g., `no_such_canonical_event`). Committed alongside the p1b hooks.go / hooks_test.go changes in commit 36f7c661 to keep master CI green.

## Pattern

This is a scope-coupling fold-back — adjacent to `[[t11-shim-coverage-defers-full-split]]` and `[[t12-cross-cutting-shim-coverage]]`: write_scope rules expect file-local edits, but the test surface leaks upstream of the refactor when negative-case fixtures pin behavior that the refactor changes. The correct action is to expand write_scope (when justified) rather than fold back the refactor itself — but the bundle author has to anticipate which adjacent tests pin which fall-throughs.

## Recommendation for future contract authors

For tasks that change `*EventName` mapper coverage in `internal/platform/hooks.go`, the contract write_scope SHOULD include `internal/platform/coverage_gap{,5}_test.go` (and any future `coverage_gap*_test.go` files) by default. Add a "scope-rule" comment in the bundle template so this isn't re-discovered each time a mapper is extended.

## Status

Accepted as part of PR #95. No follow-up task needed; the fixture swaps preserve the test's original intent (drive an unmapped canonical-event branch) under the new vendor coverage.
