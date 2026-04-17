# Handoff: Loop Runtime Refactor — Skill Chain + Ralph Scripts + Subagent Worker

**Created:** 2026-04-14
**Author:** Claude Code session 31916bf2 (analysis) + subagent-consideration branch
**For:** AI Agent (interactive Claude Code session)
**Status:** Ready to execute — plan committed, `workflow next` wired to first task

---

## Summary

This is an interactive implementation session for the `loop-runtime-refactor` plan. The plan
fixes the orchestration loop's skill chain (load-order bugs, duplicated closeout steps, oracle
sprawl), splits `active.loop.md` back to its intended worker-only scope, creates four role-pure
ralph scripts (orchestrate/worker/closeout/pipeline), and adds a cold-start `loop-worker` skill
for Pattern E subagent testing. All planning is done and committed — your job is to start
executing phase-1a and work through the phases interactively.

## Project Context

`dot-agents` is a Go CLI (`cmd/dot-agents/`) that manages AI agent workflows across repos.
It has a `workflow` command family (orient, next, tasks, fanout, checkpoint, etc.) and a
skill/plan system under `.agents/`. The repo dogfoods itself: the orchestration loop that
drives development of `dot-agents` is what this plan is improving.

Key directories:
- `.agents/skills/` — repo-local skills (orchestrator-session-start, iteration-close, etc.)
- `~/.claude/skills/` — user-scope global skills (agent-start, skill-architect, etc.)
- `.agents/active/` — loop-state.md, active overlays, delegation artifacts
- `.agents/workflow/plans/` — canonical PLAN.yaml + TASKS.yaml per initiative
- `bin/tests/` — ralph scripts (ralph-cursor-loop currently, new scripts go here)
- `commands/workflow.go` — all `workflow *` CLI implementations (do NOT touch in this plan)

**Pre-existing build break:** `internal/graphstore/postgres.go` imports `pgx/v5` which is
absent from `go.mod`. `go test ./...` fails. Fix with `go get github.com/jackc/pgx/v5
github.com/jackc/pgx/v5/pgxpool` if you need full suite — but this plan has no Go changes,
so TypeScript and skill/script edits are unaffected.

## The Plan

Full plan: `.agents/workflow/plans/loop-runtime-refactor/loop-runtime-refactor.plan.md`
TASKS: `.agents/workflow/plans/loop-runtime-refactor/TASKS.yaml`

**Phase 1 — Skill chain fixes** (5 tasks, sequential, all `.agents/skills/` or `active.loop.md`):
- 1a: Fix `orchestrator-session-start` load order — gotchas.md before workflow.md
- 1b: Strip steps 19–30 from `active.loop.md` → single `/iteration-close` call
- 1c: Add `agent-start` suppression note + remove `workflow status` oracle from `active.loop.md`
- 1d: Add reconciliation iteration type to `active.loop.md`
- 1e: Add tool-bug auto-escalation to `iteration-close/instructions/workflow.md`

**Phase 2 — Overlay split** (2 tasks):
- 2a: Refactor `active.loop.md` to worker-only scope (~100 lines)
- 2b: Create `orchestrator.loop.md` with extracted orchestrator content

**Phase 3 — Ralph scripts** (4 tasks, `bin/tests/`):
- 3a: `ralph-orchestrate.sh` — orchestrator-only, multi-task discovery, emits RALPH_BUNDLE lines
- 3b: `ralph-cursor-loop.sh` rework — worker-only, `--bundle` required
- 3c: `ralph-closeout.sh` — scans merge-backs, runs advance + delegation closeout
- 3d: `ralph-pipeline.sh` — full E2E wrapper

**Phase 4 — Subagent worker** (3 tasks):
- 4a: Create `loop-worker` skill (`.agents/skills/loop-worker/`)
- 4b: Add Pattern E docs to `orchestrator-session-start/instructions/workflow.md`
- 4c: Add metrics.json capture to pipeline + worker scripts

## Key Files

| File | Why It Matters |
|------|----------------|
| `.agents/workflow/plans/loop-runtime-refactor/TASKS.yaml` | Canonical task list — read this first with `workflow tasks loop-runtime-refactor` |
| `.agents/workflow/plans/loop-runtime-refactor/loop-runtime-refactor.plan.md` | Full narrative with implementation notes per phase |
| `.agents/skills/orchestrator-session-start/SKILL.md` | Phase 1a target — reorder 3 lines |
| `.agents/skills/orchestrator-session-start/instructions/workflow.md` | Phase 4b target |
| `.agents/skills/iteration-close/instructions/workflow.md` | Phase 1e target |
| `.agents/active/active.loop.md` | Phases 1b, 1c, 1d, 2a targets |
| `~/.agents/profiles/loop-worker.md` | Read-only reference — the three-layer model it defines is the architectural basis for Phase 2 |
| `bin/tests/ralph-cursor-loop` | Phase 3b base — read before reworking |

## Current State

**Done:**
- Full analysis of session 31916bf2 pain points (13 issues catalogued)
- Plan created and committed: `00766b3`
- `workflow next` wired to phase-1a via correct `current_focus_task` title
- `loop-state.md ## Next Iteration Playbook` updated to focus on this plan
- Pre-existing `[tool-bug]` documented: pgx dependency missing from go.mod

**In Progress:**
- Nothing — all phases are `pending`, ready to start from phase-1a

**Not Started:**
- All 13 tasks (phases 1a through 4c)

## Decisions Made

- **No CLI changes in this plan** — all work is skills, overlays, and shell scripts only.
  `commands/workflow.go` is out of scope entirely.

- **Use `/skill-architect` for all `.agents/skills/` edits** — do not directly edit skill
  files. Mode: `improve` for existing skills (1a, 1e, 4b), `new` for the loop-worker skill (4a).

- **`active.loop.md` is restored as worker overlay, not replaced** — `~/.agents/profiles/loop-worker.md`
  documents a three-layer model where the project overlay (Layer 2) was always meant to be
  worker-facing. Orchestrator content crept in. Phase 2 extracts it to `orchestrator.loop.md`
  rather than creating a new parallel `worker.loop.md` file.

- **`loop-worker` skill is a thin wrapper** — `~/.agents/profiles/loop-worker.md` already
  has the worker discipline and closeout sequence. Phase 4a loads the profile, does not
  duplicate it.

- **`agent-start` (global skill) is not modified** — add a project-level suppression note
  to `active.loop.md` instead. The global skill is used in other repos.

- **`workflow status` removed from startup oracle chain** — it is always stale (6+
  consecutive iterations documented as baseline). Not a bug fix, just removing dead weight.

- **Reconciliation iterations get their own type** — iterations that are state catch-up only
  should not be forced to invent a feedback goal. `cli_produced_actionable_feedback: n/a`
  is a valid self-assessment value.

## Important Context

- **`current_focus_task` in PLAN.yaml must be the exact task *title* string, not the task ID.**
  `workflow next` matches on `task.Title` (line 2306 of `commands/workflow.go`). Setting it to
  the ID silently drops the plan to priority 3, losing to any other plan with a correctly set
  focus title. Use `workflow plan update <plan-id> --focus "<exact title>"` to fix.

- **`orchestrator-session-start` currently loads gotchas AFTER the fanout decision** — this is
  the root cause of session 31916bf2's orchestrator-becoming-worker pattern. Phase 1a fixes
  the load order in SKILL.md (3-line change). The gotchas.md already has the right rule; it
  just fires too late.

- **`delegation-lifecycle` skill is a thin stub** — it wraps 3 CLI commands already documented
  in `orchestrator-session-start/instructions/workflow.md`. It's not being changed in this plan
  but don't invest time trying to understand it deeply.

- **The ralph scripts are in `bin/tests/` not `scripts/`** — `scripts/` has install/verify
  scripts. Ralph lives at `bin/tests/ralph-cursor-loop` (no `.sh` extension currently).
  New scripts should follow the same pattern: no extension, `#!/usr/bin/env bash` shebang.

- **Phase 3 scripts have no tests** — shell scripts in `bin/tests/` are not tested by
  `go test ./...`. Verification is manual smoke tests documented in each task's notes.
  Don't spend time writing bats/shellspec tests unless explicitly asked.

- **This is an interactive session, not a `/loop` run** — you are not running as a headless
  worker. You can ask questions, discuss tradeoffs, and show progress. Pick one task at a time
  and complete it before moving on.

## Next Steps

1. **Orient** — Run `go run ./cmd/dot-agents workflow tasks loop-runtime-refactor` to read the
   current task list and confirm phase-1a is the first pending task.

2. **Phase 1a** — Run `/skill-architect` with mode `improve` on
   `.agents/skills/orchestrator-session-start/SKILL.md`. Change: reorder steps so
   `instructions/gotchas.md` loads at step 1, `instructions/workflow.md` at step 2. Also
   update the description to include "Use in repos with `.agents/workflow/` and `active.loop.md`
   present." See TASKS.yaml `phase-1a` notes for full detail.

3. **Phase 1b** — Edit `.agents/active/active.loop.md` to replace steps 19–30 (12 sub-steps)
   with a single step pointing to `/iteration-close`. See TASKS.yaml `phase-1b` notes.

4. **Continue phases 1c → 1d → 1e in order** before moving to Phase 2.

5. **Phase 2 requires reading `~/.agents/profiles/loop-worker.md`** before touching
   `active.loop.md` — the three-layer model it describes is the architectural guide for
   what stays vs what moves to `orchestrator.loop.md`.

## Constraints

- **No changes to `commands/workflow.go` or any Go source files.** This plan is skills,
  overlays, and shell scripts only.
- **Use `/skill-architect` for all `.agents/skills/` edits.** Direct file edits to skill
  files are not acceptable.
- **One task per iteration.** Complete, verify, and close each task before starting the next.
- **Do not modify `~/.agents/profiles/loop-worker.md`** — it is the global reference document,
  not a task target.
- **Do not modify `~/.claude/skills/agent-start/`** — global skill, used in other projects.
  Add a suppression note to `active.loop.md` instead (phase-1c).
- **`active.loop.md` edits in phases 1b/1c/1d must be complete before phase 2a** — phase 2a
  strips the file further; don't accidentally overwrite 1b/1c/1d changes.
