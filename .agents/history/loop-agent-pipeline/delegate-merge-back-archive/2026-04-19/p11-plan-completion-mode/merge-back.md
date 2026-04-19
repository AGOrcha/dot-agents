---
schema_version: 1
task_id: p11-plan-completion-mode
parent_plan_id: loop-agent-pipeline
title: Scoped plan-completion mode with planning locks and human-reviewed architectural pause points
summary: 'Scoped plan-completion mode implemented: RALPH_RUN_PLAN env var scopes pipeline to one plan or comma-separated list; scoped_completion_state() drives D4/D14 break check via ''workflow complete --json --plan''; workflow complete command returns locked/actionable/paused/drained states; plan-lock semantics work via locked completion state; review gate (RALPH_REVIEW_GATE_AUTO) per bundle separates review from closeout; post-closeout fold-back audit optional via RALPH_POST_CLOSEOUT_FOLD_BACK_AUDIT; all role-specific RALPH_*_AGENT_BIN knobs documented and wired in ralph-worker dispatch'
files_changed:
    - .agents/workflow/plans/graph-bridge-command-readiness/TASKS.yaml
    - .agents/workflow/plans/loop-agent-pipeline/PLAN.yaml
    - .agents/workflow/plans/loop-agent-pipeline/TASKS.yaml
verification_result:
    status: pass
    summary: Replacement-agent retry for rate-limited providers (described in TASKS.yaml notes) is not implemented. Routed to fold-back. All other scoped-completion-mode criteria from the delegation contract's done_means are satisfied.
integration_notes: Replacement-agent retry for rate-limited providers (described in TASKS.yaml notes) is not implemented. Routed to fold-back. All other scoped-completion-mode criteria from the delegation contract's done_means are satisfied.
created_at: "2026-04-19T15:14:55Z"
---

## Summary

Scoped plan-completion mode implemented: RALPH_RUN_PLAN env var scopes pipeline to one plan or comma-separated list; scoped_completion_state() drives D4/D14 break check via 'workflow complete --json --plan'; workflow complete command returns locked/actionable/paused/drained states; plan-lock semantics work via locked completion state; review gate (RALPH_REVIEW_GATE_AUTO) per bundle separates review from closeout; post-closeout fold-back audit optional via RALPH_POST_CLOSEOUT_FOLD_BACK_AUDIT; all role-specific RALPH_*_AGENT_BIN knobs documented and wired in ralph-worker dispatch

## Integration Notes

Replacement-agent retry for rate-limited providers (described in TASKS.yaml notes) is not implemented. Routed to fold-back. All other scoped-completion-mode criteria from the delegation contract's done_means are satisfied.
