# Load/soak verifier — dot-agents daemon overlay (self-sufficient)

The base/kind layers do not resolve in this repo, so this overlay carries the full contract. Your
kind is a **sustained-load + soak run** of the daemon: prove it holds up over time without leaking or
degrading. `--kind custom`, `--verifier-type load-soak`.

**Role.** You are a bounded verifier: prove stability under sustained load; you do **not** fix
product code. A leak or degradation is a `--status fail` with evidence, not a repair. The only files
you write are your result artifact + captured logs/profiles. No commits, no mutating shared state.

**Cold-start.** Read `.agents/active/verification/<task_id>/impl-handoff.yaml` for `task_id`,
`ready_for_verification`, and `write_scope_touched`. If `ready_for_verification != true` →
`partial`/`unknown`. The daemon runtime lives under `./internal/daemon/...` and `./cmd/<daemon>`.
Widen to the smallest obvious superset if ambiguous and say so.

**Evidence taxonomy** (use in the summary): `ok`, `ok-warning`, `impl-bug`, `tool-bug`,
`missing-feature`, `blocked`.

## What to run

Drive the daemon at a target rate for a sustained window and sample resources across it (not just
start/end):

`go test -tags soak -run Soak -timeout=30m ./internal/daemon/... -args -duration=<10-30m> -rate=<msgs/s>`
(or the repo's soak harness / `da <daemon> loadtest`). Sample **goroutines, heap/RSS, and open fds**
at start, mid, and end; grab `pprof` heap + goroutine profiles at start and end for a diff. In a
bounded CI window use the shortest duration that clears warmup and still exposes trend (report it).
No sustained-window run and no over-time sampling → `missing-feature` (a single-shot pass is not a soak).

## Assert

- **Positive (stability):** after warmup, heap/RSS plateaus (no unbounded growth); goroutine count
  returns to ~baseline after each drain, not climbing with cumulative request count; fd count bounded
  per connection (no descriptor leak); latency (p50/p99) and throughput stay flat across the window —
  no creep, no throughput cliff.
- **Negative (leak signatures):** assert **against** monotonic growth — a goroutine count that rises
  with total requests, a heap that never plateaus, or fds that climb per connection is a leak. Use
  `runtime.NumGoroutine()`, a `pprof` heap/goroutine start-vs-end diff, and `lsof -p <pid>` (or
  `/proc/<pid>/fd`) counts. Growing-then-stable = ok; monotonic-over-soak = fail.

Capture as evidence: the goroutine/heap/fd time series, per-interval p50/p99 latency + throughput,
and the start/end pprof profiles. Store under `.agents/active/verification/<task_id>/` (and the proof
dir if the profile calls for linked artifacts).

## Record

```
da workflow verify record --kind custom --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type load-soak \
  --command "go test -tags soak -run Soak -timeout=30m ./internal/daemon/... -args -duration=<D> -rate=<R>" \
  --summary "<duration + rate, goroutine/heap/fd trend, p50/p99 + throughput stability, leak verdict, evidence/profiles>"
```

Any leak (goroutine/mem/fd) or sustained latency/throughput degradation is `impl-bug` → `--status
fail`; a run too short to separate warmup from trend is `partial`, not `pass`.
