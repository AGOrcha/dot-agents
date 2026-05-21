# Delegation: hook-wiring verify (Codex/Copilot) + OpenCode-plugin fit analysis

- type: READ-ONLY audit/analysis. Make NO code or doc changes. Output is
  ONE proposal markdown only. Do not fix; correction is a separate
  follow-up the maintainer will direct.
- workspace: main checkout `/Users/nikashp/Documents/dot-agents`
  (origin/master is authoritative — `git fetch origin` first; do NOT
  reason from a stale local master ref).
- prior art (build on, do not redo): `.agents/proposals/codex-hooks-agents-linking-gap.md`
  (its Finding 1 already established Codex hooks ARE rendered+written via
  codex.go + CreateLinks). Re-confirm against CURRENT origin/master.
- status: delegated
- First command: `cd /Users/nikashp/Documents/dot-agents && git fetch origin`

## Context

PR #33 (open, branch docs-refresh) adds `docs/HOOKS.md`. The maintainer,
reading it, saw it claims **Codex hooks not wired** and **Copilot
partial**. Finding 1 of the prior proposal + the (#29-merged)
PLATFORM_DIRS_DOCS Hook Wiring Audit say Codex hooks ARE wired ("Yes").
So HOOKS.md likely carried a stale claim. Verify, don't assume.

## Deliverable 1 — Hook-wiring accuracy (Codex + Copilot)

Trace, on origin/master, the real per-platform hook wiring in
`internal/platform/` (codex.go, copilot.go, hooks.go, the CreateLinks
path) and the command invocation (`commands/` refresh/install/add):

- **Codex**: does it render AND write repo `.codex/hooks.json` + user
  `~/.codex/hooks.json` via the production path? Cite file:line.
  State definitively: wired = YES or NO.
- **Copilot**: exactly what is wired (`.github/hooks/*.json`?
  Claude-compatible settings?) and what, if anything, is genuinely
  missing that makes it "partial" vs Codex/Cursor "Yes". Is "partial"
  accurate, stale, or wrong? Cite file:line.
- Diff those facts against the claims in BOTH `docs/HOOKS.md` (on the
  `docs-refresh` branch: `git show origin/docs-refresh:docs/HOOKS.md`)
  AND the merged `docs/PLATFORM_DIRS_DOCS.md` Hook Wiring Audit on
  origin/master. For every inaccurate line, give the EXACT corrected
  wording (propose text; do not edit files). State a #33 verdict:
  HOOKS.md hook table is accurate / must be amended (with the text) /
  must be reverted.

## Deliverable 2 — OpenCode-plugin-as-hook FIT analysis

OpenCode has no separate hooks file; its plugin system is functionally
hook-like but architecturally different (code modules vs declarative
command/event JSON). Analyze whether adding OpenCode to the canonical
hook path (emitting to opencode plugin-scoped dirs) is a GOOD FIT:

- What is OpenCode's plugin contract (load mechanism, file
  shape/location — `.opencode/plugin*`? `~/.config/opencode/`?, event
  surface)? Use the OpenCode docs already cited in PLATFORM_DIRS_DOCS +
  any opencode handling already in `internal/platform/opencode.go`.
- Map the canonical `~/.agents/hooks/<scope>/<name>/HOOK.yaml` +
  command/event model onto OpenCode plugins. Where is the impedance:
  do we render config, or must we generate a code/JS plugin shim?
  Event-name mapping coverage? Per-hook vs single-plugin aggregation?
- Verdict: GOOD FIT (worth a plan — sketch the emitter shape),
  POOR FIT (say why; recommend not wiring + the alternative), or
  CONDITIONAL (what would have to be true). No implementation.

## Output

Write `.agents/proposals/hooks-wiring-and-opencode-fit.md`: problem,
Deliverable 1 (per-platform ground truth + exact #33/PLATFORM_DIRS
corrections + verdict), Deliverable 2 (OpenCode fit analysis +
recommendation + whether it warrants a spec/plan), and a short "what the
maintainer must decide" list. No other file touched. Final message:
the Codex/Copilot verdicts (1 line each), the #33 recommendation, and
the OpenCode fit verdict.

## Deliverable 3 — HOOKS.md management-lifecycle completeness

Maintainer feedback: docs/HOOKS.md (on origin/docs-refresh, PR #33)
"starts well with add and refresh but is incomplete" — it must explain
the FULL hook management lifecycle, not just creation/refresh.

- Enumerate the real lifecycle from `go run ./cmd/dot-agents hooks
  --help` + each subcommand --help AND the commands that touch hooks
  (`add`, `refresh`, `install`, `import`, `sync`, `doctor`, `remove`,
  `hooks list/show/remove`): create/add → refresh/relink →
  list/inspect → import (canonicalize legacy) → sync (push/pull
  ~/.agents) → repair (doctor) → remove. Map each to its real command
  surface (no invented subcommands — verify the `hooks` subtree
  exactly).
- Diff that against what `docs/HOOKS.md` currently covers; list the
  EXACT missing lifecycle stages and propose the precise added prose
  (do not edit the file — propose text in the proposal).
- Verdict folds into the #33 recommendation (amend HOOKS.md with the
  lifecycle section + the Deliverable-1 wiring corrections).
