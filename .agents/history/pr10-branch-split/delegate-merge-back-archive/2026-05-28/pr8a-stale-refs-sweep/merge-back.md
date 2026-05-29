---
schema_version: 1
task_id: pr8a-stale-refs-sweep
parent_plan_id: pr10-branch-split
title: 'Sweep remaining stale cmd/dot-agents refs left out of pr8a'
summary: 'PR #142 merged. Scope: lessons + workflow plans + workflow specs + bin/tests/ralph-* + scaffold starter templates (agents/skills/profiles/global/). Excluded: .agents/proposals/ (historical), docs/ (separate). 11/11 checks pass + SonarCloud gate OK.'
files_changed: []
verification_result:
    status: pass
    summary: 11/11 checks; SonarCloud gate OK; 0 issues
integration_notes: Completes pr8a-cmd-da-rename follow-up sweep. Scaffold templates + ralph harnesses no longer reference cmd/dot-agents.
created_at: "2026-05-28T00:00:00Z"
---

## Summary

PR #142 merged. cmd/dot-agents → cmd/da sweep across lessons, plans, specs, ralph harnesses, and scaffold starter templates.
