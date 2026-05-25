# R3: Background-worker service — design

**Status:** draft (2026-05-25). Resolves the two open questions D2 routed to this plan. Cross-cutting D1–D5 live in the umbrella spec at `.agents/workflow/specs/agent-run-scoring-observability-platform/design.md` and are not re-litigated here.

## Scope recap (from umbrella)

R3 stands up a long-running service that:

1. Runs ≥2 background tasks on independent schedules/triggers (initially: telemetry-ingestion watcher + scoring-recompute task).
2. Exposes task health/history.
3. Hosts the HTTP surface for R2 (dashboard API) and R5 (review/labeling collection endpoint) — per D2 they mount into the same process, not separate daemons.
4. Publishes events the R2 dashboard can subscribe to (per D4 — real-time updates, not polling).

R3 is foundational: R2 and R5 cannot complete integration until R3's HTTP + event-publish surface is live (though R2 and R5 can develop standalone against the same interfaces in the meantime).

## Decision R3-D1 — Hosting model: long-running `da service` subcommand

**Decision:** R3 ships as `da service run` (plus `da service status`, `da service stop`), a long-running cobra subcommand of the existing `da` binary. Foreground process; daemonization is the operator's responsibility (systemd unit / launchd plist / `nohup`). No separate binary.

**Rationale.**
- Single `da` binary already built from `cmd/dot-agents/main.go`. Every existing capability is a cobra subcommand. A new subcommand reuses rooted flag/help/JSON-output machinery.
- The service consumes in-repo state (`.agents/active/iteration-log/`, `.ralph-loop-streams/`, the KG sqlite under `internal/graphstore/`) — co-locating it with the CLI that produces that state avoids a cross-binary contract.
- Operators already invoke `da` repeatedly per session; `da service status` is the discoverable health check, matching the umbrella verification clause.
- Daemonization (process supervision, log rotation, restart-on-crash) is a deployment concern, not a code concern. Go process exits cleanly on SIGINT/SIGTERM and ships structured logs to stderr; systemd/launchd handle the rest. No supervisor code in-tree.

**Rejected alternatives.**
- Standalone binary (`da-service`). Adds a second build/install target + module entrypoint. Only win is cosmetic. Rejected.
- Sidecar to the CLI (background goroutine the CLI spawns). Lifetime coupled to CLI invocations is wrong for a long-running ingester; restart semantics become "whoever invoked the last `da` command owns the daemon." Rejected.
- Embedded into an existing long-running command (e.g. `da workflow watch`). Conflates responsibilities: workflow watching is for the loop, the service is for the observability platform. Rejected.

## Decision R3-D2 — Task framework: minimal in-process scheduler

**Decision:** Build a minimal in-process scheduler in `internal/service/scheduler` (~300–500 LoC + tests). Tasks are Go funcs registered with a name, trigger (cron-like interval OR fsnotify filesystem trigger), and timeout. The scheduler owns goroutine lifecycle, panic recovery, last-run/next-run/last-error bookkeeping, and a stop channel honored on SIGINT/SIGTERM. No external dependency for the scheduler itself; `fsnotify` is added for filesystem triggers.

**Rationale.**
- Initial task set is 2–4 tasks all running in the same process — not a distributed job queue. The use-cases `asynq` / `river` optimize for (Redis/Postgres-backed durable queues, retries across worker fleets, scheduled delayed jobs across machines) are not present.
- Adding `asynq` requires Redis as a runtime dependency; `river` requires Postgres. The repo currently uses only sqlite (`modernc.org/sqlite`) and optionally pgx for the graphstore Postgres adapter. Forcing a Redis/Postgres dependency on every R3 deployment for a scheduler is a significant ops burden.
- In-flight task state (per umbrella verification "restart preserves in-flight task state") is solved by checkpointing per-task watermarks to YAML sidecars (analogous to how scoring writes `iter-N.score.yaml`). A task framework's durable queue is not needed when the tasks are idempotent watchers, not transactional units of work.
- The scheduler is small enough to own. A bug we wrote is a bug we can fix; a bug in `asynq` is a vendored-dependency upgrade.

**Rejected alternatives.**
- `asynq` (Redis-backed). Requires Redis. Job-queue model is overkill for cron+fsnotify triggers. Rejected.
- `river` (Postgres-backed). Requires Postgres at runtime even for sqlite-only deployments. Rejected.
- `robfig/cron` only. Solves only cron triggers, not fsnotify; we would still need a wrapper. Rejected (but its expression parser may be vendored as a tiny dep if interval-string parsing grows beyond Go's `time.ParseDuration`).

**Re-evaluation trigger:** if R4's eval harness grows into a job queue (multi-machine workers, durable retries, sandboxed agent exec), revisit `river`. The per-task-framework boundary is internal to `internal/service/scheduler`, so swap-out cost is contained.

## Architecture sketch

```
cmd/dot-agents/main.go           (unchanged)
commands/service/                (NEW — cobra subcommand surface)
  service.go                       `da service run|status|stop`
  run.go
  status.go
internal/service/                (NEW — service runtime)
  service.go                       Run(ctx, Config) orchestrates everything
  config.go                        Config (port, iter-log dir, task toggles)
  scheduler/                       In-process scheduler
    scheduler.go                   Register, Start, Stop
    task.go                        Task contract (name, trigger, run func)
    trigger.go                     Interval + fsnotify triggers
    state.go                       Per-task last-run/next-run/last-error
  events/                          Publish primitive (D4)
    bus.go                         In-process pub/sub (channels)
    bus_test.go
  http/                            HTTP server + mount points for R2/R5
    server.go                      net/http, graceful shutdown
    routes.go                      Mount points; /healthz; /api/tasks
    mounts.go                      RegisterR2Mount, RegisterR5Mount
  tasks/                           Concrete background tasks
    ingest_iterlog.go              fsnotify .agents/active/iteration-log/
    rescore.go                     Cron-like: rescore on rubric bump
    health.go                      Self-observability task
  state/                           Per-task watermark sidecars
    watermark.go                   .agents/active/service-state/*.yaml
```

## Surface contracts reserved for R2 and R5 (D2)

R3 does NOT implement R2/R5 endpoints — it reserves the mount machinery so they can land later without re-plumbing.

- `internal/service/http.RegisterMount(prefix string, h http.Handler)` — R2 calls this to mount `/api/*`; R5 mounts `/api/reviews/*`.
- `internal/service/events.Bus.Publish(topic, payload)` — R3 tasks publish iteration-ingested, rescore-done, task-error events. R2 will subscribe via SSE in its own plan; R3 only ships the bus.
- `internal/service/http.RegisterMount` is exported but its R2/R5 callers are STUB consumers in R3 — a no-op `RegisterR2Mount(srv, nil)` proves the wiring; R2's plan replaces the nil with a real handler.

## Per-task watermark protocol (restart-safe state)

Each task owns one watermark sidecar at `.agents/active/service-state/<task-name>.watermark.yaml`. The file captures whatever the task needs to resume idempotently — e.g. for the iter-log ingester, the last-processed iter number + its mtime; for the rescore task, the rubric version it last ran against.

Watermarks are written atomically (same temp+rename pattern as `internal/scoring/persist.go writeYAMLAtomic`). On startup each task reads its watermark; if absent it starts from scratch. This is what satisfies "restarting the service does not lose in-flight task state" without a durable job queue.

## Tasks shipped in R3 v1

1. **iterlog-ingester** (fsnotify trigger on `.agents/active/iteration-log/`): on any new `iter-N.yaml`, compute the R1 score (call into `internal/scoring`), publish an `iteration.scored` event on the bus, update watermark. This replaces the manual `da score run` cadence and is the primary live signal for R2.
2. **rescore-on-rubric-bump** (interval, e.g. 1m): poll the rubric version constant; when it differs from the last-applied version recorded in `<watermark>/rescore.yaml`, rescore everything via the existing `scoring.ScoreAll`. Idempotent.

(A KG-staleness-refresh task is in scope per the plan summary but the gcc1 contract changes are still settling — defer to a later slice rather than ship a half-wired version.)

## Verification approach

- Unit tests in `internal/service/scheduler` (trigger firing, panic recovery, stop semantics).
- Integration test: spin up the service in a goroutine pointed at a fixture iter-log dir, drop a new `iter-N.yaml`, assert the score sidecar appears AND an event lands on a test subscriber within 2s.
- Restart test: run service, write iter-1 + iter-2, kill, restart, write iter-3, assert only iter-3 is re-processed (watermark honored).
- HTTP smoke: `curl /healthz` returns 200; `curl /api/tasks` returns the registered task list with last-run/next-run/last-error.
