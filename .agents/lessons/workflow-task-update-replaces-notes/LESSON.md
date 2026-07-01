# Lesson: `da workflow task update --notes` REPLACES the notes field (does not append)

## Pattern

`da workflow task update <plan> --task <id> --notes "<text>"` sets the task's
`notes` to exactly `<text>` — a full **replace**, not an append or merge. Passing
a short status line silently discards whatever rich content the field already
held (delegation contracts, hard-test criteria, ISP routing blocks, prior
amendments, escalation history).

## Root cause

`runWorkflowTaskUpdate(planID, taskID, title, notes, writeScope)` assigns
`task.Notes = notes` when `notes != ""`. There is no read-modify-write; the CLI
has no `--append-notes` mode. The write succeeds and prints `✓ Updated task`, so
the loss is invisible unless you inspect the diff.

## What went wrong (2026-06-30)

Dispositioning `pr6-typescript-port`, `pr5-followup`, and
`sq5-archive-on-pr-merge`, a single `--notes "<disposition line>"` per task
clobbered each task's detailed original notes (the PARKED carve-out record, the
swallowed-TODO recovery list, the T1 archival contract + hard test). Caught only
by reading `git diff --cached` before commit — the stat showed ~30 deletions for
what should have been a one-line note change. All three were reconstructed to
preserve the originals with a dated disposition prefix prepended.

## Rule

Before `--notes` on a task that may already carry notes:
1. Read the current notes first (`git show origin/master:<TASKS.yaml>` or grep the
   task block).
2. If they hold anything worth keeping, compose the FULL replacement = original
   body + your addition (prefer prepending a dated `DISPOSITION`/`STATUS` line so
   the history stays legible), and pass that whole string.
3. For multi-line notes, edit the `TASKS.yaml` `notes:` block directly with a
   block scalar (`notes: |-`) rather than a single-line `--notes` — see
   `schema-usage` colon-space rule. Always `git diff` before committing and treat
   a deletion-heavy stat on a "note tweak" as a red flag.

## How to apply

The behavior is fine-by-design (it's an *update*), just sharp-edged — so this is a
"be aware" lesson, not a bug. If the friction recurs often, the clean fix is an
additive `--append-notes` flag on `task update` (read-modify-write); until then,
treat `--notes` as destructive and reconstruct. Related: [[analysis-gaps-need-tasks]]
(notes are not schedulable work — don't overload a task's notes as a backlog).
