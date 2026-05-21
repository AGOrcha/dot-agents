# Delegation: 0.3.0 public-facing docs update (v2 — corrected)

- task_id: docs-0.3.0
- branch/worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/rel-030`
  (branch `release/0.3.0` → PR #20) — INSIDE workspace root (v1 was
  `/tmp/wt-rel`, sandbox-blocked; relocated)
- owner: dot-agents
- status: re-delegated (v1 completed analysis, was write-blocked)

## Goal

Reconcile public-facing docs to the 0.3.0 reality so PR #20 ships
accurate user docs with the version bump.

## Write scope (ONLY these)

- `README.md`
- `docs/*.md` only if a public claim is wrong (v1 found none needed;
  CANONICAL_HOOKS_DESIGN / CONSTANTS_INVENTORY / PLATFORM_DIRS_DOCS are
  internal-design — leave unless you find an in-scope public contradiction)

## Out of scope (do NOT touch)

- `VERSION`, `CHANGELOG.md` (done), `src/**`, `.agents/**`, `internal/**`,
  `ports/**`, all Go code. No code fixes — flag, don't change.

## GROUND TRUTH (verified against `commands/root.go` on this branch)

- Binary self-identifies as **`da`** (`root.go:16 Use:"da"`, all examples
  `da`, version template `da version`). **No `dot-agents` vs `da`
  mismatch** — disregard the v1 bundle's claim of one; nothing to flag.
- Real top-level command surface (18, registration order):
  `init, add, remove, refresh, import, status, doctor, skills, agents,
  hooks, rules, mcp, settings, review, sync, explain, install, session`
- **There is NO `da workflow` command on 0.3.0** (workflow ships later
  via pr3b, not in this release). Do not document a workflow command.
  The only workflow-adjacent surface is `da review`
  (show/approve/reject pending workflow *proposals*).
- `da sync` = git ops on `~/.agents/` (init/commit/pull/push/status/log).
- Verify any flag/subcommand you state by reading the relevant
  `commands/<cmd>.go` (binary execution may be sandbox-denied; source is
  the source of truth — `go build ./...` works for a compile check).

## Required corrections (confirmed by v1 analysis)

1. **README "## Commands" section is pre-Go-rewrite** — missing
   `rules`, `mcp`, `settings` (`list` / `show` / `remove <scope> <name>`
   over canonical `~/.agents/` files), `review`
   (`show`/`approve`/`reject <id>`), `session` (`session stats`).
   Rewrite to the real 18-command surface with accurate Short strings.
2. **Broken links / nonexistent paths — remove or repair:**
   - `research/` directory does not exist (README links it ~4×:
     ~lines 109, 396, 407 + specific `research/*.md`).
   - `docs/TYPESCRIPT_PORT_BOUNDARY.md` does not exist (~line 152).
   - `ports/typescript/` does not exist in the tree at all — drop the
     TS-port install option (~126–130), the port subsection (~147–152),
     and the Requirements line (~283). (`scripts/install.sh --port ts`
     still exists but installs from a release tarball, not this tree —
     keep only if you phrase it as the script behavior, not a repo dir.)
3. **"Layer 2: Workflow Management (Coming)" / Roadmap** — partly shipped
   via `da review`; reconcile the framing to "config + workflow
   *proposals* shipped (`da review`); deeper workflow-state mgmt is
   roadmap" — match what the code actually does, no overclaiming.

No fabrication — every command/flag must exist in the branch's source.
Match existing README voice; keep prose tight.

## Verification (read-only)

- `cd <worktree> && go build ./... 2>&1 | tail -1` (compile sanity).
- `git status --porcelain` shows ONLY `README.md` (+ any `docs/*.md` you
  justified) before committing.

## Closeout

- Commit on `release/0.3.0`, `git push origin release/0.3.0` (updates
  PR #20). **Do NOT merge.**
- Final message: what changed, anything still flagged (incl. the
  global-rules `da workflow ...` references that don't exist in 0.3.0 —
  not a repo doc, flag only), and anything deliberately left.
