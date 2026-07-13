---
type: "example"
quality: "good"
---

# Good Self-Review Example

## Context
Adding a new API endpoint that fetches user preferences.

## Review Output

### Self-Review Summary

**Files reviewed:** 4
**Issues found:** 2
**Severity breakdown:** 0 critical, 1 warning, 1 info

### Findings

1. **WARNING** `src/api/preferences.ts:42` — Missing error handling on database query. If the connection drops mid-request, the promise rejects unhandled.
   - **Fix:** Wrap in try/catch, return 503 with retry-after header.

2. **INFO** `src/api/preferences.ts:15` — Function `getPrefs` could be named `getUserPreferences` for clarity since other modules also have preference-like getters.
   - **Fix:** Rename to `getUserPreferences` and update the single call site.

### Advisory Board

**Security Auditor:** No issues. Input is validated via schema middleware. No secrets exposed.

**Performance Engineer:** WARNING — The query at line 42 fetches all columns. Only `theme` and `locale` are used. Select only needed columns to reduce payload and index usage.

**Maintainability Advocate:** INFO — Consider adding a JSDoc comment on the exported handler explaining the expected query parameters.

### Checklist Result
**Result:** PASS (after fixing the warning)
**Failing items:** #8 (error handling) — fixed before commit.

### Verdict
PASS — ready to commit after applying the two fixes above.
