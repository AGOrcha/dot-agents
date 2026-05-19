---
schema_version: 1
task_id: sq2-reliability-rating-fixes
parent_plan_id: sonarqube-pr10
title: Address new_reliability_rating findings classified as fixable
summary: 'sq2 reliability fixes: applied Cluster A (shell single-bracket -> double-bracket in add.sh/json.sh/verify.sh, ~44 sites), Cluster B (TS replace -> replaceAll across workflow.ts/agents.ts/skills.ts/codex.ts, 11 sites), Cluster C (TS sort -> sort with localeCompare across core/{config,hooks,mcp}.ts and tests/{agentsrc,commands}.test.ts, 6 sites). Two commits landed: ca15ef3 (Cluster A) and b941530 (Clusters B+C). No // NOSONAR / # noqa suppressions.'
files_changed:
    - .agents/active/delegation-bundles/del-sq1-current-state-audit-1777864449.yaml
    - .agents/active/delegation-bundles/del-t2-extend-self-review-skill-1777864417.yaml
    - .agents/active/delegation-bundles/del-tp1-current-gap-audit-1777864446.yaml
    - .agents/active/delegation/sq1-current-state-audit.yaml
    - .agents/active/delegation/t2-extend-self-review-skill.yaml
    - .agents/active/delegation/tp1-current-gap-audit.yaml
    - .agents/active/merge-back/sq1-current-state-audit.md
    - .agents/active/merge-back/t2-extend-self-review-skill.md
    - .agents/active/merge-back/tp1-current-gap-audit.md
    - .agents/active/verification/sq1-current-state-audit/doc-audit.result.yaml
    - .agents/active/verification/sq1-current-state-audit/merge-back.result.yaml
    - .agents/active/verification/t2-extend-self-review-skill/merge-back.result.yaml
    - .agents/active/verification/t2-extend-self-review-skill/review-decision.yaml
    - .agents/active/verification/t2-extend-self-review-skill/skill-extension.result.yaml
    - .agents/active/verification/tp1-current-gap-audit/doc-audit.result.yaml
    - .agents/active/verification/tp1-current-gap-audit/merge-back.result.yaml
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
    summary: 'Hard test: parent must verify new_reliability_rating == 1 (A) on PR#10 SonarCloud incremental analysis after push. Sandbox prevented local bash -n / npm run build / npm test execution; verification relied on (a) grep audits confirming zero remaining single-bracket test sites, zero remaining .replace(/.../g) calls, zero bare .sort() on string arrays, and (b) self-review of the unified diff. Pre-existing dirty files outside write_scope (e.g. ports/typescript/package.json modification, .agents/active/* files) were left untouched per loop-worker rules. No fold-back observations needed; all sq1-classified findings were addressable as marked.'
integration_notes: 'Hard test: parent must verify new_reliability_rating == 1 (A) on PR#10 SonarCloud incremental analysis after push. Sandbox prevented local bash -n / npm run build / npm test execution; verification relied on (a) grep audits confirming zero remaining single-bracket test sites, zero remaining .replace(/.../g) calls, zero bare .sort() on string arrays, and (b) self-review of the unified diff. Pre-existing dirty files outside write_scope (e.g. ports/typescript/package.json modification, .agents/active/* files) were left untouched per loop-worker rules. No fold-back observations needed; all sq1-classified findings were addressable as marked.'
created_at: "2026-05-04T04:10:00Z"
---

## Summary

sq2 reliability fixes: applied Cluster A (shell single-bracket -> double-bracket in add.sh/json.sh/verify.sh, ~44 sites), Cluster B (TS replace -> replaceAll across workflow.ts/agents.ts/skills.ts/codex.ts, 11 sites), Cluster C (TS sort -> sort with localeCompare across core/{config,hooks,mcp}.ts and tests/{agentsrc,commands}.test.ts, 6 sites). Two commits landed: ca15ef3 (Cluster A) and b941530 (Clusters B+C). No // NOSONAR / # noqa suppressions.

## Integration Notes

Hard test: parent must verify new_reliability_rating == 1 (A) on PR#10 SonarCloud incremental analysis after push. Sandbox prevented local bash -n / npm run build / npm test execution; verification relied on (a) grep audits confirming zero remaining single-bracket test sites, zero remaining .replace(/.../g) calls, zero bare .sort() on string arrays, and (b) self-review of the unified diff. Pre-existing dirty files outside write_scope (e.g. ports/typescript/package.json modification, .agents/active/* files) were left untouched per loop-worker rules. No fold-back observations needed; all sq1-classified findings were addressable as marked.
