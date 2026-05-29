---
schema_version: 1
task_id: t01-design-subpackage-shape
parent_plan_id: root-command-decomposition
title: Design target subpackage shape + shared-symbol contract
summary: 'SHAPE.md design contract for root-command-decomposition. Single doc committed at 70fc430 on feature/root-decomp-shape-doc, PR https://github.com/NikashPrakash/dot-agents/pull/56. Captures: final subpackage list (lifecycle/ + mcp/ + settings/ + rules/), cmdutil home (commands/internal/cmdutil), per-symbol export decisions (all lifecycle helpers stay package-private post-t11 seam split), DI handling (preserve package-var seams, interface-DI in t15), root shim policy (delete in t13), per-cluster commit cadence, and KG-equivalent external-caller grep showing only NewRootCommand + RenderCommandError are imported externally. 7 ODs surfaced.'
files_changed: []
verification_result:
    status: pass
    summary: 'DO NOT MERGE the PR before parent review. ODs to track for downstream: OD-1 (re-run KG queries before t13), OD-2 (linkcount helper export window t08->t09), OD-3 (wiring_test.go post-shim-strip viability), OD-4 (testutil promotion decision at t11), OD-5 (importguard home for t14), OD-6 (t10pre->t10c serialization), OD-7 (NewDeps factory naming convention). All 7 are routed to specific downstream tasks — none block t01 advance. KG MCP tools were unreachable in this sandbox; grep -rn fallback over the Go module is equivalent for an internal module with no go:linkname. Re-running via mcp__code-review-graph before t13 is OD-1.'
integration_notes: 'DO NOT MERGE the PR before parent review. ODs to track for downstream: OD-1 (re-run KG queries before t13), OD-2 (linkcount helper export window t08->t09), OD-3 (wiring_test.go post-shim-strip viability), OD-4 (testutil promotion decision at t11), OD-5 (importguard home for t14), OD-6 (t10pre->t10c serialization), OD-7 (NewDeps factory naming convention). All 7 are routed to specific downstream tasks — none block t01 advance. KG MCP tools were unreachable in this sandbox; grep -rn fallback over the Go module is equivalent for an internal module with no go:linkname. Re-running via mcp__code-review-graph before t13 is OD-1.'
created_at: "2026-05-24T19:10:30Z"
---

## Summary

SHAPE.md design contract for root-command-decomposition. Single doc committed at 70fc430 on feature/root-decomp-shape-doc, PR https://github.com/NikashPrakash/dot-agents/pull/56. Captures: final subpackage list (lifecycle/ + mcp/ + settings/ + rules/), cmdutil home (commands/internal/cmdutil), per-symbol export decisions (all lifecycle helpers stay package-private post-t11 seam split), DI handling (preserve package-var seams, interface-DI in t15), root shim policy (delete in t13), per-cluster commit cadence, and KG-equivalent external-caller grep showing only NewRootCommand + RenderCommandError are imported externally. 7 ODs surfaced.

## Integration Notes

DO NOT MERGE the PR before parent review. ODs to track for downstream: OD-1 (re-run KG queries before t13), OD-2 (linkcount helper export window t08->t09), OD-3 (wiring_test.go post-shim-strip viability), OD-4 (testutil promotion decision at t11), OD-5 (importguard home for t14), OD-6 (t10pre->t10c serialization), OD-7 (NewDeps factory naming convention). All 7 are routed to specific downstream tasks — none block t01 advance. KG MCP tools were unreachable in this sandbox; grep -rn fallback over the Go module is equivalent for an internal module with no go:linkname. Re-running via mcp__code-review-graph before t13 is OD-1.
