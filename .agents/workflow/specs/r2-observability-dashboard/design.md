# R2 — Real-time observability and evaluation dashboard — spec

**Status:** draft (2026-05-27)
**Scope:** project (dot-agents)
**Parent spec:** [`agent-run-scoring-observability-platform`](../agent-run-scoring-observability-platform/design.md)
**Plan:** `.agents/workflow/plans/r2-observability-dashboard/` (PLAN.yaml + plan-level design.md)
**Sibling specs:** [`r4-code-task-generation-eval`](../r4-code-task-generation-eval/design.md), [`r5-review-labeling-access`](../r5-review-labeling-access/design.md)

## Problem

R1 has shipped: every iteration and session now has a numeric score, breakdown, and integrity observations persisted as `iter-N.score.yaml` / `session-*.score.yaml` sidecars (`internal/scoring/persist.go`). There is no surface a developer or operator can look at to answer the live questions: *which runs are healthy, which iteration just regressed, what's the cache-hit trend across the last hour, where did integrity observations cluster, which task is the next eligible one to take?* Today these questions are answered by `da workflow status` + grepping files + reading sidecar YAML by hand, and none of those refresh as a wave progresses. The umbrella spec D4 ("real-time updates as runs progress") is the verbatim gap.

## Goals

1. Make every R1 signal queryable through a single addressable surface (HTTP API).
2. Render a live view of in-flight orchestration so the developer driving a wave does not have to context-switch into raw files to know what just happened.
3. Push, not poll: the UI updates within 2 seconds of an iter-log write without the client asking.
4. Stay read-only in v1 — mutation lives in [`r5-review-labeling-access`](../r5-review-labeling-access/design.md).
5. Ship as part of the same Go binary R3 will already deliver — no separate node runtime, no separate deploy.

## Personas

- **Wave-driver (primary).** A developer running one or more delegated worker fanouts; needs a live look at iteration scores, integrity gaps, hook-outcome regressions, and a per-run drill-down to triage a flagged iteration without leaving the dashboard.
- **Backlog-scanner (secondary).** A developer between waves; needs aggregate trend views (score trend, cache-hit trend, cost trend) to decide which lesson to write, which gate to tighten, or which plan to pick up next.
- **Cross-session ops (deferred to a later wave).** A user inspecting historical runs across weeks; v1 only serves the active iter-log + sidecars discoverable from config, not arbitrary historical roots.

## Decisions

### D2.1 — Read-through surface over the existing sidecars; no new persistent store in v1

The dashboard reads directly from `.agents/active/iteration-log/` plus the `iter-N.score.yaml` / `session-*.score.yaml` sidecars R1 already writes. Aggregates are computed on demand inside the service with an in-process LRU.

**Why:** introducing SQLite or Postgres forks the source-of-truth question — the iter-log writers (`commands/workflow/close_task.go`, `internal/scoring/persist.go`) become "publishers" to a parallel store that must be kept in lockstep. The umbrella spec already defers ingestion design to R3. R2 should not pre-pay that cost at v1 volumes (≤ 10⁴ iterations).

**Rejected:** SQLite-first denormalization; Postgres; an in-memory index seeded on service start. All move complexity earlier than measured read-latency justifies.

**Forward door:** the read path sits behind a `Store` interface so a denormalized backend can be swapped in once R3 owns an aggregation task and there is real volume to measure.

### D2.2 — Real-time channel is server-pushed, not polled

The umbrella spec D4 forbids polling. R2's backend exposes a single push channel that the UI subscribes to once; new iteration scores, recomputed sessions, and rubric updates arrive as discrete events. The browser receives them through a built-in primitive (no client library tax) and the channel survives reverse proxies without special handshake.

**Why:** traffic is server → client only; a bidirectional channel buys nothing in v1. R5 may add write-back later, but R5's writes can ride their own POST endpoint without making R2's channel bidirectional.

**Fallback:** if a client's subscription drops, it refetches the affected queries on reconnect. v1 does not attempt server-side replay of missed events.

### D2.3 — Backend and frontend ship in one Go binary, behind R3's service host

The frontend is built statically and embedded into the binary R3 already needs to ship (`go:embed dist/`). The dashboard backend mounts under R3's HTTP host and consumes R3's publish primitive. R2 does not own service lifecycle, listener configuration, or hosting model — those belong to R3.

**Why:** single artifact, single port, single config surface. A separate node runtime would double the deploy story for no user-visible win.

**Forward door:** in dev, a flag points static assets at a frontend dev server for HMR — production runs purely from the embedded bundle.

### D2.4 — Frontend is a single-page application with no SSR

Aggregate dashboard, per-run drill-down, per-iteration deep view, and a rubric explainer are all client-rendered. There is no SEO concern, no per-request hydration, no anonymous public surface.

**Why:** SSR frameworks pay deployment-complexity costs we get no return on for a single-tenant in-binary dashboard. A plain SPA collapses cleanly into `go:embed`.

### D2.5 — Read-only surface in v1

No write endpoints, no editing, no comment threads. Mutation (labels, comments, role-gated actions) is the entire reason [`r5-review-labeling-access`](../r5-review-labeling-access/design.md) exists. R5 extends the same routes R2 owns by augmenting response payloads behind a composition point — it does not fork the surface.

### D2.6 — Binds to loopback by default

No authn/authz in v1. The service is single-tenant and listens on `127.0.0.1`. Network exposure is an R5 concern (RBAC + identity layer).

## Requirements (behavioral)

1. **R1.** A request for the run list returns every session present in the resolved iter-log roots with its aggregate session score, iteration count, last-iteration timestamp, and a stable session ID — sortable and paginatable.
2. **R2.** A request for a single run returns its iteration list with per-iteration scores, signal presence flags, and integrity observation counts. Missing scores (sidecar not yet written) return a null score with the iteration still listed.
3. **R3.** A request for a single iteration returns the full record + score breakdown (per-signal sub-score, weight, vote/no-vote) + integrity observations + transcript turn count.
4. **R4.** A request for the active rubric returns its version, signal list, weights, and band thresholds — sourced from `internal/scoring/rubric.go` at process start.
5. **R5.** When a new iteration score sidecar is written on disk, a connected client receives a notification keyed by `{session_id, iteration}` within 2 seconds of the file write. The client uses that notification to invalidate the relevant query.
6. **R6.** When a session score sidecar is updated (recomputation under the same rubric version), connected clients receive a notification keyed by `session_id`.
7. **R7.** When the rubric is recomputed under a new rubric version, all connected clients receive a single broadcast event that triggers a full reload.
8. **R8.** Every response carries a stable schema versioned for forward evolution; payload shapes are pinned by a JSON schema that contract tests validate against.
9. **R9.** The service exposes a health endpoint reporting run count, last iter-log mtime, and connected subscriber count for cheap operator triage.
10. **R10.** The dashboard renders correctly with zero runs (cold start) and with a corrupt sidecar (single bad file does not break the list).

## Done criteria (verifiable)

1. `curl -s localhost:<port>/api/runs | jq '.data | length'` returns a non-negative integer against the active iter-log.
2. `curl -s localhost:<port>/api/runs/<id>/iterations | jq '.data[0].score'` returns either a numeric score or `null` (never an error) for any iteration listed in `/api/runs`.
3. While a `da score run` is in progress, a browser tab open to the dashboard receives a push event for the new iteration within 2 seconds of the iter-log write (measured by Playwright or an integration test).
4. Killing the backend mid-stream and restarting it: the open browser tab reconnects within 5 seconds and refetches without showing stale data older than the disconnect.
5. A contract test asserts every handler's response validates against the shipped JSON schema; CI fails on drift.
6. A unit test asserts that with zero sidecars on disk, every list endpoint returns an empty data array and a 200 status, never a 500.
7. A drill-down click in the UI from `/` to `/runs/:id` to a single iteration view loads and renders within 1 second against a fixture of 100 runs / 1000 iterations.

## Open questions (must resolve before or during implementation)

1. **OQ1 — Iter-log root discovery.** v1 reads the active iter-log root; do we also surface historical roots that ship in the same repo (`.agents/history/<plan>/`)? Recommendation: defer to a later wave; v1 wires a list-of-roots config field but only ships the active root in the default config.
2. **OQ2 — Score-sidecar staleness window.** A window exists between iter-log write and score sidecar write where R2 sees an iteration with no score. UI affordance for this state must be confirmed at handler-design time (proposal: literal "scoring…" pill in the cell; null in the JSON).
3. **OQ3 — Filesystem watch semantics on macOS.** FSEvents latency (~50-200ms) eats real margin in the 2-second budget when combined with the broker fan-out. Confirm whether a 1-second mtime poll runs alongside the watcher as a belt-and-suspenders fallback or replaces it.
4. **OQ4 — Cost trend signal.** R1 captures cache-hit and token-usage signals; is there a derived dollar-cost projection in the trend view, or is the v1 trend purely cache + token? Recommendation: derive from configured price-per-token-class in `.agentsrc.json`; v1 surfaces token + cache only if price config is absent.
5. **OQ5 — Subscriber backpressure.** If a slow client cannot drain the push channel, do we drop events, buffer with a bound, or close the connection? Recommendation: bounded buffer with disconnect-on-overflow, client refetches on reconnect.
6. **OQ6 — Aggregate window.** Trend views show "the last N runs / last N hours / since rubric version X" — pick a single primary axis for v1 to avoid filter combinatorics.

## Deferred (explicitly out of scope)

- Write endpoints, label submission, comment threads — owned by [`r5-review-labeling-access`](../r5-review-labeling-access/design.md).
- Authn/authz, role-based filtering, audit trail — same.
- CSV/Parquet export.
- Mobile-responsive design pass; v1 is desktop-first.
- Alerting / threshold notifications.
- Cross-repo aggregation (single repo per service instance in v1).
- Historical iter-log roots (see OQ1).
- A denormalized read-store (see D2.1 forward door).

## Relationship to other specs and plans

- **[`agent-run-scoring-observability-platform`](../agent-run-scoring-observability-platform/design.md)** — parent; D4 is the live-updates requirement R2 implements; D2 says R3 hosts R2's API.
- **`r3-background-worker-service`** (sibling plan, no spec yet) — owns the service host, the publish primitive, and the lifecycle. R2 cannot ship without R3's publish primitive interface. Both can be implemented in parallel against a placeholder publisher, but the integration smoke test gates on R3 milestone.
- **[`r4-code-task-generation-eval`](../r4-code-task-generation-eval/design.md)** — produces additional iter-log roots (`.agents/eval/runs/<id>/iteration-log/`) the dashboard must learn to discover. The contract pinned in OQ1 covers this.
- **[`r5-review-labeling-access`](../r5-review-labeling-access/design.md)** — extends R2's routes with `labels[]` payload augmentation behind a composition point; consumes R2's API client + auth-header plumbing.
- **`r1-outcome-scoring`** (completed) — supplies every score and signal R2 renders.
- **[[verifier-owns-ci-watch-shift-left]]** — the dashboard's CI/Sonar gate panel (deferred) will eventually consume the same `pr-ci` verifier outputs that the project-overlay verifier profile already writes to `.agents/active/verification/<task_id>/pr-ci.result.yaml`.
- **[[worker-owns-pr-readiness-loop]]** — loop-worker readiness summaries are a candidate signal in a later wave; v1 does not consume them.

## Candidate canonical-plan tasks (appendix; not yet materialized)

This list seeds the plan slate. The plan's `PLAN.yaml` + `TASKS.yaml` already exist; this list is the spec-side reconciliation against the current plan tasks so a future planner can verify the plan still answers what the spec asks for.

1. **t-api-contract** — pin endpoint set + payload shapes + JSON schemas + event taxonomy; ship `schemas/dashboard-*.schema.json`. (Maps to existing `t01-api-contract-design`.)
2. **t-store-read** — implement the read-through `Store` over iter-log + sidecars; LRU + mtime invalidation. (Maps to `t02-storage-read-layer`.)
3. **t-handlers** — implement REST handlers against the contract; contract test against JSON schemas. (Maps to `t03-handlers-rest`.)
4. **t-push-broker** — implement the push broker against the R3 publisher placeholder; fan-out + heartbeat + bounded buffer. (Maps to `t04-sse-broker`.)
5. **t-publisher-wiring** — integrate against R3's real publisher when it lands (cross-plan dep). Gated on R3 milestone.
6. **t-watcher-fallback** — fsnotify primary + 1-second mtime poll fallback per OQ3.
7. **t-recompute-on-miss** — handle the staleness window per OQ2.
8. **t-frontend-shell** — SPA skeleton + routing + auth-header plumbing for R5 reuse + TanStack-Query client + push subscription.
9. **t-frontend-aggregate** — runs grid + score trend + cache trend.
10. **t-frontend-drilldown** — per-run iteration timeline + per-iteration breakdown view.
11. **t-frontend-rubric** — rubric explainer view.
12. **t-embed-bundle** — `go:embed` build + dev-mode asset proxy flag.
13. **t-e2e-live** — end-to-end test: start service against fixture iter-log, run `da score run`, assert push frame within 2s.
14. **t-cold-start** — empty-iter-log + corrupt-sidecar resilience tests.

The plan-level `design.md` at `.agents/workflow/plans/r2-observability-dashboard/design.md` already encodes the implementation tech for each of the above; this spec is the contract the plan is accountable to. If the plan drifts (different surface, missed requirement, ignored done criterion), this spec is authority.
