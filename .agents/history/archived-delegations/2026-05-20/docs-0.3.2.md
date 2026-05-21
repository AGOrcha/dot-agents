# Delegation: 0.3.2 release/public docs (pr3c — PR #18)

- task_id: docs-0.3.2
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/pr3c`
  (branch `pr3c-rebased` → pushes `pr3c/kg` → PR #18; stacked on pr3b)
- owner: dot-agents
- status: delegated

## Goal

0.3.2 ships the `da kg` surface (0.3.0/0.3.1 docs correctly omitted it).
pr3c is rebased on the updated pr3b, so README already has the
`da workflow` docs (from docs-0.3.1). Add `da kg`; reconcile the
inherited "da kg ships in 0.3.2+" hedge. Mirrors docs-0.3.0/0.3.1.

## Write scope (ONLY)

- `README.md`
- `internal/scaffold/home/starter/skills/global/agent-start/SKILL.md`
- `docs/*.md` only if a public claim is now wrong
- `CHANGELOG.md` — **verify only** the existing `[0.3.2]` entry vs the
  real surface; light edits ok; do not restructure or touch other
  versions (`[0.3.1]`/`[0.3.0]` are pr3b/master's, leave them)

## Out of scope (do NOT touch)

- `VERSION` (already 0.3.2), Go code, hooks, `commands/**`,
  `internal/**` except the one SKILL.md, `.agents/**`, `src/**`. Do NOT
  re-edit the `da workflow` README section (it came from docs-0.3.1 and
  is source-verified) — only ADD the `da kg` surface alongside it.

## GROUND TRUTH (verified on `pr3c-rebased`)

- Real top-level surface (20): the pr3b 19 + **`kg`**.
- `da kg` subcommands (from `commands/kg/cmd.go`): `setup`, `health`,
  `serve`, `queue`, `lint`, `maintain reweave|mark-stale|compact`,
  `bridge health|mapping|sync`, `warm`. **Verify exact subcommands,
  required flags, and Short strings against `commands/kg/cmd.go` and
  the kg command files on THIS branch** — no fabrication.
- `agent-start/SKILL.md:25` (inherited) currently says *"the `da kg`
  graph surface ships in 0.3.2+"* — on this branch it IS 0.3.2; reword
  so `da kg` reads as available in this release (workflow wording is
  already correct from docs-0.3.1 — leave it).
- README already has the `da workflow` "Workflow State" section (from
  docs-0.3.1). Add a parallel `da kg` section to `## Commands`; bump
  the top-level command count (it will say 19 → make 20).

## Required corrections

1. README `## Commands`: add the `da kg` surface (real subcommands,
   accurate Short strings), parallel in style to the Workflow section.
2. README: if any "knowledge graph … roadmap/coming" framing exists,
   reconcile to shipped for what `commands/kg` actually delivers (no
   overclaiming).
3. `agent-start/SKILL.md`: drop the `da kg` "0.3.2+" hedge — it ships
   here. Keep the workflow wording from docs-0.3.1 intact.
4. Verify CHANGELOG `[0.3.2]` matches the real kg surface; tighten if
   off. No fabrication; match README voice; tight prose.

## Verification (read-only)

- `cd <worktree> && go build ./... 2>&1 | tail -1` (if Bash-denied,
  note it; scope is docs-only so build is unaffected — state that).
- `git status --porcelain` shows only in-scope files.

## Closeout

- Commit on `pr3c-rebased`, `git push --force-with-lease origin
  pr3c-rebased:pr3c/kg` (updates PR #18). Do NOT merge.
- Final message: what changed, source-verification confirmation for the
  `da kg` claims, anything flagged/left.
