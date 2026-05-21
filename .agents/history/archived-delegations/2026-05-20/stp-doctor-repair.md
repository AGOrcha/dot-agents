# Delegation: stp-doctor-repair — doctor repair owns projection + full managed-entity link

- plan/task: shared-target-projection-wiring / stp-doctor-repair
- worktree: .claude/worktrees/stp-doctor (branch stp-doctor off origin/master efb19756)
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/stp-doctor`

## Decision (locked by user)
doctor's repair path must NOT stay symlink-only. For each MANAGED project doctor
repairs, it must:
  (a) run `platform.RunSharedTargetProjection(project, path, <installed>, Flags.DryRun)`
      to fix broken/missing projected shared-target artifacts (repo
      `.codex/agents/*.toml`, Claude shared-skills projection); AND
  (b) ensure ALL managed da entities are linked — full `p.CreateLinks(project, path)`
      across installed platforms — not merely re-running CreateLinks for links it
      already detected as broken.

## Context
`commands/doctor.go` (~:180-199) currently re-runs CreateLinks only for
already-broken links (targeted symlink repair) and never runs the projection.
Reference the established, correct call shape on master: refresh.go:157,
add.go:531, install.go:222, and import.go relink (PR #31). add/install/import use
installed-only scoping; match the scope to the CreateLinks loop you drive.

## Do
1. Study doctor.go's repair model (how it enumerates managed projects + the
   current broken-link repair block). Determine the right place to, per managed
   project, run the projection THEN a full installed-platform CreateLinks pass.
2. Implement (a)+(b) mirroring the established shape + warn-and-continue error
   handling. MUST stay idempotent: a healthy managed project produces no spurious
   changes and no noisy output (doctor is a diagnostic — preserve that UX).
3. Existing Deps/contract shape only. No internal/platform change.

## Scope
Write scope: `commands/doctor.go` only (+ `commands/doctor_test.go` if needed for
new-branch coverage — stp3 owns full regression). Interface seam for coverage, NO
coverage-exceptions allowlist entry.

## Verify
go build ./... clean; go test ./commands/ -count=1 green; go vet ./... clean;
gofmt -l . empty. Manually reason about idempotence (healthy project → no-op).

## Closeout
Commit on stp-doctor, push, open PR to master `feat(stp-doctor-repair): doctor
repair runs projection + links all managed da entities`. Body: the prior
symlink-only limitation, the (a)+(b) fix, idempotence reasoning, verification.
DO NOT merge (user gates). Final message: what changed, idempotence argument,
verification.
