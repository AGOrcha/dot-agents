# Delegation: pr5 — scaffold re-home + legacy shell retirement

- plan/task: pr10-branch-split / pr5-scaffold-rehome (CRITICAL PATH)
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/pr5`
  (branch `pr5-scaffold-rehome` off latest master)
- source of truth (IN-workspace, readable): the mega-branch worktree
  `/Users/nikashp/Documents/dot-agents/.agents/worktrees/proj-mega-branch`
  (branch feature/PA-cursor-projectsync-phase1-extract-293f). This branch
  is the intended END-STATE: scaffold dirs already reconciled-to-latest,
  `src/` already retired (0 files), prompts present. Port FROM it.
- status: delegated
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/pr5`

## Goal

Bring master to the mega-branch end-state for scaffold + legacy shell:
embed starter assets in the Go binary, recover the load-bearing agent
prompts into a tracked home, and delete the dead `src/` shell tree.

## Tasks (do in this order)

1. **Recover the 3 untracked prompts.** From
   `<mega>/.agents/prompts/{isp.prompt.md,impl-agent.project.md,review-agent.project.md}`
   copy into BOTH:
   - the repo `.agents/prompts/` (tracked home), AND
   - the scaffold starter prompts dir under `internal/scaffold/home/`
     (mirror the mega-branch's layout — find where mega places them in
     the starter tree and match exactly) so new projects receive them.
   These are load-bearing for the ISP staged runtime (impl -> verifier
   -> review); without this the embedded binary references missing
   prompts.
2. **Port scaffold asset dirs verbatim** from mega-branch:
   `internal/scaffold/home/` (19), `internal/scaffold/hooks/` (16),
   `internal/scaffold/templates/` (4) — including the `embed.go`
   wiring. Match mega-branch exactly (it is the reconciled canonical).
3. **Delete the legacy `src/` tree** — all 41 files on master
   (`git rm -r src/`). Pre-verified: no Go references `src/`. Confirm
   again (`grep -rn 'src/' --include='*.go' .` excluding tests/comments)
   before deleting.
4. **Fold the noted SonarQube items** present in the scaffold Go code:
   S8184 (blank-import needs an explanatory comment), S3776
   (extract `copyStarterEntry` to reduce cognitive complexity). Apply
   only if these constructs are present in the ported code.
5. **6 starter-skill canonical cross-check.** Mega-branch's
   `internal/scaffold/home/starter/skills` IS the canonical snapshot —
   port it as-is. List the starter skills you ported with a one-line
   content summary each in your final message; FLAG any you are unsure
   represents latest canonical (do NOT try to read `~/.agents` — it is
   out of your sandbox; main agent resolves flagged ones inline).

## Scope

Write scope: `internal/scaffold/{home,hooks,templates}/`,
`.agents/prompts/`, `src/` (deletion). Plus
`scripts/coverage-exceptions.txt` only if a non-excluded new .go file
genuinely needs an allowlist entry. Nothing else.

## Coverage gate (NOW LIVE — per-file enforce)

`internal/scaffold/{home,hooks,templates}/` embed copy-helpers are
gate-EXCLUDED. But any new/modified .go OUTSIDE those excluded subdirs
(e.g. an `internal/scaffold/*.go` at the package root, or copyStarterEntry
if it lands non-excluded) must be >=95% or rationale-allowlisted. Add
behavior-asserting source-mirroring tests as needed.

## Verify

`go build ./...` clean (embed must compile with the recovered prompts
present); `go test ./... -count=1` green; `go vet ./...` clean;
`gofmt -l .` empty; local coverage gate
(`COVERAGE_PKG_MODE=off COVERAGE_FILE_MODE=enforce`) PASS. Sanity: the
binary's scaffold/init path references prompts that now exist in the
embedded tree (no dangling prompt refs).

## Closeout

Commit on `pr5-scaffold-rehome`, push, open PR to master titled
`feat(pr5): scaffold re-home + legacy src/ retirement`. Body: prompts
recovered (paths, both destinations), scaffold dirs ported (counts),
src/ files deleted (count), Sonar items folded, starter-skill list +
any flags, gate status, verification. DO NOT merge (user gates).
Final message: same summary + anything flagged for inline resolution.
