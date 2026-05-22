---
name: test-stub-upgrade-pattern
description: When upgrading a stub implementation to a real one, find and update tests that assert stub behavior (e.g., "expected warning: not yet implemented") before running the full suite
type: feedback
---

When a phase implements behavior that was previously stubbed, existing tests that assert the stub's side-effects will break (e.g., "expected stub warning for contradictions" fails once the real function runs).

**Why:** Stub tests verify placeholder behavior — warnings like "not yet implemented". Once the real implementation ships, those assertions are wrong by design, not by regression.

**How to apply:**
1. Before running `go test ./...` after a stub-upgrade, grep the test file for any test name or assertion referencing the intent/function being upgraded.
   ```bash
   grep -n "stub\|not yet\|Stub" commands/kg_test.go
   ```
2. Update those tests to assert real behavior (or rename from `_Stub` to `_Live`/`_NoConflict`/`_Empty` as appropriate).
3. Only then run the full suite — catching it proactively avoids a red-then-fix cycle.
