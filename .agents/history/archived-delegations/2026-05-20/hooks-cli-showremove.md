# Delegation: implement `da hooks show` + `da hooks remove` (close CLI gap, reconcile base remove)

- type: IMPLEMENTATION (CLI gap closure). Main-owned hooks domain.
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/hooks-cli`
  (branch `hooks-cli-showremove` off origin/master f38912ff)
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/hooks-cli`
  then `go version` (Bash restored; if genuinely denied STOP, don't loop).
- Reference: `.agents/proposals/hooks-wiring-and-opencode-fit.md` (audit:
  `da hooks` subtree currently has ONLY `list`; hook *removal* today is
  via the top-level `da remove`, not `da hooks remove`).

## ANALYZE FIRST (mandatory — do this before writing any code)

The maintainer's explicit constraint: "there is something in the base
remove — clean it out properly and analyze that path." So:
1. Read `commands/remove.go` (top-level `da remove`) and trace exactly
   how it currently removes a hook (the hook-removal code path,
   helpers, what it deletes, scope handling, dry-run).
2. Read the `commands/hooks/` subtree (cmd.go + siblings) — confirm the
   real current subcommand set (audit says `list`-only; verify) and how
   `list`/spec resolution works (reuse its hook-spec lookup).
3. Decide the consolidation: there must be ONE shared hook-removal
   helper that BOTH `da remove` (existing behavior preserved) AND the
   new `da hooks remove` call — NOT a parallel reimplementation. Extract
   the base-remove hook logic into that shared helper and have both
   entrypoints use it. Record the path you chose in the merge-back.

## IMPLEMENT

- `da hooks show <name>` — display a hook (bundle/spec: resolved path,
  kind, scope, contents/summary) reusing the existing hooks-spec
  resolver (the `hookSpecResolver`/findHookSpec seam from the cg6b-B1
  work if present). Read-only.
- `da hooks remove <name>` — remove a hook via the shared helper from
  the ANALYZE step (same effect as the relevant `da remove` path; honor
  dry-run, scope, the canonical `~/.agents/hooks/` model). No behavior
  change to `da remove`'s existing surface; it now just delegates to the
  shared helper.
- Wire both into the `hooks` cobra subtree alongside `list`. Match the
  package's existing command idiom (Deps/struct DI, ExampleBlock, arg
  validators) — do NOT introduce a new pattern or a package-global.

## Coverage / quality (standing policy)

- Behavior-asserting tests in source-mirroring files; the new code must
  be genuinely covered — NO new `coverage-exceptions.txt` entry, use
  the package's interface/seam pattern for any unreachable defensive
  branch. If a file you touch is already allowlisted, do NOT ride it for
  the new lines.
- 0 Sonar issues. `go build ./...`, `go test ./commands/... -count=1`,
  `go vet ./...`, `GOOS=windows go vet ./commands/hooks/`, `gofmt -l .`
  all clean.

## Scope

`commands/hooks/` (+ its tests) and `commands/remove.go` (refactor to
the shared helper only — preserve its external behavior). Nothing else.
Do NOT touch docs (HOOKS.md amend is a separate, later, sequenced pass
once this lands). Do NOT self-arm any background CI waiter — push +
report; the orchestrator polls CI.

## Closeout

Commit on `hooks-cli-showremove`, push, open a user-gated PR to master:
`feat(hooks): add `da hooks show`/`remove`, consolidate base-remove hook path`.
Body: the analyzed base-remove path + the shared helper you extracted,
the new subcommands, behavior-preservation of `da remove`, coverage,
verification. DO NOT merge. Final message: the consolidation shape,
surfaces added, any risk found in the old base-remove path.
