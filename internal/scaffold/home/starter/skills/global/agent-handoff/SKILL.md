---
name: "agent-handoff"
description: "Package current session context into a structured handoff document for another agent, your future self, or a coworker. Use at end of sessions, before task switches, or when context window is getting full. Supports modes: list, update, view."
argument-hint: "[list | update <slug> | view <slug> | <description>]"
---

# Agent Handoff

Package shaped context — plans, decisions, project state — into a structured handoff document. Supports four modes dispatched from arguments.

## Workflow

1. **Parse mode** — Read `instructions/modes.md` to determine which mode to run based on `$ARGUMENTS`.

2. **Auto-gather context** — Read `instructions/auto-gather.md` and silently collect context before doing anything. Do NOT display or summarize what you gathered.

3. **Run the mode** — Dispatch to the appropriate instruction file:
   - Create/default → `instructions/create-mode.md`
   - List → `instructions/list-mode.md`
   - Update → `instructions/update-mode.md`
   - View → `instructions/view-mode.md`

4. **Cleanup** (create mode only) — Before generating the handoff doc, reference `instructions/commit-cleanup.md` to ensure in-progress work is committed.

5. **Gotchas** — Read `instructions/gotchas.md` if anything seems unclear or output feels thin.
