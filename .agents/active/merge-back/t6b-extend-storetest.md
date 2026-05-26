---
schema_version: 1
task_id: t6b-extend-storetest
parent_plan_id: go-test-fixture-extraction
title: Extend internal/graphstore/internal/storetest with Metadata/Edge/NoteSymbolLink runners
summary: 'Extended internal/graphstore/internal/storetest with 5 prefix-parameterized runners (RunMetadataRoundTrip, RunEdgeUpsertCreate, RunEdgeUpsertUpdate, RunNoteSymbolLinkRoundTrip, RunNoteSymbolLinkIdempotent). Each takes a caller-owned string prefix so the shared Postgres testcontainer doesn''t see cross-test row collisions. Added 7 in-package smoke tests including TestPrefixIsolation_SameStore (every runner twice with two prefixes against one store) and TestPrefixCollision_Negative (documents uniqueness is a caller obligation). Commit 62b9bf70 on feature/t6b-extend-storetest; PR https://github.com/NikashPrakash/dot-agents/pull/90. Anti-scope honored: sqlite_test.go and postgres_test.go untouched (t6c owns consumer migration).'
files_changed:
    - .agents/active/delegation/p1a-mapper-extensions.yaml
verification_result:
    status: pass
    summary: 'Parent should review PR #90 then advance t6b to completed so t6c can unblock. Pre-existing TestCRGBridge env-dependent failure (Python code_review_graph module on PATH) is unrelated; verification skipped it via -skip TestCRGBridge.'
integration_notes: 'Parent should review PR #90 then advance t6b to completed so t6c can unblock. Pre-existing TestCRGBridge env-dependent failure (Python code_review_graph module on PATH) is unrelated; verification skipped it via -skip TestCRGBridge.'
created_at: "2026-05-26T12:30:52Z"
---

## Summary

Extended internal/graphstore/internal/storetest with 5 prefix-parameterized runners (RunMetadataRoundTrip, RunEdgeUpsertCreate, RunEdgeUpsertUpdate, RunNoteSymbolLinkRoundTrip, RunNoteSymbolLinkIdempotent). Each takes a caller-owned string prefix so the shared Postgres testcontainer doesn't see cross-test row collisions. Added 7 in-package smoke tests including TestPrefixIsolation_SameStore (every runner twice with two prefixes against one store) and TestPrefixCollision_Negative (documents uniqueness is a caller obligation). Commit 62b9bf70 on feature/t6b-extend-storetest; PR https://github.com/NikashPrakash/dot-agents/pull/90. Anti-scope honored: sqlite_test.go and postgres_test.go untouched (t6c owns consumer migration).

## Integration Notes

Parent should review PR #90 then advance t6b to completed so t6c can unblock. Pre-existing TestCRGBridge env-dependent failure (Python code_review_graph module on PATH) is unrelated; verification skipped it via -skip TestCRGBridge.
