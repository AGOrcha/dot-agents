# Delegation: docs-refresh — public-facing docs accuracy pass

- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/docs-refresh`
  (branch `docs-refresh` off latest master)
- status: delegated
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/docs-refresh`

## Goal

Bring the public/user-facing documentation into accuracy with the
CURRENT state of the project. This is a correctness pass, NOT a rewrite
and NOT new feature docs. Fix what is stale or wrong; do not invent.

## Ground truth to reconcile against

- Binary is **`da`** (cobra `Use:"da"`, module
  `github.com/NikashPrakash/dot-agents`). Any doc/install text saying the
  command/binary is `dot-agents` is WRONG — fix to `da` (keep the
  project/repo name "dot-agents" where it refers to the project itself).
- Current release is **v0.3.2** (`VERSION` file). Install path is the
  Homebrew tap + `dot-agents.rb` formula; verify version/sha references
  aren't pinned to a stale release in a way that misleads.
- The legacy `src/` bash implementation is **retired** (pr5). Remove or
  correct any docs that describe the shell implementation as current /
  tell users to run shell scripts under `src/`.
- The command surface is the Go CLI. Verify documented commands exist
  (`go run ./cmd/dot-agents --help` and subcommand `--help` are the
  authority). Correct removed/renamed commands; do not document
  unreleased/parked things (no TypeScript-port docs — pr6 is parked).

## Scope (strict)

Audit + correct ONLY public-facing docs:
- `README.md`
- `dot-agents.rb` (only if version/url/sha text is misleading — do NOT
  hand-edit a checksum; flag instead)
- `docs/*.md` that are user-facing (e.g. PLATFORM_DIRS_DOCS,
  CANONICAL_HOOKS_DESIGN if user-consumed) — SKIP internal ADRs
  (`docs/adr/**`), internal specs, `.agents/**`, research/.

Do not touch Go code, tests, CI, schemas, or `.agents/`.

## Method

1. `go run ./cmd/dot-agents --help` (+ relevant subcommand `--help`) to
   get the real command surface.
2. Grep the in-scope docs for stale signals: the literal `dot-agents`
   used as the *invoked command*, `src/` shell-run instructions, old
   version strings, removed commands, `da workflow`/`da kg` phrasing
   that misrepresents actual command names.
3. Make minimal, surgical corrections. Where something is ambiguous or
   needs a decision, FLAG it in the final report rather than guessing.

## Verify

`git status --porcelain` shows only in-scope doc files. No code/CI/.agents
changes. Re-read each edited section for internal consistency.

## Closeout

Commit on `docs-refresh`, push, open PR to master titled
`docs: accuracy pass (da binary, v0.3.2, legacy src/ retired)`. Body:
per-file what changed and why (the stale fact it corrected), plus any
flagged ambiguities. DO NOT merge (user gates). Final message: summary
of corrections + flags.
