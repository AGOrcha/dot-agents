---
schema_version: 1
task_id: p3-starter-promotion
parent_plan_id: loop-discipline-stop-hooks
title: 'Promote iteration-close, isp, loop-worker skills + AGENT.md + loop-worker profile into starter'
summary: 'Retroactive merge-back for archived delegation. PR #109 merged 2026-05-25 (title: "p3-starter-promotion: embed loop-discipline skills + agent + profile into da init starter"). Closeout filed retroactively to free scope for follow-up promotions (pr5-followup-skills, pr5-followup-verifier-xref) in pr10-branch-split plan. No regressions; PR #112 followup landed cog 16→15 in copy_test.go (haiku).'
files_changed:
    - internal/scaffold/home/starter/skills/global/iteration-close/
    - internal/scaffold/home/starter/skills/global/isp/
    - internal/scaffold/home/starter/skills/global/loop-worker/
    - internal/scaffold/home/copy.go
    - internal/scaffold/home/copy_test.go
verification_result:
    status: pass
    summary: PR #109 merged to master; all CI green
integration_notes: Retroactive closeout to free starter/skills/global/ scope for orchestration-skills follow-up (pr5-followup-skills task in pr10-branch-split).
created_at: "2026-05-28T00:00:00Z"
---

## Summary

Retroactive merge-back for PR #109 (p3-starter-promotion). Closeout filed to free `internal/scaffold/home/starter/skills/global/` + `internal/scaffold/home/copy_test.go` scope for the pr5-followup-skills + pr5-followup-verifier-xref follow-up delegations in pr10-branch-split.

## Integration Notes

PR #109 merged 2026-05-25; follow-up promotions (orchestrator-session-start, delegation-lifecycle, plan-wave-picker, provider-consumer-pair) tracked as pr5-followup-skills.
