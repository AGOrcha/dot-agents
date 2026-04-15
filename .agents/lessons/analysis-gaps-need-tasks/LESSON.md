---
name: analysis-gaps-need-tasks
description: When a session produces analysis results (comparison runs, gap findings, identified bugs), those gaps must become canonical tasks before the session closes — not prose notes.
type: lesson
---

# Analysis Gaps Need Tasks

## The Pattern

Sessions that produce analysis (A/B comparisons, architectural reviews, session debriefs) regularly identify gaps, bugs, or decisions. If those findings stay as prose in comparison docs or session notes, they don't get scheduled — and the next experiment runs against the broken baseline.

## What Happened

Three separate gaps were identified in prior sessions but never captured as tasks:

1. **loop-state split** — session `d694aff6` designed the `iter-N.yaml` split with full spec. It stayed as prose in the session transcript. The A/B experiment (session `c8ad19f1`) ran with the monolithic `loop-state.md` (33k tokens), causing 2.5M cache-read token bloat per script worker iteration. The experiment measured the wrong thing.

2. **fanout write_scope bug** — the A/B comparison notes explicitly flagged: *"Empty `write_scope` in bundle is a gap to address (fanout doesn't auto-pull from task definition)."* No task was created. The bug persisted until the next orchestrator session caught it.

3. **Pattern E → sub-agent conversion** — the session analysis concluded Pattern E should become a Claude Code sub-agent (AGENT.md). This stayed as recommendation prose. No task, no slice, no `phase-6`.

## The Rule

**After any session that produces a gap finding, bug identification, or architectural decision: create the canonical task before closing the session.** Prose notes are not tasks. A task in TASKS.yaml is.

Specifically:
- Comparison runs → task for each "gap to address" bullet
- Session debrief "what we'd do differently" → task or fold-back for each item
- Architectural decision "should convert X to Y" → task with write_scope, depends_on, notes

## Why Plans Can Also Have This Problem

Plans can underspecify, which leaves gaps that look like tasks but aren't complete:
- Marking a mechanism as "optional" when it enforces a contract → implementation skips it
- Leaving schema/migration/promotion steps implicit → implementation omits them
- Missing artifact types (schema files, migration scripts, sub-agent registrations) that are obvious in retrospect

**How to apply:** At plan-writing time, ask: "What artifacts does this produce? Does each have a task? Does each artifact need a schema file, migration path, or registration step?"

## When to Apply

- End of any comparison or A/B run: scan the results for "gap" / "to-do" / "mitigation" bullets
- End of any architectural analysis session: scan for "should", "would", "next step" language
- After writing a plan phase: ask what artifact types the phase produces and whether each has a task
- Before closing an orchestrator session: check if any analysis output contains untracked gaps
