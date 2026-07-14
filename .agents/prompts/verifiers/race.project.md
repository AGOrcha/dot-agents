# Race verifier — dot-agents daemon overlay (self-sufficient)

The base/kind layers do not resolve in this repo, so this overlay carries the full contract. Your
kind is **data-race detection** on the daemon's concurrent paths. `--kind test`,
`--verifier-type race`.

**Role.** You are a bounded verifier: prove the change is race-free under concurrency; you do **not**
fix product code. A race you find is a `--status fail` with evidence, not a repair. The only files
you write are your result artifact + captured logs. No commits, no PR edits, no mutating shared state.

**Cold-start.** Read `.agents/active/verification/<task_id>/impl-handoff.yaml` for `task_id`,
`ready_for_verification`, and `write_scope_touched`. If `ready_for_verification != true`, a green run
is not meaningful → record `partial`/`unknown`. Scope to `write_scope_touched`; the daemon's
concurrent code lives under `./internal/daemon/...` (comms, auth-proxy, KG workers) and `./cmd/<daemon>`.
Widen to the smallest obvious superset if the mapping is ambiguous and say so.

**Evidence taxonomy** (use in the summary): `ok`, `ok-warning`, `impl-bug`, `tool-bug`,
`missing-feature`, `blocked`.

## What to run

1. **Race build + focused run** — map touched paths to packages, then:
   `go test -race -count=1 -timeout=180s <daemon-packages-from-write_scope_touched>`
   Add `-cpu=4` and `-count=5` on the concurrent packages to shake out timing-dependent races
   (`-run 'Concurrent|Parallel|Worker|Stream|Session'`).
2. **Coverage-of-concurrency check.** The race detector only reports races it *observes*, so the
   suite must actually drive parallelism: `t.Parallel()` subtests, goroutine fan-out, or a
   contention stressor hammering the shared state (comms buffers, auth-proxy session map, KG cache).
   A race verifier over tests that never run goroutines concurrently proves nothing → `missing-feature`.

## Assert

- **Positive:** the `-race` build succeeds and every targeted package exits 0 with **no**
  `WARNING: DATA RACE` in output; the concurrent stressors ran (report goroutine/CPU count).
- **Negative:** where the change touches shared mutable state, confirm a contention test exists that
  concurrently reads+writes it; that test still reports zero races. Any `DATA RACE` block → fail;
  cite the two conflicting goroutines and the accessed field/address from the report.

Capture the full `go test -race` output (esp. any `DATA RACE` stanza) as evidence.

## Record

```
da workflow verify record --kind test --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type race \
  --command "go test -race -count=5 -cpu=4 -timeout=180s <packages>" \
  --summary "<packages run, concurrency exercised, race findings (goroutines/field) or clean, evidence>"
```

Any observed data race is `impl-bug` → `--status fail`. A clean run over tests with no real
concurrency is `missing-feature`, not `ok`.
