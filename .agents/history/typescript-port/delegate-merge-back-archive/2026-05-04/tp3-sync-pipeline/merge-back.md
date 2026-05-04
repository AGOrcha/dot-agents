---
schema_version: 1
task_id: tp3-sync-pipeline
parent_plan_id: typescript-port
title: Implement CI sync check that diffs Go command tree against the boundary spec
summary: 'tp3-sync-pipeline complete. Added ports/typescript/scripts/check-boundary.ts (static Go cobra parser + TS commander parser + diff against docs/typescript-port-boundary.json), ports/typescript/tests/boundary-sync.test.ts (5 vitest assertions, asserts zero violations), and .github/workflows/typescript-port.yml (npm ci + npm run build + npx vitest tests/boundary-sync.test.ts on push/PR to master and feature/*). Validation experiment: added a fake Go top-level command in commands/root.go + commands/fake.go, ran the check, confirmed it failed with [go-unclassified-top-level] actionable diagnostic; reverted both files. Anti-scope honored: no Go-side __dump-tree subcommand. All 99 TS tests pass; go vet ./commands/... clean; go build ./... clean.'
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
    summary: 'Out-of-scope concession: did NOT add tsconfig.scripts.json or a check:boundary npm script (would have required modifying package.json outside write_scope) — CI relies on vitest to transpile and run the script. If a future iteration wants standalone node ./dist-scripts/check-boundary.js, add a separate tsconfig + npm script in a follow-up task. Per task instructions: NOT advancing — parent should review and run workflow advance.'
integration_notes: 'Out-of-scope concession: did NOT add tsconfig.scripts.json or a check:boundary npm script (would have required modifying package.json outside write_scope) — CI relies on vitest to transpile and run the script. If a future iteration wants standalone node ./dist-scripts/check-boundary.js, add a separate tsconfig + npm script in a follow-up task. Per task instructions: NOT advancing — parent should review and run workflow advance.'
created_at: "2026-05-04T04:12:01Z"
---

## Summary

tp3-sync-pipeline complete. Added ports/typescript/scripts/check-boundary.ts (static Go cobra parser + TS commander parser + diff against docs/typescript-port-boundary.json), ports/typescript/tests/boundary-sync.test.ts (5 vitest assertions, asserts zero violations), and .github/workflows/typescript-port.yml (npm ci + npm run build + npx vitest tests/boundary-sync.test.ts on push/PR to master and feature/*). Validation experiment: added a fake Go top-level command in commands/root.go + commands/fake.go, ran the check, confirmed it failed with [go-unclassified-top-level] actionable diagnostic; reverted both files. Anti-scope honored: no Go-side __dump-tree subcommand. All 99 TS tests pass; go vet ./commands/... clean; go build ./... clean.

## Integration Notes

Out-of-scope concession: did NOT add tsconfig.scripts.json or a check:boundary npm script (would have required modifying package.json outside write_scope) — CI relies on vitest to transpile and run the script. If a future iteration wants standalone node ./dist-scripts/check-boundary.js, add a separate tsconfig + npm script in a follow-up task. Per task instructions: NOT advancing — parent should review and run workflow advance.
