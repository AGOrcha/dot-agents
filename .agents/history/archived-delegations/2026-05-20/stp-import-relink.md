# Delegation: stp-import-relink — projection in relinkImportedProjects

- plan/task: shared-target-projection-wiring / stp-import-relink
- worktree: .claude/worktrees/stp-import (branch stp-import off origin/master efb19756)
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/stp-import`

## Goal
`commands/import.go` `relinkImportedProjects` (def ~:1425, called ~:322) loops
platforms calling `p.CreateLinks(project, path)` (~:1435) WITHOUT first running
the shared-target projection — so a user-config import does not materialize repo
`.codex/agents/*.toml` / Claude shared-skills projection for relinked projects.

## Do
Mirror the ALREADY-CORRECT wiring on master (reference: refresh.go:157,
add.go:531, install.go:222 — `platform.RunSharedTargetProjection(name, path,
<installedEnabled>, <dryRun>)`). Call it once per relinked project BEFORE that
project's per-platform CreateLinks loop, with the same enabled/installed-platform
scoping the other 3 entries use. Match their error-handling shape. Existing
Deps/contract shape only — do NOT pre-empt di-refactor / graphstore-contract.

## Scope
Write scope: `commands/import.go` only (+ `commands/import_test.go` if needed for
coverage of the new branch — stp3 owns the full regression suite). Interface seam
for coverage, NO coverage-exceptions allowlist entry. No internal/platform change.

## Verify
go build ./... clean; go test ./commands/ -count=1 green; go vet ./... clean;
gofmt -l . empty.

## Closeout
Commit on stp-import, push, open PR to master `feat(stp-import-relink): run
shared-target projection on import relink`. Body: the gap, the fix mirroring the
3 existing entries, verification. DO NOT merge (user gates). Final message:
what changed + verification.
