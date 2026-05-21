# Delegation: 0.3.1 release/public docs (pr3b — PR #16)

- task_id: docs-0.3.1
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/pr3b`
  (branch `pr3b-rebased` → pushes `pr3b/workflow` → PR #16)
- owner: dot-agents
- status: delegated

## Goal

0.3.1 ships the `da workflow` surface that 0.3.0's docs *correctly
omitted*. Reconcile public docs + the #21 "0.3.1+" conditionals so PR
#16 ships accurate docs alongside its VERSION/CHANGELOG bump (mirrors the
docs-0.3.0 pass).

## Write scope (ONLY)

- `README.md`
- `internal/scaffold/home/starter/skills/global/agent-start/SKILL.md`
  (the #21 conditional wording — workflow now ships at 0.3.1)
- `docs/*.md` only if a public claim is now wrong
- `CHANGELOG.md` — **verify only** the existing `[0.3.1]` entry is
  accurate/complete vs the real surface; light edits ok, do not
  restructure or touch other versions

## Out of scope (do NOT touch)

- `VERSION` (already 0.3.1), other CHANGELOG versions, Go code,
  `commands/**`, `internal/**` except the one SKILL.md, `.agents/**`,
  `src/**`, hooks (the #21 availability-guards stay — they are correct
  defensively even with workflow shipped).

## GROUND TRUTH (verified on `origin/pr3b/workflow`)

- Real top-level surface (19): `init, add, remove, refresh, import,
  status, doctor, skills, agents, hooks, rules, mcp, settings, review,
  sync, explain, install, session, workflow`. **`da workflow` EXISTS
  here** (it did not in 0.3.0).
- Verify the `da workflow` subcommands/flags by reading
  `commands/workflow/*.go` on this branch (e.g. `cmd.go`, plan/task,
  orient, checkpoint, verify, fold-back). `go build ./...` for a compile
  check. No fabrication.
- README is still the 0.3.0-reconciled version (pr3b didn't touch it):
  - `### Layer 2: Workflow Proposals (Shipping)` with **Orient/Persist
    marked "Roadmap"** (~L105-106) — 0.3.1 ships `da workflow orient`
    and the workflow-state surface, so reconcile: what now ships vs
    what's still genuinely roadmap (don't overclaim — state only what
    `commands/workflow` actually provides).
  - `## Commands` (~L162) has **no `da workflow`** — ADD it with the
    real subcommand groups + accurate Short strings.
  - `## Roadmap` (~L394) — move now-shipped workflow items out of
    roadmap.
- `agent-start/SKILL.md:25` says *"workflow surface ships in 0.3.1+"* —
  on this branch it IS 0.3.1; reword so it reflects availability in this
  release (keep `da kg` still gated — kg ships in 0.3.2, not here).

## Required corrections

1. README `## Commands`: add the `da workflow` surface (real
   subcommands), grouped sensibly.
2. README Layer-2 + Roadmap: reconcile "Roadmap"→shipped only for what
   `commands/workflow` truly delivers; leave genuine roadmap items.
3. `agent-start/SKILL.md`: workflow now shipped (0.3.1) — drop the
   "ships in 0.3.1+" hedge for workflow; keep `da kg` conditional.
4. Confirm CHANGELOG `[0.3.1]` matches the real surface; tighten if off.
   No fabrication — every command/flag verified against branch source.
   Match README voice; tight prose.

## Verification (read-only)

- `cd <worktree> && go build ./... 2>&1 | tail -1`.
- `git status --porcelain` shows only in-scope files.

## Closeout

- Commit on `pr3b-rebased`, `git push --force-with-lease origin
  pr3b-rebased:pr3b/workflow` (updates PR #16). Do NOT merge.
- Final message: what changed, anything flagged/left, confirmation the
  `da workflow` doc claims were source-verified.
