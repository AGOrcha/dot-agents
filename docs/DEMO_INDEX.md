---
title: Demo Materials Index
description: A skim-friendly index of demo materials, decks, and walkthroughs for dot-agents.
sidebar:
  order: 4
---

# dot-agents — Demo Materials Index

A skim-friendly index of materials prepared (and proposed) for leadership /
tech-lead demos of dot-agents.

**Audience:** anyone preparing a demo, talking deck, or written walkthrough.
**Status legend:** ✅ ready · 🟡 partial / needs polish · ⬜ proposed, not authored.

---

## Authored demo material

| File | Status | Purpose | Format |
|---|---|---|---|
| [`DEMO_DIAGRAM.md`](./DEMO_DIAGRAM.md) | ✅ ready | Two Mermaid diagrams + talk tracks: "what dot-agents is" + "how the workflow loop works". 5-minute live demo script. | Diagram + narrated script |
| [`DEMO_README.md`](./DEMO_README.md) | ✅ ready | 1-minute architecture overview — the landing page for someone who has never seen dot-agents. | README-style with one diagram |
| [`DEMO_WORKFLOW_WALKTHROUGH.md`](./DEMO_WORKFLOW_WALKTHROUGH.md) | ✅ ready | End-to-end narrated example of a real wave: plan → fanout → workers → merge-back → closeout, traced through merged PRs #130/#133/#135 (platform-driven diagnostics). | Narrated walkthrough with commit/PR refs |
| [`DEMO_LESSONS_NARRATIVE.md`](./DEMO_LESSONS_NARRATIVE.md) | ✅ ready | Story-format walkthrough of the most-instructive lessons from `.agents/lessons/` — what we figured out the hard way. | Short article |

## Supporting / canonical reference (already in `docs/`)

These pre-existing docs are demo-ready as supporting material; cite them, don't
duplicate them.

| File | Use as |
|---|---|
| [`LOOP_ORCHESTRATION_SPEC.md`](./LOOP_ORCHESTRATION_SPEC.md) | Authoritative description of the orchestrator → delegation → fold-back loop. |
| [`RESOURCE_COMMAND_CONTRACT.md`](./RESOURCE_COMMAND_CONTRACT.md) | The `rules/skills/agents/hooks/mcp/settings` resource-command parity contract. |
| [`PLATFORM_DIRS_DOCS.md`](./PLATFORM_DIRS_DOCS.md) | Per-platform directory conventions (Cursor, Claude, Codex, Copilot, OpenCode). |
| [`KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`](./KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md) | KG design + bridge contract. |
| [`WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`](./WORKFLOW_AUTOMATION_PRODUCT_SPEC.md) | The product framing the demo is selling. |

## Proposed (future-prep, not yet authored)

Listed here so the next demo cycle has a backlog rather than a blank page.

| Material | Why it would matter | Effort estimate |
|---|---|---|
| ⬜ `DEMO_CLI_TOUR.md` — cheat-sheet of all `da` subcommands with example output | Operators want a one-pager they can pin. The `da explain` surface already covers some of this; a curated overlay would help. | M — needs CLI-output capture + curation |
| ⬜ `DEMO_CODEX_THREAD_CASE_STUDY.md` — story of how an LLM-driven design contribution (the codex-019e6245 thread) got captured, audited, sequenced into 6 phases | Concrete demonstration of human-in-the-loop governance over AI design work. Source material is the [examination + sequenced plan proposal](../.agents/proposals/codex-019e6245-examination-and-sequenced-plan.md). | M — source material is dense (~600 lines); needs narrative compression |
| ⬜ `DEMO_SONAR_CI_RECIPE.md` — how the `pr-ci` verifier_profile + SonarCloud + GitHub Actions catches issues pre-merge | Closes the "what about quality gates?" question from engineering leadership. Lesson source: [`verifier-owns-ci-watch-shift-left`](../.agents/lessons/verifier-owns-ci-watch-shift-left/LESSON.md). | M — needs CI run screenshots + a worked example |
| ⬜ `DEMO_MULTI_AGENT_DISPATCH.md` — Cursor / Claude / Codex / Copilot / OpenCode parity matrix + per-platform diagnostics | Validates platform-neutral story; useful for orgs already on a specific stack. The new `BrokenLinkReporter` + `StatusBadger` + `LinkCounter` interfaces (PRs #130, #133, #135) are the canonical source. | S — comparison table from `internal/platform/` + screenshots of `da doctor --json` |
| ⬜ `DEMO_CONFIG_V2_VISION.md` — the two-tier sources/extends/packages model + audit-grounded migration roadmap | Forward-looking material for orgs evaluating long-term direction. Source: [`config-v2-dependency-map.md`](../.agents/proposals/config-v2-dependency-map.md), PR #124 (additive schema landed). | M — needs spec excerpt + roadmap framing |

---

## Demo planning notes

- **First 5 minutes** → `DEMO_DIAGRAM.md` (architecture + loop, no live CLI).
- **Next 5 minutes** → `DEMO_WORKFLOW_WALKTHROUGH.md` (replay one wave from
  merged PRs; optionally swap for live `da workflow orient` + `da workflow
  next`).
- **Closing** → 30-second roadmap pull from `DEMO_DIAGRAM.md` §"Demo script" +
  the codex-019e6245 sequenced plan.
- **Q&A backup** → `DEMO_LESSONS_NARRATIVE.md` for "but does it actually work in
  practice?" objections.
