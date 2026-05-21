# Delegation: pr7 — test infra, globalflagcov, docs, ADRs, research

- plan/task: pr10-branch-split / pr7-test-infra-docs
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/pr7`
  (branch `pr7-test-infra-docs` off latest master)
- source of truth (IN-workspace, readable): the mega-branch worktree
  `/Users/nikashp/Documents/dot-agents/.agents/worktrees/proj-mega-branch`
  (branch feature/PA-cursor-projectsync-phase1-extract-293f)
- status: delegated
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/pr7`

## Goal

Port the test-infrastructure / tooling / docs slice from the mega-branch
verbatim into this PR. Additive only — no behavior change to existing
packages.

## Scope (strict — these paths ONLY)

Copy from `<mega>/` into the pr7 worktree, preserving structure:
- `internal/testutil/`            (2 files)
- `internal/globalflagcov/`       (3 files)
- `cmd/globalflag-coverage/`      (1 file)
- `docs/adr/`                     (9 ADRs)
- `docs/` other ported specs      (the ~15 *.md that are NOT pr6's
  TYPESCRIPT_PORT_* / typescript-port-boundary.json — pr6 is parked;
  DO NOT bring any ports/typescript or TYPESCRIPT_PORT_* files)
- `research/`                     (40 files)
- `tests/`                        (15 files — smoke + sandbox)
- `bin/tests/`                    (5 files — ralph orchestrator)

Touch nothing outside this list. No edits to existing Go packages.

## Coverage gate (NOW LIVE — per-file enforce)

`internal/testutil/` is gate-excluded (no action). `internal/globalflagcov/`
and `cmd/globalflag-coverage/` are NOT excluded — each new .go file must
be >=95% Go statement coverage on the local profile, OR added to
`scripts/coverage-exceptions.txt` with a genuine one-line rationale
(NOT aspirational). Bring real behavior-asserting tests
(source-mirroring filenames) if the ported files lack them.

## Verify

`cd` the worktree: `go build ./...` clean; `go test ./... -count=1`
green (or at minimum the new/affected packages; run the full suite if
feasible); `go vet ./...` clean; `gofmt -l .` empty.
Local gate: `COVERAGE_FILE=<profile> COVERAGE_PKG_MODE=off
COVERAGE_FILE_MODE=enforce bash scripts/coverage-gate.sh` PASS (single-OS
local is advisory; CI merged profile is authoritative).

## Closeout

Commit on `pr7-test-infra-docs`, push, open PR to master titled
`feat(pr7): test infra, globalflagcov, docs, ADRs, research`.
Body: file counts ported per dir, coverage status of the 2 non-excluded
Go packages (>=95% or allowlisted+rationale), verification run. DO NOT
merge (user gates). Final message: what was ported, gate status, any
issue.
