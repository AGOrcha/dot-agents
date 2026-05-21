# Delegation: fix misleading `da workflow` references in 0.3.0

- task_id: fix-workflow-refs-030
- branch/worktree:
  `/Users/nikashp/Documents/dot-agents/.claude/worktrees/wf-refs-030`
  (branch `fix/workflow-refs-030`, branched from `release/0.3.0`)
- target: **sub-PR into `release/0.3.0`** (NOT master, NOT PR #20 directly)
- owner: dot-agents
- status: delegated

## The mistake (why this is bad)

`da workflow …` ships in **pr3b (0.3.1)**, NOT in 0.3.0. Yet 0.3.0
ships content that **instructs, executes, or names** `da workflow …` as
if it exists: shipped hooks that run it, a starter skill that tells
agents to run it, and **real CLI messages that tell users to run it**.
On a clean 0.3.0 install these are broken/misleading guidance — an agent
or user following them hits "unknown command", and a hook prints
`da workflow checkpoint failed` when the command never existed.

## Guiding principle (apply per occurrence)

0.3.0 must never instruct/execute/promise a command it does not ship.
**Do NOT delete the workflow functionality or its forward wiring** —
pr3b re-enables it at 0.3.1. Prefer, in order:
1. **Graceful guard** — detect availability
   (`da workflow --help >/dev/null 2>&1` / `command -v`) and only
   run/suggest when present; otherwise silent shell-fallback (no
   cry-wolf "failed" message when the command simply isn't installed).
2. **Correct the message** — a user-facing hint must not name a
   nonexistent command; point to what 0.3.0 actually offers, or drop
   the sentence.
3. **Version-note** only where a guard/correction doesn't fit.

Forward-compatible: the change must not conflict semantically with
pr3b's workflow surface (pr3b will rebase on 0.3.x).

## Scope — classify & fix each occurrence

Verified occurrences on this branch (re-grep to be exhaustive:
`git grep -n -E 'da workflow |dot-agents workflow '` minus README/docs):

1. **Real CLI messages (user-facing) — fix:**
   - `commands/ux.go:250` — hint string `"Run \`da workflow prefs\` …"`.
     Determine if this path is even reachable on 0.3.0 (no workflow
     cmd). If reachable, the hint must not name `da workflow`; if
     unreachable, still correct/remove it (it ships in the binary).
   - Any other Go error/hint string naming `da workflow` or a wrong
     binary.
   1a. **Binary-name in CLI messages — `dot-agents` → `da` (FIRST-CLASS
       scope, per user):** The binary is `da` (`root.go Use:"da"`). ANY
       user-facing CLI string that names the *invoked binary* as
       `dot-agents` is wrong — examples, `Example:` blocks, error/hint
       text, usage/help strings, cobra `Use`/templates,
       `SetVersionTemplate`. Grep repo-wide in emitted strings:
       `git grep -nE '"[^"]*dot-agents (init|add|remove|refresh|import|status|doctor|skills|agents|hooks|rules|mcp|settings|review|sync|explain|install|session|version|--)' -- 'commands/**.go' 'internal/**.go'`
       and cobra `Example:`/`Use:`/`SetUsageTemplate`/`SetVersionTemplate`.
       Change the binary token to `da`.
       **Do NOT change legitimate `dot-agents`:** the Go module path
       `github.com/NikashPrakash/dot-agents`, import paths, the
       `cmd/dot-agents` directory, clone URLs, or the project *name* in
       prose ("dot-agents manages…"). Only the **invoked-binary token**
       in user-facing output. Verify each by reading context; when
       unsure, flag rather than guess.
2. **Shipped hooks (runtime) — guard:**
   - `internal/scaffold/hooks/global/session-orient/orient.sh` (~183/187)
   - `internal/scaffold/hooks/global/session-capture/capture.sh` (~93/97)
     They already shell-fallback on failure but emit a misleading
     `… failed` warning. Add an availability guard so on 0.3.0 they
     skip silently (or say "workflow unavailable", not "failed") and
     still work unchanged once 0.3.1 provides the command.
3. **Shipped starter skill — correct:**
   - `internal/scaffold/home/starter/skills/global/agent-start/SKILL.md:25`
     — "optionally run `da workflow orient`…": make it conditional /
     note it requires the workflow surface (0.3.1+), don't hard-instruct.
4. **Code comments** (`internal/platform/platform.go:9`) — not
   user-facing; leave or lightly note; not a priority.
5. **schemas/*.json**, **.github/workflows/test.yml** — assess: schema
   descriptions and CI are not user-facing CLI guidance. Likely
   **flag-only, do not edit** unless CI on this branch would actually
   invoke a missing command (check test.yml's da workflow step vs the
   0.3.0 binary; if it breaks 0.3.0 CI, that's a real issue — report,
   propose, but confirm before editing CI).

## Out of scope (do NOT touch)

- `README.md`, `docs/*.md` — owned by the parallel `docs-0.3.0` worker.
- `VERSION`, `CHANGELOG.md`, `.agents/**`, `src/**`, `ports/**`.
- The global `~/.claude/CLAUDE.md` / `~/.agents/rules/**` `da workflow`
  references — NOT repo-tracked; **flag in the report** (they route to a
  `~/.agents/proposals/` change, separate), do not edit.
- Do not delete/disable the workflow forward-wiring.

## Verification (read-only)

- `cd <worktree> && go build ./... 2>&1 | tail -1` (compile).
- `gofmt -l` on any touched `.go`; `bash -n` on any touched `.sh`.
- `go test ./commands/ ./internal/platform/ -count=1` if Go touched
  (relocation/string edits are behavior-light; confirm green).
- `git status --porcelain` — only the in-scope files.

## Closeout

- Commit on `fix/workflow-refs-030`, `git push -u origin
  fix/workflow-refs-030`. Do NOT open the PR or merge — the orchestrator
  opens the sub-PR (base `release/0.3.0`) and gates merge.
- Final message: per-occurrence table (file:line → classification →
  fix applied / flagged-only + why), the global-rules flag, compile/test
  results, anything deliberately left.
