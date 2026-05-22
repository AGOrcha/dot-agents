# Lesson: match CI test flags locally before pushing concurrency changes

## What happened

A graphstore shutdown change (track abandon-and-fail reapers; `Close()`
waits via `sync.WaitGroup`) was verified locally with
`go test ./internal/graphstore/ -run 'Deadlock|Guarded|...' -count=1`
and `go build ./...` — all green — then pushed. CI failed on **all
three OS**: `TestSQLiteStore_WedgedStepDoesNotDeadlockStore` →
"race detected during execution of test". CI's `GO_TEST_FLAGS` is
`-race -count=1 -timeout=300s`; the local runs omitted `-race`, so a
real `WaitGroup.Add`-not-ordered-before-`Wait` data race was invisible
locally and shipped.

## Root cause

The local verification command did not match the CI invocation. The
defect class (a data race from a lazily-spawned `WaitGroup` goroutine)
is *only* observable under `-race`. A non-race local pass gave false
confidence on exactly the dimension the change was about
(concurrency/shutdown ordering).

## Rule

1. **Any change touching goroutines, channels, `sync.*`, `context`
   cancellation, or shutdown ordering MUST be verified locally with the
   same flags CI uses — at minimum `-race -count=1`** — before commit/
   push. Check the workflow's `GO_TEST_FLAGS` and mirror it.
2. Do not infer "concurrency change is safe" from a non-`-race` run or
   from `go build`. Build never catches data races; `-race` is the
   instrument.
3. For dynamically-spawned tracked goroutines: every `WaitGroup.Add`
   (0→positive) must happen-before the `Wait`. Lazy `wg.Go()`/`Add`
   from a request path racing a `Close()`/`Wait()` is a race even if
   temporally separated. Standard fix: a mutex + `closed` flag —
   `Add` under the lock only while `!closed`; `Close` locks, sets
   `closed`, unlocks, then `Wait`; post-close work runs untracked
   best-effort.

## How to apply

- Before pushing a concurrency/lifecycle change in this repo, run:
  `go test ./<changed-pkg>/ -race -count=1` (and a focused
  `-race -count=10` on the new concurrent test if cheap, to shake out
  scheduling-dependent races).
- When adding a `WaitGroup` to await goroutines created on demand,
  reach for the mutex+`closed` pattern from the start; don't lazily
  `Add` from the hot path without an ordering edge to `Wait`.
- Generally: when a CI job has non-default test flags, the local
  pre-push command is "whatever CI runs," not the shortest green run.
