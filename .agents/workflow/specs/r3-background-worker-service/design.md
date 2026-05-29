# Spec — r3-background-worker-service

**Status:** draft (designer-B pass, 2026-05-27). Promotes the existing
`.agents/workflow/plans/r3-background-worker-service/design.md` into the
canonical spec form and adds the contract sections (open questions, deferred,
relationship) the in-plan document elided.

## 1. Problem statement and goals

Today, agent-run telemetry is produced by hook-and-CLI invocations: hooks
write `.agents/active/iteration-log/iter-N.yaml`, then a human (or scheduled
loop) runs `da score run` to materialize sidecar scores. Two consequences:

1. The score sidecars are stale until somebody runs the CLI. Anything that
   wants to watch agent runs in real time (R2 dashboard, R5 review queue,
   R4 eval harness) has to poll the filesystem or re-run scoring on every
   read.
2. Rubric/version bumps don't auto-propagate; existing sidecars stay on the
   old rubric until an operator remembers to rescore.

**Goal:** stand up a long-running service that holds the moving parts which
must outlive a single CLI invocation — a watcher on the iter-log directory,
a rescore loop driven by the rubric-version constant, and the HTTP surface
that R2 and R5 will mount onto. The service is the single in-process host
for these jobs and the bus that lets them notify subscribers.

**Non-goals (v1).** Distributed job execution, durable job queues, multi-
machine fan-out, RBAC, frontend code, daemonization tooling. Those are
either downstream plans (R5 owns RBAC) or operator concerns (systemd /
launchd own daemonization).

## 2. Explicit decisions with rationale

### D1 — Hosting model: `da service` cobra subcommand

R3 ships as `da service run` (plus `da service status`, `da service stop`),
a long-running subcommand of the existing `da` binary. Foreground process;
daemonization is the operator's responsibility (systemd unit / launchd
plist / nohup). No second binary.

Rationale: every existing capability is a cobra subcommand, the service
reads in-repo state (`.agents/active/iteration-log/`, the KG sqlite under
`internal/graphstore/`), and co-locating it with the CLI avoids a cross-
binary contract. Daemonization is deployment, not code.

Rejected: standalone `da-service` binary (cosmetic, second install target);
sidecar to CLI invocations (wrong lifetime for an ingester); embedding
into `da workflow watch` (conflates loop concerns with observability).

### D2 — Task framework: minimal in-process scheduler

Build a ~300-500 LoC scheduler in `internal/service/scheduler/` (see
`scheduler-core` task in TASKS.yaml for shape). Tasks are Go funcs
registered with a name, a trigger (interval or fsnotify), and a timeout.
Scheduler owns goroutine lifecycle, panic recovery, last-run/next-run/
last-error bookkeeping, and `Stop(timeout)` draining.

Rationale: the initial task set (2-4 tasks) all run in-process; the
distributed-queue use cases that `asynq`/`river` optimize for (Redis/
Postgres-backed durable queues, multi-machine fleets) are absent. Forcing
Redis or Postgres as a runtime dep just for a scheduler is unjustified
ops burden when the repo currently runs on sqlite only.

Rejected: `asynq` (requires Redis); `river` (requires Postgres at
runtime); `robfig/cron` alone (only handles cron, not fsnotify).

Re-evaluation trigger: if R4's eval harness grows multi-machine workers
or durable retries, revisit `river`. The framework boundary is internal
to `internal/service/scheduler/`, so swap cost is contained.

### D3 — Restart safety via per-task watermark sidecars (not a durable queue)

Each task owns one `.agents/active/service-state/<task-name>.watermark.yaml`
file, written atomically (temp+rename, mirroring `internal/scoring/persist.go
writeYAMLAtomic`). On startup each task reads its watermark; absent ⇒ start
from scratch.

Rationale: the v1 tasks are *idempotent watchers* (re-scoring an iter is a
no-op if the watermark says it's already done), not transactional units of
work. A durable queue would be a heavier abstraction than the actual
restart semantics require. The watermark is also human-readable, which is
worth the cost over a binary checkpoint.

### D4 — Event bus is in-process pub/sub only

`internal/service/events.Bus` is channel-based, bounded buffer per
subscriber, drop-oldest on slow consumer, signature
`Publish(topic string, payload any)`. No durable storage (consumers re-
read canonical state from sidecars if they missed an event), no cross-
process IPC.

Rationale: events are "wake up and look at the new state on disk," not
authoritative deliveries. R2 will subscribe via SSE; R5 will mount its
collection endpoint as an HTTP handler that doesn't even need bus access
for v1. Keeping the bus tiny preserves the cross-plan contract surface.

### D5 — HTTP surface is reservation, not implementation

`internal/service/http` exposes `RegisterMount(prefix, handler)`. R3 only
ships `/healthz` and `/api/tasks` (the scheduler.State() projection). R2's
`/api/*` and R5's `/api/reviews/*` are mounted by their own plans through
the same registration call. R3 ships stub `RegisterR2Mount(srv, nil)`-style
no-op test wiring to prove the contract.

## 3. Requirements (behavioral, not implementation)

1. The service runs continuously in the foreground and exits cleanly on
   SIGINT/SIGTERM within a bounded shutdown timeout.
2. ≥2 background tasks run on independent triggers: an fsnotify-driven
   iter-log ingester and an interval-driven rescore-on-rubric-bump task.
3. After a restart, no task re-processes work it has already acknowledged
   via its watermark.
4. Task health (last-run-at, next-run-at, last-error, consecutive-failures)
   is observable via `da service status` and a JSON HTTP endpoint.
5. Other plans (R2, R5) can mount HTTP handlers under arbitrary prefixes
   without modifying R3 code.
6. Other plans can subscribe to an in-process event bus without modifying
   R3 code; slow consumers do not block publishers.
7. A panic in any task's RunFn is recovered, recorded as last-error, and
   does not bring down the scheduler.

## 4. Open questions

- **OQ1 — KG-staleness-refresh task in v1?** Plan summary mentions it; in-
  plan design.md defers it pending `graphstore-concurrency-contract`
  settle. That plan is now archived (per the cg6b note in
  coverage-gate-per-file TASKS.yaml). **Recommendation:** still defer to a
  v1.1 slice; v1 should ship the two tasks already specified to keep the
  surface small. Decision belongs to the orchestrator at materialization.
- **OQ2 — Concurrency limit per task?** Scheduler today implies one
  goroutine per task. If a task's RunFn takes longer than its trigger
  interval (rescore on a big run), does the next tick block, queue, or
  drop? Default proposal: drop with a counter; document. Confirm before
  `scheduler-core` lands.
- **OQ3 — Configurable bind address vs loopback-only by default?** Plan
  notes default `:7878` for the cobra surface. With R5 introducing RBAC
  later, defaulting to loopback (`127.0.0.1:7878`) and requiring an
  explicit `--addr 0.0.0.0:...` to bind externally is the safer default.
  Confirm before `cobra-surface` lands.
- **OQ4 — `da service stop` mechanism.** TASKS.yaml notes "POST
  /admin/stop ... gated on loopback only" — this is the only state-
  changing endpoint on the v1 server. Lock down to loopback even if D5
  later allows external bind. Tracked here because it cuts across `http-
  server` and `cobra-surface`.

## 5. Done criteria

- `da service run` starts, registers the two v1 tasks, serves `/healthz`
  + `/api/tasks`, and exits cleanly on SIGINT within 5s.
- Integration test: write `iter-N.yaml` to a temp iter-log dir under a
  running service; assert the score sidecar appears AND an
  `iteration.scored` event lands on a test subscriber within 2s.
- Restart test: run service, write iter-1+iter-2, kill, restart, write
  iter-3 — assert only iter-3 is re-processed.
- Rescore test: start service with rubric version A, advance the version
  to B, assert one rescore pass runs and a `rescore.done` event publishes.
- Mount test: a fake R2 handler registered via `RegisterMount("/api/test",
  ...)` is reachable; conflicting prefixes are rejected deterministically.
- HTTP smoke: `curl /api/tasks` returns the registered task list with
  last-run/next-run/last-error fields populated.
- `docs/SERVICE.md` documents start/stop, systemd + launchd examples,
  task list, mount contract, watermark layout, and troubleshooting.

These trace back to umbrella spec
`agent-run-scoring-observability-platform`'s D1-D5 cross-cutting decisions.

## 6. Deferred items

- KG-staleness-refresh task (OQ1).
- Distributed/multi-machine execution; if needed, swap scheduler for
  `river` per D2's re-evaluation trigger.
- RBAC on HTTP endpoints — R5's plan owns this.
- Frontend (R2 plan owns it).
- Daemonization scaffolding (operator concern).
- Log rotation / structured logging beyond stderr (operator concern).
- A second sibling Publisher interface for async/queue-backed events —
  per `event-bus` task notes, that's a follow-up if/when needed.

## 7. Relationship to other specs and plans

- **Umbrella:** `agent-run-scoring-observability-platform` owns D1-D5
  cross-cutting decisions. R3 implements D2 (long-running host) and D4
  (push events). Don't re-litigate those here.
- **Sibling R-plans:**
  - `r1-outcome-scoring` ships the sidecar shape this service ingests
    (`scoring.ScoreIteration`, `scoring.WriteIterationScoreWithRecord`).
    R3 calls into `internal/scoring`; it does NOT duplicate scoring.
  - `r2-observability-dashboard` consumes `/api/*` mount + event-bus
    subscription. R3 ships the mount machinery + bus; R2 ships the
    handler.
  - `r5-review-labeling-access` mounts `/api/reviews/*` and owns RBAC.
  - `r4-code-task-generation-eval` may grow into a workload that
    triggers D2's `river` re-evaluation.
- **Codex staged-dispatch model:** the codex-companion runtime already
  has its own per-stage agent dispatch; R3's scheduler is for in-repo
  background jobs, not LLM dispatch. The two surfaces don't overlap.
- **`[[verifier-owns-ci-watch-shift-left]]`:** existing verifier agents
  poll CI independently and are not run as scheduler tasks. R3's
  scheduler is for repo-local file watchers and idempotent rescores,
  not for tasks that need their own polling loop.
- **`[[validate-bundle-against-head]]`:** when this plan is fanned out,
  verify `internal/service/` does not exist on HEAD (confirmed
  2026-05-27 — `find /Users/nikashp/Documents/dot-agents/internal/
  service` returns nothing). Task `scheduler-core` adds it.

## Candidate canonical-plan tasks (appendix)

These already exist in `TASKS.yaml` (HEAD-verified 2026-05-27 — task IDs
match). This appendix is descriptive, not authoritative.

| Task ID | Status | Notes |
|---|---|---|
| design-doc | completed | Replaced by this spec; in-plan design.md retained for archive context |
| scheduler-core | pending | Add `fsnotify` dep; OQ2 (concurrency on tick overrun) must be answered before fanout |
| event-bus | pending | Confirm D4 surface is exactly `Publish(topic, payload)` before fanout |
| http-server | pending | OQ3 (loopback default) + OQ4 (`/admin/stop` lockdown) must be answered before fanout |
| tasks-iterlog-ingester | pending | Must call into `internal/scoring`, not duplicate |
| tasks-rescore | pending | Idempotent — no-op when rubric version unchanged |
| service-runtime | pending | Composes scheduler+http+bus; integration test is the umbrella verifier |
| cobra-surface | pending | Mirrors `commands/score.go` shape; OQ3+OQ4 propagate here |
| docs-and-verification | pending | `docs/SERVICE.md` + umbrella E2E integration test |

Cross-plan ordering: nothing in this plan depends on anything outside the
plan. R2 and R5 plans depend on `http-server` + `event-bus`, not the
other way.
