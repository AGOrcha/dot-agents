# R2 Dashboard API contract

**Status:** stable (2026-05-28) — gates `t02-storage-read-layer`, `t03-handlers-rest`,
`t04-sse-broker`, `t08-frontend-skeleton`.
**Spec:** `../../../specs/r2-observability-dashboard/design.md` (requirements R1–R10, done criteria, OQ1–OQ6).
**Plan design:** `../design.md` (decisions Q1–Q6).
**Governing convention:** `[[api-conventions]]` — `/api/v1/<domain>/<resource>`. This dashboard is
the `observability` domain; all paths below conform to that shape (`/api/v1/observability/...`).
**Schemas:** `schemas/dashboard-run.schema.json`, `schemas/dashboard-iteration.schema.json`,
`schemas/dashboard-rubric.schema.json`, `schemas/dashboard-event.schema.json`.

This document is the single source of truth for the dashboard's HTTP + SSE surface. The four
JSON Schemas under `schemas/` are the machine-readable form of the payload shapes here; the
contract test (`t03`) validates every handler response against them, and the frontend type
generator (`t08`, `json-schema-to-typescript`) consumes the same files. If this doc and the
schemas disagree, the schemas are authority for payload shape; this doc is authority for
endpoint behaviour (methods, params, status codes, semantics).

---

## 1. Conventions

### 1.1 Versioning

Per `[[api-conventions]]`, the version is **version-first at the root and always present in the
URL**: every path is `/api/v1/observability/<resource>` (e.g. `/api/v1/observability/runs`). There
is no per-domain version segment and no version elision. A contract test asserts the literal
`/api/v1/observability/` prefix so the move to `/api/v2/...` later is a deliberate, testable break.
All payloads carry a stable schema validated by the shipped JSON Schemas (spec R8).

### 1.2 Response envelope

Every successful JSON response is wrapped:

```json
{ "data": <payload>, "meta": { "etag": "<opaque>", "count": <int, list endpoints only> } }
```

- `data` — the resource (object) or resource list (array), shaped per the relevant schema.
- `meta.etag` — opaque ETag string (also sent in the `ETag` response header; see §1.5).
- `meta.count` — present only on list endpoints; equals `len(data)` for the returned page.

The JSON Schemas describe the **`data` payload** (or per-element shape for lists), not the
envelope. The contract test unwraps `data` before validating against the schema.

### 1.3 Error envelope

Simpler `{ error: { code, message } }` is chosen over RFC 7807 — no problem-type URIs, no content
negotiation, one shape the frontend parses unconditionally.

```json
{ "error": { "code": "not_found", "message": "no run for session_id \"abc\"" } }
```

`code` is a stable machine token; `message` is human-readable and may change. Error codes:

| code            | HTTP | meaning                                                        |
|-----------------|------|----------------------------------------------------------------|
| `bad_request`   | 400  | malformed/invalid query param (e.g. `limit=-1`, `iter=foo`)    |
| `not_found`     | 404  | no run/iteration for the given id                              |
| `internal`      | 500  | unexpected server error (never returned for a missing score — that degrades to `null`) |

`Content-Type: application/json; charset=utf-8` on every JSON response (success and error).

**Resilience rule (spec R10, done-criteria 6):** a corrupt or unparseable single sidecar/iter-log
file MUST NOT fail a list endpoint. The store skips the bad file, the list returns 200 with the
remaining entries, and a structured warning is logged. List endpoints with zero data return
`{ "data": [], "meta": { "etag": "...", "count": 0 } }` and status 200 — never 500.

### 1.4 Pagination

List endpoints accept `limit` and `offset` only (anti-scope: no cursors). `limit` default 50,
max 500; `offset` default 0. `limit=0` is rejected as `bad_request`. Out-of-range `offset`
returns an empty `data` array with 200. No `next`/`prev` links in v1.

### 1.5 Caching / ETag

Each response sets a weak-ish `ETag` derived from the newest contributing file mtime + the
resource key (e.g. `"runs:<max_mtime_unix>:<count>"`). A request carrying a matching
`If-None-Match` gets `304 Not Modified` with an empty body. The same etag is echoed in
`meta.etag`. ETag is purely a read-cache optimisation; live invalidation is the SSE channel's job.

### 1.6 Iter-log root resolution (design.md Q5 / spec OQ1)

The service resolves a **list** of iter-log roots from config (default: the single active root
`<repo>/.agents/active/iteration-log/`). Endpoints operate over the union of all resolved roots.
A `session_id` is globally unique across roots; an `iteration` number is unique within a root but
may collide across roots, so iteration endpoints accept an optional `iter_log_dir` query param to
disambiguate (defaults to the active root). Historical roots beyond the active one are deferred
to a later wave (OQ1) — the contract supports the list; the default config ships one root.

### 1.7 Binding & auth

Service binds `127.0.0.1` by default (spec D2.6). No authn/authz in v1 (R5 concern). All
endpoints are anonymous read-only. No write endpoints exist (anti-scope).

---

## 2. Endpoint catalogue

Seven endpoints. All `GET`. Base path `/api/v1/observability` (the `observability` domain of
`[[api-conventions]]`).

| # | Method | Path                                                | Returns (data)                | Schema                          |
|---|--------|-----------------------------------------------------|-------------------------------|---------------------------------|
| 1 | GET    | `/api/v1/observability/runs`                        | `RunSummary[]`                | dashboard-run                   |
| 2 | GET    | `/api/v1/observability/runs/{session_id}`           | `RunDetail`                   | dashboard-run                   |
| 3 | GET    | `/api/v1/observability/runs/{session_id}/iterations`| `IterationSummary[]`          | dashboard-iteration             |
| 4 | GET    | `/api/v1/observability/iterations/{n}`              | `IterationDetail`             | dashboard-iteration             |
| 5 | GET    | `/api/v1/observability/rubric`                      | `RubricDoc`                   | dashboard-rubric                |
| 6 | GET    | `/api/v1/observability/health`                      | `Health` (inline §3.6)        | — (inline)                      |
| 7 | GET    | `/api/v1/observability/events`                      | `text/event-stream`           | dashboard-event (per `data:`)   |

The event stream lives at `/api/v1/observability/events` per the `[[api-conventions]]` event-stream
rule (`/api/v1/<domain>/events`); SSE is the transport for this loopback single-user build.

`RunSummary` / `RunDetail` are the same schema (`dashboard-run`); a **summary** omits the optional
`per_iteration` array to keep the list payload small, a **detail** includes it. Likewise
`IterationSummary` / `IterationDetail` share `dashboard-iteration`; the summary omits
`breakdown`, `integrity`, `objective`, and `verifiers` (the heavy detail-only fields).

### DTO ↔ Go-source mapping (the projection contract)

The DTOs are a **presentation projection** — the names below are pinned so handler authors
(`t03`) and frontend authors (`t08`) share one vocabulary. Source types live in `internal/scoring`.

| DTO                | Built from (internal/scoring)                                  |
|--------------------|----------------------------------------------------------------|
| `RunSummary/Detail`| `SessionScore` + iter-log walk over `IterationRecord` (harness/model/wave/mtime) |
| `IterationSummary` | `IterationRecord` + `PersistedScore` (sidecar; may be absent)  |
| `IterationDetail`  | above + `PersistedScore.Breakdown` + `IntegrityObservation[]` + `IterationObjectives` |
| `RubricDoc`        | `DefaultRubric()` (`Rubric` → `SignalSpec[]` + `ScoreBand[]`)  |
| `DashboardEvent`   | broker-emitted; payload is a thin key set, not a full DTO      |

The `dashboard.Store` interface (`t02`) returns these DTOs (or their Go structs); handlers (`t03`)
only wrap them in the envelope and set headers. Suggested Store surface (authoritative for `t02`):

```go
type Store interface {
    ListRuns(ctx context.Context, f RunFilter) ([]RunSummary, error)
    GetRun(ctx context.Context, sessionID string) (RunDetail, error)
    ListIterations(ctx context.Context, sessionID string) ([]IterationSummary, error)
    GetIteration(ctx context.Context, iterLogDir string, n int) (IterationDetail, error)
    Rubric(ctx context.Context) (RubricDoc, error)
    Health(ctx context.Context) (Health, error)
}
```

---

## 3. Endpoint reference

### 3.1 `GET /api/v1/observability/runs` — list runs (spec R1)

List every session discovered across resolved iter-log roots.

**Query params**

| param      | type   | default | notes                                                              |
|------------|--------|---------|--------------------------------------------------------------------|
| `limit`    | int    | 50      | 1–500; `bad_request` otherwise                                     |
| `offset`   | int    | 0       | ≥0                                                                 |
| `sort`     | enum   | `last_update` | one of `last_update`, `score`, `iteration_count`, `session_id` |
| `order`    | enum   | `desc`  | `asc` \| `desc`                                                   |
| `band`     | enum   | (none)  | filter to one band: `excellent`\|`good`\|`fair`\|`poor`\|`unscored` |
| `harness`  | string | (none)  | exact-match filter on harness                                     |

**Response** `200` — `data: RunSummary[]` (each per `dashboard-run`, `per_iteration` omitted),
`meta.count` = returned page length. Empty when no sessions (200, `[]`).

**Sort stability:** ties broken by `session_id` ascending for deterministic paging.

**Example**

```json
{
  "data": [
    {
      "session_id": "b1c2-…", "harness": "claude-code", "model": "claude-opus-4-8",
      "wave": "r2-observability-dashboard", "rubric_version": "2.1.0",
      "iteration_count": 12, "scored": true, "score": 0.81, "band": "good",
      "first_iteration": 1, "last_iteration": 12,
      "last_update": "2026-05-28T14:03:11Z",
      "iter_log_dir": ".agents/active/iteration-log",
      "mean_cache_hit_rate": 0.73
    }
  ],
  "meta": { "etag": "runs:1748441000:1", "count": 1 }
}
```

### 3.2 `GET /api/v1/observability/runs/{session_id}` — one run (spec R1)

**Path param** `session_id` (string, required).
**Response** `200` — `data: RunDetail` (includes `per_iteration`). `404 not_found` if the session
is in none of the resolved roots.

### 3.3 `GET /api/v1/observability/runs/{session_id}/iterations` — iteration list (spec R2)

Iterations for one run, ascending by `iteration`.

**Query params:** `limit`, `offset` (as §1.4). No band filter (the run is already scoped).
**Response** `200` — `data: IterationSummary[]` (per `dashboard-iteration`, detail-only fields
omitted). **A listed iteration whose `iter-N.score.yaml` sidecar is not yet written returns
`scored: false`, `score: null`, `band: "unscored"`, `rubric_version: ""`** — never an error (spec
R2, OQ2). `404 not_found` only if the `session_id` itself is unknown (an empty iteration list for a
known session is `200` with `[]`).

### 3.4 `GET /api/v1/observability/iterations/{n}` — one iteration, full detail (spec R3)

**Path param** `n` (integer ≥1). Non-integer → `bad_request`.
**Query param** `iter_log_dir` (string, optional) — disambiguate when `n` exists in more than one
resolved root (§1.6); defaults to the active root.
**Response** `200` — `data: IterationDetail` with `breakdown`, `integrity`, `objective`,
`verifiers`, `token_usage`, `transcript_turn_count` populated. If the iter-log entry exists but the
score sidecar is missing/stale, the handler MAY trigger recompute-on-miss (`t06`) and otherwise
returns `scored: false` / `score: null` with the non-score fields still populated. `404 not_found`
if no iter-log entry for `n` in the resolved root.

### 3.5 `GET /api/v1/observability/rubric` — active rubric (spec R4)

**No params.** Response `200` — `data: RubricDoc` per `dashboard-rubric`: `version`, `combination`,
`signals[]` (id, label, weight, description, two_way), `bands[]` (name, min). Sourced from
`DefaultRubric()` at process start; the etag is the rubric version, so it is effectively immutable
per process.

### 3.6 `GET /api/v1/observability/health` — liveness + counts (spec R9)

**No params.** Response `200` — `data` (inline, not a separate file schema):

```json
{
  "status": "ok",
  "run_count": 14,
  "iteration_count": 132,
  "last_iter_log_mtime": "2026-05-28T14:03:11Z",
  "subscriber_count": 2,
  "rubric_version": "2.1.0",
  "roots": [".agents/active/iteration-log"]
}
```

`status` is always `"ok"` when the process is serving (liveness). `last_iter_log_mtime` is null
when no roots contain any file. `subscriber_count` is the live SSE subscriber count from the
broker (`t04`). This endpoint never returns 5xx while the process is up.

### 3.7 `GET /api/v1/observability/events` — SSE channel (spec R5–R7, design.md Q3)

Long-lived `text/event-stream` response. Server→client push only (see §4 for why SSE not
WebSocket). The connection stays open; the server emits one SSE frame per event.

**Request headers:** standard `EventSource` (`Accept: text/event-stream`). No auth.
**Response headers:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
`Connection: keep-alive`, `X-Accel-Buffering: no` (defeat nginx buffering).
**On connect:** the server writes a ~2KB SSE comment (`: ` + padding) to flush proxy buffers, then
begins streaming.

**Frame format** (per event):

```
event: iteration.scored
id: 42
data: {"type":"iteration.scored","seq":42,"ts":"2026-05-28T14:03:11Z","payload":{"session_id":"b1c2-…","iteration":12,"band":"good"}}

```

- `event:` = the topic (`type`).
- `id:` = `seq`, a **monotonic per-connection** sequence (resets on reconnect — no durable replay).
- `data:` = one JSON object validating against `dashboard-event.schema.json` (single line).
- Frames are separated by a blank line.

**Heartbeat:** every 15s the server sends a bare comment line `: ping\n\n` (also valid as a typed
`heartbeat` event). Keeps the connection alive through idle-timeout proxies.

**Event taxonomy** (matches the broker topics in `t04` and the spec requirements):

| event (`type`)     | spec | payload (required keys)            | client action                                   |
|--------------------|------|------------------------------------|-------------------------------------------------|
| `iteration.scored` | R5   | `session_id`, `iteration` (+`band`)| invalidate `runs`, `runs/{sid}`, `iterations/{n}` |
| `score.recomputed` | —    | `session_id`, `iteration` (+`band`)| invalidate `iterations/{n}`                     |
| `session.updated`  | R6   | `session_id`                       | invalidate `runs`, `runs/{sid}`                 |
| `rubric.changed`   | R7   | `rubric_version`                   | full reload (rubric version bumped)             |
| `heartbeat`        | —    | `{}`                               | none (keepalive)                                |

**Ordering & replay (explicit, per spec D2.2 fallback):** events are delivered in publish order
within a single connection (`seq` strictly increasing). **There is NO server-side replay of missed
events in v1.** On disconnect the client reconnects (exponential backoff up to 30s, `t11`) and, on
reconnect, **invalidates all queries** to recover any events missed while disconnected (done-
criteria 4). `seq` is not a durable cursor and must not be sent back as a resume token.

**Backpressure (spec OQ5):** the broker (`t04`) holds one bounded buffered channel per subscriber.
A subscriber that cannot drain within a 1s grace is dropped; the client reconnects and refetches.
This is the "bounded buffer with disconnect-on-overflow" resolution of OQ5.

---

## 4. Why SSE, not WebSocket

The plan-level design (Q3) and spec (D2.2) both resolve the real-time mechanism to **Server-Sent
Events**, and this contract is built on that decision:

- **Traffic is unidirectional (server→client).** Every event here is a push notification; the
  client never sends data over the channel — it reacts by issuing ordinary `GET` requests. A
  WebSocket's bidirectional channel buys nothing in v1.
- **SSE rides the existing HTTP stack.** It is a long-lived `text/event-stream` response with no
  upgrade handshake, so it survives reverse proxies and load balancers more reliably than a WS
  upgrade, and it reuses the same `net/http` handler model as the REST endpoints (`t05` is just
  another handler).
- **Native browser `EventSource`** — no client library tax (`t11` wraps it in a thin reconnecting
  client).
- **Forward door:** if R5 later adds reviewer write-events, those ride their own `POST` endpoint;
  R2's channel stays unidirectional. WebSocket remains a deliberate future option, not a v1 need.

Rejected: WebSocket (bidirectional capability we don't use; heavier paths), long-poll with
watermark (spec D4 forbids polling), gRPC-web streaming (codegen surface for one channel).

---

## 5. Anti-scope (pinned)

- **No write endpoints, no mutation** — labels/comments/role-gated actions are R5.
- **No RBAC / authn** — R5; v1 binds loopback.
- **No pagination beyond `limit`/`offset`** — no cursors, no `next`/`prev` links.
- **No server-side event replay** — reconnect + refetch (§3.7).
- **No new persistent store** — read-through from iter-log + sidecars (design.md Q2); the `Store`
  interface is the forward door for a future denormalized backend.
- **No cost/dollar projection** in v1 (OQ4) — token + cache only; `mean_cache_hit_rate` is the
  trend axis the runs DTO exposes.
```
