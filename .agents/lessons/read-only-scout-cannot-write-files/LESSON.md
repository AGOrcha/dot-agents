# Lesson: a read-only `scout` cannot write files — have it RETURN content

**Date:** 2026-07-21
**Surfaced by:** the 0.5.0 CHANGELOG synthesis — three scouts were briefed to
"write `local://changelog-slice-*.md`". The `scout` agent is **read-only (no
edit/write/command tools)**, so the files were never created; the full
per-PR classification lived only in each scout's transcript. The scouts that
put their content in the *returned report* (GapA/GapB/Late) were usable; the two
told only to "write the file" (Early/Mid) returned just a summary, and their
per-PR detail had to be reconstructed from `git log` branch names.

## Pattern
Briefing a read-only agent (`scout`, or any agent whose tool set excludes
`write`/`edit`) to persist output via `write`/`local://` silently drops that
output — the agent has no tool to comply, so the artifact never lands and only
a summary survives in the final report.

## Cause
Delegation briefs are authored against the *task*, not the *agent's actual
capabilities*. `scout` is explicitly read-only ("investigation and reporting").
"Write the result to `local://X`" is uncompliable for it.

## Rule
- When delegating to a **read-only** agent, require the deliverable **IN THE
  RETURNED REPORT** (the full content — tables, classifications), never as a
  `local://`/file write. Say so explicitly: "you are read-only; put the full
  content in your report."
- If the deliverable genuinely needs to be a file, delegate to a **writing**
  agent (`task`) or have the read-only agent return the content and the
  orchestrator writes it.
- Match every brief to the agent's tool set before sending: a brief that names
  a tool the agent lacks is a silent-failure trap.

## Regression check
Before dispatching a `scout` batch, grep the brief for `write`/`local://`/
"create the file" — if present, either switch to `task` or reword to "return in
your report".
