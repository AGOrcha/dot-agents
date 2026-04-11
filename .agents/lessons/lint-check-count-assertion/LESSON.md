---
name: lint-check-count-assertion
description: When adding a new lint check, update any tests that assert the total checks_run count before running the suite
type: feedback
---

When a test asserts `report.ChecksRun == N` (or similar "total N checks" assertions), adding a new lint check will break it with a confusing off-by-one failure.

**Why:** Tests asserting exact check counts are guards against silently-dropped checks. They're correct and should not be removed — just updated.

**How to apply:** Before adding a new lint check (or any function to a slice-like collection), grep for `ChecksRun`, `checks_run`, or the count literal in tests. Update the assertion to `N+1` (with a comment naming the new check) before running the full suite. Do this first — not after seeing the failure.

Example fix:
```go
// Before
if report.ChecksRun != 7 {

// After — 7 original + integrity_violation (Phase 6A)
if report.ChecksRun != 8 {
```
