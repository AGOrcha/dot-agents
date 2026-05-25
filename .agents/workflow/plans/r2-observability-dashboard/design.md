# R2: Real-time observability dashboard — plan-level design

**Status:** draft (2026-05-25)
**Spec:** `../../specs/agent-run-scoring-observability-platform/design.md`
**Depends on:** R3 (service host + publish primitive), R1 (scores — shipped)

## Scope recap

R2 ships:
- A backend HTTP API serving aggregate run/session/iteration telemetry + R1 scores + per-signal breakdowns + correction/integrity observations.
- A React/TS single-page app rendering:
  - Aggregate dashboard (runs grid, score trend, cache-hit trend, cost trend).
  - Per-run drill-down (iteration timeline, per-iteration score breakdown, integrity gaps, transcript turn count).
- A push channel for live updates (new iteration → dashboard re-renders within 2s of iter-log write, per spec verification clause).
- End-to-end smoke against a live `da score run` invocation.

R2 does NOT ship:
- R3's service skeleton, scheduler, or hosting model (that is R3).
- The publish primitive itself (R3 owns it; R2 consumes it).
- New scoring signals, rubric changes, or new captured telemetry.
- Authn/authz (R5's job; R2 v1 is anonymous read-only inside the service boundary).

## Decisions

### Q1 — Frontend tooling: Vite + React + TypeScript + TanStack Query

**Decision.** Vite + React 18 + TypeScript + TanStack Query (v5) + Tailwind for styling + Recharts (or visx) for time-series. Static-built bundle served as embedded assets from the Go service.

**Rationale.**
- Single-page app with no SSR need — no SEO, no unauthenticated public pages, no per-request server data hydration.
- Vite's dev server + HMR is the lowest-friction iteration loop; smallest dep tree per route.
- TanStack Query natively models server-state + invalidation + the SSE-event invalidation pattern.
- Static build means `go:embed dist/` works — the dashboard ships in the same binary R3 already needs to ship; no separate node runtime, no separate deployment artefact.

**Rejected.**
- **Next.js.** App-router SSR + RSC pay deployment-complexity costs we get no return on for a single-user-tenant dashboard. Vercel-shaped deployment story conflicts with R3's "single Go binary" trajectory.
- **Remix.** Same trade-off; no win.
- **Plain HTML + htmx.** Tempting for cost-aggregate views, but the per-run drill-down has interactive timeline + filter UI that React-with-state wins on.
- **SvelteKit / Solid.** Smaller bundles but team-familiarity tax + smaller charting ecosystem.

### Q2 — Storage: read-through from telemetry on disk; in-process LRU cache only

**Decision.** No new persistent store for v1. Backend handlers read directly from `.agents/active/iteration-log/` (and historical iter-log roots discovered via config) and from the `iter-N.score.yaml` / `session-*.score.yaml` sidecars the R1 persist task already writes. An in-process LRU cache (~256 entries, TTL 30s) sits in front of disk reads to bound p99 latency on hot endpoints.

**Rationale.**
- All read-surface data already lives on disk in addressable form. `LoadIterationLog` + a `glob(iter-log-dir/*.score.yaml)` is the full read.
- Introducing SQLite/Postgres adds an ETL surface that has to stay in lockstep with the iter-log writers (`commands/workflow/close_task.go`) and the score persister. The umbrella spec already deferred ingestion-design to R3 — R2 shipping a parallel write path forks the source-of-truth question.
- R3's task framework will eventually own a "score-backfill" or "aggregate-rollup" task — the right time to denormalize is once that task exists and we measure read latency on real volume.
- For v1 volumes (≤ 10⁴ iterations across ≤ 10³ sessions) walking the dir is ≤ 100ms cold, well inside a 2s push budget.

**Rejected.**
- **SQLite denormalization now.** Forks source-of-truth, doubles the failure surface (cache invalidation bugs), pre-pays a cost we can't justify at v1 volume.
- **Postgres.** Same problem + an operational dep R3 doesn't yet need.
- **In-memory index built on service start, refreshed on FS-watch.** Half-step — combines storage and push-channel concerns. Defer.

**Forward door.** A `dashboard.Store` interface fronts the read path so a future SQLite/Postgres backend swaps in without touching handlers.

### Q3 — Real-time mechanism: Server-Sent Events (SSE)

**Decision.** SSE for client-bound push. R3 exposes a `Publish(event)` primitive (per spec D4); R2's backend subscribes to it and fans out to connected SSE clients via a small in-process broker (`internal/dashboard/events`). Heartbeat every 15s. Client uses `EventSource` (browser-native).

**Rationale.**
- Traffic is server→client only. WebSocket's bidirectional channel buys nothing; SSE is half the protocol complexity and reuses the existing HTTP stack.
- SSE survives proxies/load balancers more reliably than WS (no upgrade handshake; just a long-lived `text/event-stream` response).
- Native browser `EventSource` is built-in — no client library tax.
- The "polling-with-watermark" fallback the spec mentions is incompatible with D4 ("push-based, not polling") — explicitly out.
- WebSocket is a reasonable second-system if we later add reviewer-side write events (R5 labeling), but R5 can run on its own POST endpoint without making R2's channel bidirectional.

**Rejected.**
- **WebSocket.** Bidirectional capability we don't need; heavier client + server code paths.
- **Long-poll with watermark.** Spec D4 forbids polling.
- **Push via gRPC-web streaming.** Adds a build-time codegen surface for a single channel; no proportionate win.

**Fallback.** If a client's `EventSource` connection drops, the client refetches the affected query via TanStack Query on reconnect (idempotent). We do not attempt to replay missed events server-side in v1.

### Q4 — API surface shape

REST/JSON, resource-oriented. Endpoints:

```
GET  /api/runs                       — list runs (= sessions), filterable, paginated
GET  /api/runs/:session_id           — one run's metadata + session score
GET  /api/runs/:session_id/iterations — iteration list for a run
GET  /api/iterations/:n              — one iteration's full record + score breakdown
                                       (?iter_log_dir=… override for non-active logs)
GET  /api/rubric                     — active rubric (version, signals, weights, bands)
GET  /api/health                     — liveness + counts (run count, last iter mtime)
GET  /api/stream                     — SSE channel; events: iteration.scored,
                                       session.updated, score.recomputed
```

Versioning: `/api/v1/...` reserved; v1 elided in URL path for v1 only, asserted by contract test. Response envelope: `{ "data": …, "meta": { "etag": … } }`.

### Q5 — Iter-log directory discovery

Service reads from a config-resolved iter-log root list. Default is `<repo>/.agents/active/iteration-log/`. Additional roots come from R3's config surface.

### Q6 — Frontend routing

`/` aggregate dashboard. `/runs/:sessionId` per-run drill-down. `/iterations/:n` deep link to one iteration. `/rubric` rubric explainer.

## Anti-scope

- No authn/authz; service binds to `127.0.0.1` by default.
- No editing — read-only UI in v1 (R5 introduces writes).
- No alerting/threshold UI.
- No export-CSV in v1.
- No mobile-responsive design pass (desktop-first; reviewer workstations).

## Verification strategy

1. `go test ./internal/dashboard/...` — handler unit tests with a fixture iter-log dir; assert JSON shape via golden files.
2. `go test ./internal/dashboard/events/...` — SSE broker fan-out test (N subscribers each receive event within 100ms).
3. Contract test: every handler response validates against a JSON Schema shipped in `schemas/dashboard-*.schema.json`.
4. Frontend: Vitest + React Testing Library for components; Playwright smoke for the live-iteration path.
5. End-to-end: a Go integration test starts the standalone service against a fixture iter-log, runs `da score run`, then asserts a `iteration.scored` SSE frame appears within 2s.
6. CLI smoke: `curl -s localhost:<port>/api/runs | jq` returns non-empty array (per spec umbrella verification).

## Risks

- **R3 publish-primitive shape unknown.** R2 designs against a placeholder `events.Publisher` interface. When R3 lands the real one, the broker swaps. Mitigation: tiny `Publish(topic string, payload any)`, gate the production wiring task on R3 milestone.
- **Score-sidecar staleness.** Sidecars are written by `da score run` / `da workflow close-task` — between iter-log write and score sidecar write there is a window where R2 sees an iteration with no score. Handler returns `score: null` in that window; UI shows a "scoring…" pill.
- **Embedded static assets vs. dev mode.** Production builds use `go:embed`; dev mode proxies to Vite. A `--dev-asset-proxy http://localhost:5173` flag on the service.
- **Historical iter-log discovery.** Out of scope for v1 (active log only); future enhancement.
- **fsnotify on macOS (FSEvents) latency ~50-200ms.** Within 2s budget but eats margin. Backup: t06 polls iter-log mtime every 1s; whichever fires first wins.
