---
schema_version: 1
task_id: sq4-duplication-debt-decision
parent_plan_id: sonarqube-pr10
title: Decide scope on new_duplicated_lines_density (this PR vs follow-up plan)
summary: 'ADR-0008 records option (B) — accept new_duplicated_lines_density failure on PR#10 as a known waiver; merge anyway after reviewer affirmation. Follow-up plan IDs reserved: go-test-fixture-extraction (cluster D, ~2,500 lines across 18+ *_test.go files) and production-code-helper-extraction (cluster E, cross-module helpers, needs design review). sq2 may cherry-pick cluster F (commands/{agents,skills}/list.go shared renderer) as a one-commit scope-bump if cheap; folds back to production-code-helper-extraction otherwise. ADR is decision-only; no mass-deduping in PR#10. Reliability (sq2) and security hotspots (sq3) remain in-PR fixes.'
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
    summary: 'Write scope clean: docs/adr/0008-pr10-duplication-scope.md (new), docs/adr/README.md (index row added), .agents/workflow/plans/sonarqube-pr10/findings.md (§2 Decision (sq4) subsection appended after recommended-scope paragraph). Committed in d1fb2ca. Pre-existing dirty state in worktree (TASKS.yaml/PLAN.yaml on other plans, research/* edits) was left untouched per write-scope discipline. Parent should run workflow advance + delegation closeout after reviewing this merge-back; do not auto-advance.'
integration_notes: 'Write scope clean: docs/adr/0008-pr10-duplication-scope.md (new), docs/adr/README.md (index row added), .agents/workflow/plans/sonarqube-pr10/findings.md (§2 Decision (sq4) subsection appended after recommended-scope paragraph). Committed in d1fb2ca. Pre-existing dirty state in worktree (TASKS.yaml/PLAN.yaml on other plans, research/* edits) was left untouched per write-scope discipline. Parent should run workflow advance + delegation closeout after reviewing this merge-back; do not auto-advance.'
created_at: "2026-05-04T03:45:02Z"
---

## Summary

ADR-0008 records option (B) — accept new_duplicated_lines_density failure on PR#10 as a known waiver; merge anyway after reviewer affirmation. Follow-up plan IDs reserved: go-test-fixture-extraction (cluster D, ~2,500 lines across 18+ *_test.go files) and production-code-helper-extraction (cluster E, cross-module helpers, needs design review). sq2 may cherry-pick cluster F (commands/{agents,skills}/list.go shared renderer) as a one-commit scope-bump if cheap; folds back to production-code-helper-extraction otherwise. ADR is decision-only; no mass-deduping in PR#10. Reliability (sq2) and security hotspots (sq3) remain in-PR fixes.

## Integration Notes

Write scope clean: docs/adr/0008-pr10-duplication-scope.md (new), docs/adr/README.md (index row added), .agents/workflow/plans/sonarqube-pr10/findings.md (§2 Decision (sq4) subsection appended after recommended-scope paragraph). Committed in d1fb2ca. Pre-existing dirty state in worktree (TASKS.yaml/PLAN.yaml on other plans, research/* edits) was left untouched per write-scope discipline. Parent should run workflow advance + delegation closeout after reviewing this merge-back; do not auto-advance.
