# Delegation: stp1 — wire shared-target projection into refresh

- plan/task: shared-target-projection-wiring / stp1-wire-refresh
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/stp-projection`
  (branch `stp-projection` off master efb19756 — already includes #26)
- reference impl (forward-port, verify against CURRENT master — may have
  drifted): feature-branch commits `829a3490` ("commands: wire
  CollectAndExecuteSharedTargetPlan before platform loops") and
  `c78b470b` ("platform: add RunSharedTargetProjection for
  refresh/install/add"). Inspect via `git show 829a3490`,
  `git show c78b470b` (they are in repo history, just not on master).
- spec: `.agents/workflow/specs/shared-target-projection-wiring/design.md`
- status: delegated
- First command: `cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/stp-projection`

## Goal

Invoke the shared-target projection once per project in
`commands/refresh.go`, BEFORE the per-platform `CreateLinks` loop,
scoped to the enabled platform set — so repo-level `.codex/agents/*.toml`
and Claude shared `.claude/skills/` / `.agents/skills/` projection
materialize on master.

## Scope (strict)

- Write scope: `commands/refresh.go` ONLY (+ a `refresh_test.go` test if
  needed for coverage — stp3 owns the full regression suite, but cover
  any new branch you add here per the gate).
- Producer is COMPLETE on master (`internal/platform` shared-target
  machinery, `bdaf37ea`) — do NOT modify it. This task is the
  command-layer invocation only.
- Use the EXISTING `Deps`/contract shape. Do NOT introduce a new DI
  pattern (must not pre-empt di-refactor-rollout / graphstore-contract).
- Cover new branches via interface-seam pattern, NOT a
  coverage-exceptions allowlist entry (standing policy).

## Method

1. `git show 829a3490` + `git show c78b470b` to read the reference
   wiring; reconcile against current `commands/refresh.go`
   (`CollectAndExecuteSharedTargetPlan` / `RunSharedTargetProjection` /
   `BuildSharedTargetPlan` live in `internal/platform/resource_plan.go`).
2. Add the projection call before the per-platform `CreateLinks` loop,
   passing the enabled platform set, matching reference ordering.
3. **Resolve spec Q1:** grep `commands/` + `cmd/` for any 4th production
   entry that also runs per-platform `CreateLinks` without the
   projection (a `sync` path?). Record the finding in the merge-back
   (stp2 will wire install/add; flag here if a 4th exists).

## Verify

`go build ./...` clean; `go test ./commands/ -count=1` green;
`go vet ./...` clean; `gofmt -l .` empty. Sanity: a dry-run of refresh
in a scratch project shows the projection intents.

## Closeout

Commit on `stp-projection`, push, open PR to master titled
`feat(stp1): wire shared-target projection into refresh`. Body: the
reference commits forward-ported, the call site + ordering, Q1 finding
(4th entry yes/no), verification. DO NOT merge (user gates). Final
message: what was wired, Q1 resolution, anything flagged.
