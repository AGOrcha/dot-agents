---
name: select-star-scan-order
description: When porting DB schemas to Go, SELECT * scans must match exact column order — enumerate columns explicitly or count and comment them inline.
type: feedback
---

When writing `rows.Scan(...)` against a `SELECT *` query, the destination variables must be in the exact column-definition order from the CREATE TABLE statement. Mismatches (such as scanning a field twice, or skipping a column) produce silent wrong-field bindings or a scan error only at runtime — not at compile time.

**Why:** During the CRG→Go port, `scanNode` had `&n.FileHash` scanned at the `return_type` slot because the column ordering wasn't verified against the DDL. The test suite caught it, but only because dedicated round-trip tests existed.

**How to apply:**
1. Prefer `SELECT col1, col2, col3 ...` over `SELECT *` when scanning into a struct — the column list doubles as documentation.
2. If `SELECT *` is used (e.g. for brevity in read helpers), add a comment immediately before the Scan call listing the expected column order and count, e.g. `// id, kind, name, qualified_name, file_path, line_start, line_end, language, parent_name, params, return_type, modifiers, is_test, file_hash, extra, updated_at (16 cols)`.
3. Always write a round-trip test that stores a struct and reads it back field-by-field — this catches scan-order bugs before they reach production.
