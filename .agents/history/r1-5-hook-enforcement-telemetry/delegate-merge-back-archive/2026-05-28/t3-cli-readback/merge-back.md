---
schema_version: 1
task_id: t3-cli-readback
parent_plan_id: r1-5-hook-enforcement-telemetry
title: Render hook outcome contribution in score queries
summary: Extended da score iteration (text + JSON) to surface hook_outcomes sentinel_id + rule_id sources from iter-N.hook-outcomes.yaml. Added hookOutcomeSource projection (sentinel_id, rule_id, result, lifecycle_point, intervention_class, correlation_id), loader (loadHookOutcomeSources) that filters to prevent_before_action + remediate_at_stop matching the scoring extractor, deterministic sort by (rule_id, sentinel_id), and a 'Hook outcome sources' text block + hook_outcome_sources JSON field. Both gated on (hook row present + sidecar readable); missing/malformed sidecar degrades silently. Transcript content never read or printed by construction. 4 new tests (12 subtests) all pass; full ./... regression green except pre-existing TestCRGBridgeFreshBuildRealCRG (missing python module, unrelated).
files_changed: []
verification_result:
    status: pass
    summary: Branch t3-cli-readback (commit b10d59f5) ready for PR. Touches only commands/score.go + commands/score_test.go per write_scope. No internal/scoring/ edits per anti-scope. Uses existing wf.HookOutcomeSidecar type from commands/workflow.
integration_notes: Branch t3-cli-readback (commit b10d59f5) ready for PR. Touches only commands/score.go + commands/score_test.go per write_scope. No internal/scoring/ edits per anti-scope. Uses existing wf.HookOutcomeSidecar type from commands/workflow.
created_at: "2026-05-27T00:38:38Z"
---

## Summary

Extended da score iteration (text + JSON) to surface hook_outcomes sentinel_id + rule_id sources from iter-N.hook-outcomes.yaml. Added hookOutcomeSource projection (sentinel_id, rule_id, result, lifecycle_point, intervention_class, correlation_id), loader (loadHookOutcomeSources) that filters to prevent_before_action + remediate_at_stop matching the scoring extractor, deterministic sort by (rule_id, sentinel_id), and a 'Hook outcome sources' text block + hook_outcome_sources JSON field. Both gated on (hook row present + sidecar readable); missing/malformed sidecar degrades silently. Transcript content never read or printed by construction. 4 new tests (12 subtests) all pass; full ./... regression green except pre-existing TestCRGBridgeFreshBuildRealCRG (missing python module, unrelated).

## Integration Notes

Branch t3-cli-readback (commit b10d59f5) ready for PR. Touches only commands/score.go + commands/score_test.go per write_scope. No internal/scoring/ edits per anti-scope. Uses existing wf.HookOutcomeSidecar type from commands/workflow.
