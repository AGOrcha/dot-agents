---
schema_version: 1
task_id: p0-schema-additive-extension
parent_plan_id: config-v2-migration
title: 'Phase 0: additive v2 schema extension (preserves v1 compat)'
summary: 'v2 additive schema landed; PR #124 READY (mergeStateStatus=CLEAN, all checks pass: macos+ubuntu+windows, sonar, coverage gate, lint). v1 byte-stability preserved via testdata/v1 fixture round-trip; v2 fixtures (minimal + full + cross-tier-shape) cover the new field surface. AgentsRC + Source structs extended atomically per [[schema-usage]] 6-place rule (struct + core mirror + Unmarshal + Marshal + agentsRCKnown + JSON schema). Pre-existing schema bug (refresh.required-on-property invalid under Draft 2020-12) fixed as collateral so the new jsonschema/v6 compile-based tests can run.'
files_changed: []
verification_result:
    status: pass
    summary: Next phase p1-resolver-core-flat consumes these fields. No CLI behavior change in this PR. Cross-tier source matching (extends->git|http|local, packages->oci|http) is shape-validated by patterns; the source-id lookup constraint is resolver-layer (p1b). Schema accepts both version 1 and 2.
integration_notes: Next phase p1-resolver-core-flat consumes these fields. No CLI behavior change in this PR. Cross-tier source matching (extends->git|http|local, packages->oci|http) is shape-validated by patterns; the source-id lookup constraint is resolver-layer (p1b). Schema accepts both version 1 and 2.
created_at: "2026-05-27T16:26:24Z"
---

## Summary

v2 additive schema landed; PR #124 READY (mergeStateStatus=CLEAN, all checks pass: macos+ubuntu+windows, sonar, coverage gate, lint). v1 byte-stability preserved via testdata/v1 fixture round-trip; v2 fixtures (minimal + full + cross-tier-shape) cover the new field surface. AgentsRC + Source structs extended atomically per [[schema-usage]] 6-place rule (struct + core mirror + Unmarshal + Marshal + agentsRCKnown + JSON schema). Pre-existing schema bug (refresh.required-on-property invalid under Draft 2020-12) fixed as collateral so the new jsonschema/v6 compile-based tests can run.

## Integration Notes

Next phase p1-resolver-core-flat consumes these fields. No CLI behavior change in this PR. Cross-tier source matching (extends->git|http|local, packages->oci|http) is shape-validated by patterns; the source-id lookup constraint is resolver-layer (p1b). Schema accepts both version 1 and 2.
