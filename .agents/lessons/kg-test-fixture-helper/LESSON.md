---
name: kg-test-fixture-helper
description: KG tests that need a populated graph should use setupKGWithNotes() — don't re-seed notes inline in each test
type: feedback
---

Every KG phase from Phase 3 onward needs a graph with multiple note types to test search/query behavior. Use the shared `setupKGWithNotes(t *testing.T) string` helper in `commands/kg_test.go` rather than creating notes inline per test.

**Why:** Inline seeding bloats each test by 15–30 lines of repeated note construction, and diverges over time as note schema evolves. The shared helper gives a consistent fixture: entity (cobra, YAML), decision (use cobra, use YAML), repo (dot-agents).

**How to apply:**
- For tests that need a pre-populated graph, call `home := setupKGWithNotes(t)` — it calls `runKGSetup()` internally and returns the temp KG_HOME.
- If a specific test needs a note type not in the fixture (e.g., synthesis, concept), add it ad-hoc with `createGraphNote` in that test.
- When adding new note types to the fixture is universally useful, update `setupKGWithNotes` — don't fork it.
