---
schema_version: 1
task_id: tp1-current-gap-audit
parent_plan_id: typescript-port
title: Audit current Go ↔ TS command surface gap; produce checked-in gap memo
summary: 'tp1 complete: docs/TYPESCRIPT_PORT_GAP.md is the row-level gap memo. Section 1 classifies all 19 Go top-level commands (8 Stage 1 mirrored, 11 Phase 4 opt-outs incl. workflow-write surface, plus read-only workflow as Phase 5 deferred). Section 2 enumerates subcommand/flag depth for init/add/refresh/status/doctor/skills/agents/hooks with implemented/partial/missing/deferred-by-design markers. Section 3 captures Stage 2 deferred items: plugin spec section in status, read-only workflow CLI surfaces, agentsrc ExtraFields preservation; clarifies KG and workflow-mutating subcommands stay permanently Go-only (not Phase 5 candidates). Memo includes consumer guidance for tp2 (5 JSON fields) and tp3 (false-positive guard). All classifications trace to docs/TYPESCRIPT_PORT_BOUNDARY.md anchors.'
files_changed:
    - .agents/workflow/plans/binary-rename-da-sweep/PLAN.yaml
    - .agents/workflow/plans/binary-rename-da-sweep/TASKS.yaml
    - .agents/workflow/plans/binary-rename-da-sweep/binary-rename-da-sweep.plan.md
    - .agents/workflow/plans/self-review-iteration-close-wiring/PLAN.yaml
    - .agents/workflow/plans/self-review-iteration-close-wiring/TASKS.yaml
    - .agents/workflow/plans/sonarqube-pr10/PLAN.yaml
    - .agents/workflow/plans/sonarqube-pr10/TASKS.yaml
    - .agents/workflow/plans/typescript-port/PLAN.yaml
    - .agents/workflow/plans/typescript-port/TASKS.yaml
verification_result:
    status: pass
    summary: Pre-existing dirty state outside write_scope (modified plan YAMLs in self-review-iteration-close-wiring/sonarqube-pr10/typescript-port, deleted binary-rename-da-sweep plan dir, untracked delegation bundles for sq1/t2/tp1) was left untouched per worker rules. Parent should run workflow advance for tp1 after reviewing the memo.
integration_notes: Pre-existing dirty state outside write_scope (modified plan YAMLs in self-review-iteration-close-wiring/sonarqube-pr10/typescript-port, deleted binary-rename-da-sweep plan dir, untracked delegation bundles for sq1/t2/tp1) was left untouched per worker rules. Parent should run workflow advance for tp1 after reviewing the memo.
created_at: "2026-05-04T03:19:42Z"
---

## Summary

tp1 complete: docs/TYPESCRIPT_PORT_GAP.md is the row-level gap memo. Section 1 classifies all 19 Go top-level commands (8 Stage 1 mirrored, 11 Phase 4 opt-outs incl. workflow-write surface, plus read-only workflow as Phase 5 deferred). Section 2 enumerates subcommand/flag depth for init/add/refresh/status/doctor/skills/agents/hooks with implemented/partial/missing/deferred-by-design markers. Section 3 captures Stage 2 deferred items: plugin spec section in status, read-only workflow CLI surfaces, agentsrc ExtraFields preservation; clarifies KG and workflow-mutating subcommands stay permanently Go-only (not Phase 5 candidates). Memo includes consumer guidance for tp2 (5 JSON fields) and tp3 (false-positive guard). All classifications trace to docs/TYPESCRIPT_PORT_BOUNDARY.md anchors.

## Integration Notes

Pre-existing dirty state outside write_scope (modified plan YAMLs in self-review-iteration-close-wiring/sonarqube-pr10/typescript-port, deleted binary-rename-da-sweep plan dir, untracked delegation bundles for sq1/t2/tp1) was left untouched per worker rules. Parent should run workflow advance for tp1 after reviewing the memo.
