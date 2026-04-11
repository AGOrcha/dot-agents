---
name: archive-completed-active-plans
description: When a plan is complete, move it out of .agents/active into the matching history folder so active artifacts reflect current work
type: process
---

`.agents/active/` should stay limited to work that is still open, blocked, or actively being executed. When a task is completed, its plan should be archived into `.agents/history/<task>/` alongside any implementation notes or handoffs instead of lingering in the active set.

**Why:** Leaving completed plans in `.agents/active/` makes plan selection noisy, causes agents to re-evaluate already-finished work, and hides the real in-progress items that need attention.

**How to apply:**
1. When closing a task, make sure the plan status or checklist reflects the completed state.
2. Move or copy the finished `*.plan.md` file into the matching `.agents/history/<task>/` directory.
3. Keep `.agents/active/` for plans that are still in progress, intentionally deferred, or serving as the current execution spine.
4. If a task finished without a history directory yet, create one before removing the active plan copy.

**Also normalize remaining plans:** Plans that stay in `.agents/active/` because they are blocked or architectural should have an accurate top-level `Status:` and `Depends on:` header so future agents can immediately classify them without reading the full content. Add these headers on cleanup passes, not after every commit.

**Stale unchecked boxes are noise:** A plan with `Status: Completed` and unchecked `- [ ]` items is a stale hygiene artifact. The status header is authoritative. Do not re-implement already-done work because of unchecked boxes.
