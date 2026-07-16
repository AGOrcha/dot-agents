---
schema_version: 1
task_id: craft-reference-stable-relocation
parent_plan_id: transcript-analysis-and-pipeline-craft
title: Relocate tailored full-loop craft reference to docs/ and repoint pipeline-architect skill
summary: 'Authored the tailored operational craft reference at docs/full-loop-pipeline-craft.md (404 lines, 9 sections): sections 1-7 made model/registry-agnostic (all plan-evidence anchors stripped) plus a new context-budget/compaction section grounded in research R4. Plan-scoped craft/full-loop-craft.md left unchanged as the evidence-spec. The 6 pipeline-architect citation repoints are a tracked follow-up (gitignored machine state, not committable).'
files_changed:
    - docs/full-loop-pipeline-craft.md
verification_result:
    status: pass
    summary: 'Parent: 1 file +404; 9 sections; zero leaked model/plan anchors (verified by grep); TOC + cross-section + research relative link resolve. feedback_goal YES.'
integration_notes: 'Branch feat/transcript-craft-relocation @ b9918630 → PR to master. FOLLOW-UP (not blocking, old citation still resolves since plan craft/ preserved): repoint 6 pipeline-architect citations (SKILL.md ~L3/L12 + 5 instruction files ~L4) from the plan-scoped craft path to docs/full-loop-pipeline-craft.md — targets are gitignored machine state ~/.agents/skills/pipeline-architect/; the tracked scaffold source should also add the citation. Fold into (fb:scaffold-home-skill-sync-mechanism).'
created_at: "2026-07-16T05:10:00Z"
---

## Summary
Tailored operational reference authored at `docs/full-loop-pipeline-craft.md` (model-agnostic, +context-budget §). Plan-scoped `craft/full-loop-craft.md` preserved as evidence-spec.

## Verification
- 1 file, +404; 9 `## ` sections; 0 leaked model/plan anchors; links resolve.
- feedback_goal: YES.

## Follow-ups (tracked, not blocking)
Repoint 6 pipeline-architect skill citations to the stable docs path — gitignored machine state; fold into `(fb:scaffold-home-skill-sync-mechanism)` so the tracked scaffold cites `docs/full-loop-pipeline-craft.md`.
