---
schema_version: 1
task_id: t3-extend-iteration-close
parent_plan_id: self-review-iteration-close-wiring
title: Wire iteration-close to invoke self-review and call workflow checkpoint --role review
summary: 'Wired self-review into iteration-close per ADRs 0002 (output schema) and 0003 (fire ordering = AFTER verify-record-test, BEFORE checkpoint). SKILL.md gained tier:T2 + contract{reads,writes,escape_hatches} matching t2''s self-review pattern. instructions/workflow.md inserted a new ''Invoke Self-Review'' section with three sub-steps: (1) /self-review writes .agents/active/verification/<task_id>/review-decision.yaml; (2) da workflow verify record --kind review --task <id> --phase1-decision … --phase2-decision … picks the YAML up via the existing review-record path; (3) da workflow checkpoint --log-to-iter <N> --role review fires mergeReviewIterLog to populate iter-N.yaml''s review block. Failure modes documented: accept->proceed; reject->halt closeout; escalate->fold-back. Anti-scope held: no redesign of mergeReviewIterLog, verify record, or checkpoint flag surface. False-positive guard (''omitting --role review leaves review block empty'') is called out explicitly in workflow.md. Frontmatter parses as valid YAML; go build ./... clean. Closes the dead-coded iter-log review-block path the plan was created to fix.'
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
    summary: 'Files modified (write_scope only): ~/.agents/skills/dot-agents/iteration-close/SKILL.md (66 lines) and ~/.agents/skills/dot-agents/iteration-close/instructions/workflow.md (260 lines), reachable from the repo via the symlink at .agents/skills/iteration-close. No code changes (commands/workflow/* untouched); no schema changes. Parent should: (1) review the new ''Invoke Self-Review'' section in workflow.md for ADR-0003 ordering compliance; (2) confirm the contract block in SKILL.md follows t2''s self-review template; (3) run advance and delegation closeout. t5-verify-end-to-end is the dependent task that exercises the full chain on a real diff and produces the iter-N.yaml hard-test evidence.'
integration_notes: 'Files modified (write_scope only): ~/.agents/skills/dot-agents/iteration-close/SKILL.md (66 lines) and ~/.agents/skills/dot-agents/iteration-close/instructions/workflow.md (260 lines), reachable from the repo via the symlink at .agents/skills/iteration-close. No code changes (commands/workflow/* untouched); no schema changes. Parent should: (1) review the new ''Invoke Self-Review'' section in workflow.md for ADR-0003 ordering compliance; (2) confirm the contract block in SKILL.md follows t2''s self-review template; (3) run advance and delegation closeout. t5-verify-end-to-end is the dependent task that exercises the full chain on a real diff and produces the iter-N.yaml hard-test evidence.'
created_at: "2026-05-04T03:55:54Z"
---

## Summary

Wired self-review into iteration-close per ADRs 0002 (output schema) and 0003 (fire ordering = AFTER verify-record-test, BEFORE checkpoint). SKILL.md gained tier:T2 + contract{reads,writes,escape_hatches} matching t2's self-review pattern. instructions/workflow.md inserted a new 'Invoke Self-Review' section with three sub-steps: (1) /self-review writes .agents/active/verification/<task_id>/review-decision.yaml; (2) da workflow verify record --kind review --task <id> --phase1-decision … --phase2-decision … picks the YAML up via the existing review-record path; (3) da workflow checkpoint --log-to-iter <N> --role review fires mergeReviewIterLog to populate iter-N.yaml's review block. Failure modes documented: accept->proceed; reject->halt closeout; escalate->fold-back. Anti-scope held: no redesign of mergeReviewIterLog, verify record, or checkpoint flag surface. False-positive guard ('omitting --role review leaves review block empty') is called out explicitly in workflow.md. Frontmatter parses as valid YAML; go build ./... clean. Closes the dead-coded iter-log review-block path the plan was created to fix.

## Integration Notes

Files modified (write_scope only): ~/.agents/skills/dot-agents/iteration-close/SKILL.md (66 lines) and ~/.agents/skills/dot-agents/iteration-close/instructions/workflow.md (260 lines), reachable from the repo via the symlink at .agents/skills/iteration-close. No code changes (commands/workflow/* untouched); no schema changes. Parent should: (1) review the new 'Invoke Self-Review' section in workflow.md for ADR-0003 ordering compliance; (2) confirm the contract block in SKILL.md follows t2's self-review template; (3) run advance and delegation closeout. t5-verify-end-to-end is the dependent task that exercises the full chain on a real diff and produces the iter-N.yaml hard-test evidence.
