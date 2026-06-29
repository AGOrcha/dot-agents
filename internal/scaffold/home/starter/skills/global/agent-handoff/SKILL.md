---
name: "agent-handoff"
description: "Package current session context into a structured handoff document, and recover verified state on resume via the session-handoff journal. Use at end of sessions, before task switches, when context window is getting full, or when resuming after a compaction/crash. Supports modes: recover, list, update, view."
argument-hint: "[recover | list | update <slug> | view <slug> | <description>]"
---

# Agent Handoff

Package shaped context — plans, decisions, project state — into a structured handoff document, and **recover state across sessions through the session-handoff journal** — a crash-survivable, append-only event log that re-verifies its claims against reality before you trust them. Supports five modes dispatched from arguments.

## Workflow

1. **Parse mode** — Read `instructions/modes.md` to determine which mode to run based on `$ARGUMENTS`.

2. **Auto-gather context** — Read `instructions/auto-gather.md` and silently collect context before doing anything. Do NOT display or summarize what you gathered. This includes the **verified recovery view** (`da workflow journal recover`): durable state re-verified against reality, never a stale prose claim.

3. **Run the mode** — Dispatch to the appropriate instruction file:
   - Create/default → `instructions/create-mode.md`
   - Recover (resume after a compaction/crash) → `instructions/verified-readback.md`
   - List → `instructions/list-mode.md`
   - Update → `instructions/update-mode.md`
   - View → `instructions/view-mode.md`

4. **Cleanup** (create mode only) — Before generating the handoff doc, reference `instructions/commit-cleanup.md` to ensure in-progress work is committed.

5. **Capture cadence** — Read `instructions/journal-cadence.md` for *when* to capture state so a future session can recover it. The deterministic layer is automatic (the journaled `da workflow`/`kg`/`review` mutators append an event; recomputable surfaces like config `refresh` are excluded); the reasoned *why* is your job, at the adaptive cadence described there. This is the write side that makes recover (step 3) work.

6. **Gotchas** — Read `instructions/gotchas.md` if anything seems unclear or output feels thin.
