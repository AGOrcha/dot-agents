---
schema_version: 1
task_id: c4-skills-command-decomposition
parent_plan_id: command-surface-decomposition
title: Split skills command by lifecycle surface
summary: Split skills list and promote into commands/skills (package skills); kept createSkill and readFrontmatterDescription in commands/skills.go; tests in commands/skills/promote_test.go; docs in active.loop + loop-state.
files_changed: []
verification_result:
    status: pass
    summary: Cherry-picked 0dcc7e3 onto fed005b; resolved iter-52 add/add conflict keeping c3 HEAD prose.
integration_notes: Cherry-picked 0dcc7e3 onto fed005b; resolved iter-52 add/add conflict keeping c3 HEAD prose.
created_at: "2026-04-18T19:32:21Z"
---

## Summary

Split skills list and promote into commands/skills (package skills); kept createSkill and readFrontmatterDescription in commands/skills.go; tests in commands/skills/promote_test.go; docs in active.loop + loop-state.

## Integration Notes

Cherry-picked 0dcc7e3 onto fed005b; resolved iter-52 add/add conflict keeping c3 HEAD prose.
