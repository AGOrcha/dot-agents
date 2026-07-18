---
title: Dashboard API

description: HTTP and Server-Sent Events reference for the R2 observability dashboard.
---

# Dashboard API

The dashboard API is read-only. Its versioned base path is:

```text
/api/v1/observability
```

The authoritative route catalogue is
[`Mount.routes`](../internal/dashboard/handlers/handlers.go) in
`internal/dashboard/handlers/handlers.go`. The standalone-only liveness route is
registered by
[`internal/dashboard/server/server.go`](../internal/dashboard/server/server.go).
No prefix is stripped when the handler is mounted in either standalone or
R3-hosted mode.

## Schema sources

Four JSON Schemas exist and ground the response and event DTOs in this reference:

| DTO | Schema |
|---|---|
| Run summary/detail | [`schemas/dashboard-run.schema.json`](../schemas/dashboard-run.schema.json) |
| Iteration summary/detail | [`schemas/dashboard-iteration.schema.json`](../schemas/dashboard-iteration.schema.json) |
| Active rubric | [`schemas/dashboard-rubric.schema.json`](../schemas/dashboard-rubric.schema.json) |
| SSE event `data:` object | [`schemas/dashboard-event.schema.json`](../schemas/dashboard-event.schema.json) |

The Go DTO twins are in
[`internal/dashboard/store/dto.go`](../internal/dashboard/store/dto.go), and
contract tests validate representative handler responses against the shipped
schemas in
[`internal/dashboard/handlers/contract_test.go`](../internal/dashboard/handlers/contract_test.go).
There is no JSON Schema for the success envelope, error envelope, or health
payload; those portions below are derived directly from
[`respond.go`](../internal/dashboard/handlers/respond.go) and
[`store/dto.go`](../internal/dashboard/store/dto.go).

The schema descriptions contain a few historical unversioned path examples. The
registered paths in this document are authoritative because they are taken from
`Mount.routes` and its literal-prefix contract test.

## Common response conventions

Except for SSE and the standalone bare liveness probe, a successful response is
`application/json; charset=utf-8` with this envelope:

```json
{
  "data": {},
  "meta": {
    "etag": "resource:opaque-value",
    "count": 1
  }
}
```

`meta.count` appears only on list endpoints and is the number of items in the
returned page, not the total before pagination. Empty lists are `[]`, never
`null`. `meta.etag` is unquoted; the same value appears in the quoted HTTP
`ETag` header. Send `If-None-Match` to receive `304 Not Modified` with no body
when the representation has not changed. Weak tags, comma-separated candidates,
and `*` are accepted. The rubric ETag is the rubric version; other ETags are a
resource-keyed hash of the JSON `data`.

Handler-produced JSON errors use:

```json
{
  "error": {
    "code": "bad_request",
    "message": "invalid \"limit\": must be an integer between 1 and 500"
  }
}
```

| HTTP status | `error.code` | Meaning |
|---:|---|---|
| `400` | `bad_request` | Invalid query/path input, including an unconfigured `iter_log_dir`. |
| `404` | `not_found` | A requested run or iteration does not exist. |
| `500` | `internal` | Unexpected store or response failure, or a non-flushable SSE transport. |
| `503` | `internal` | The SSE route was composed without a broker. |

Unknown paths and disallowed methods are rejected by Go's `http.ServeMux` before
a dashboard handler runs, so its default 404/405 response is not the dashboard
JSON error envelope.

## Endpoint inventory

| Method | Path | Response `data` | Source |
|---|---|---|---|
| `GET` | `/api/health` | Bare `{"status":"ok"}`; standalone only | [`server.go`](../internal/dashboard/server/server.go) |
| `GET` | `/api/v1/observability/runs` | Run summary array | [`handlers.go`](../internal/dashboard/handlers/handlers.go) |
| `GET` | `/api/v1/observability/runs/{session_id}` | Run detail | [`handlers.go`](../internal/dashboard/handlers/handlers.go) |
| `GET` | `/api/v1/observability/runs/{session_id}/iterations` | Iteration summary array | [`handlers.go`](../internal/dashboard/handlers/handlers.go) |
| `GET` | `/api/v1/observability/iterations/{n}` | Iteration detail | [`handlers.go`](../internal/dashboard/handlers/handlers.go) |
| `GET` | `/api/v1/observability/rubric` | Active rubric | [`handlers.go`](../internal/dashboard/handlers/handlers.go) |
| `GET` | `/api/v1/observability/health` | Rich dashboard health | [`handlers.go`](../internal/dashboard/handlers/handlers.go) |
| `GET` | `/api/v1/observability/events` | `text/event-stream` | [`handlers.go`](../internal/dashboard/handlers/handlers.go), [`stream.go`](../internal/dashboard/handlers/stream.go) |

Non-`/api` paths are not API endpoints. In standalone mode they serve the SPA or
are proxied to Vite; in R3-hosted mode the host owns them.

## `GET /api/health`

Standalone process liveness. This route does not read the store and is not
registered by the R3 dashboard mount.

**Response:** `200 application/json`

```json
{"status":"ok"}
```

This response is bare JSON: it has no `data`/`meta` envelope or ETag.

## `GET /api/v1/observability/runs`

Returns one run summary per addressable session discovered across all configured
roots. The data array conforms to
[`dashboard-run.schema.json`](../schemas/dashboard-run.schema.json), with
`per_iteration` omitted from summary rows.

Query parameters are parsed by
[`internal/dashboard/handlers/params.go`](../internal/dashboard/handlers/params.go):

| Parameter | Default | Constraint |
|---|---:|---|
| `limit` | `50` | Integer from 1 through 500. |
| `offset` | `0` | Integer greater than or equal to 0. |
| `sort` | `last_update` | `last_update`, `score`, `iteration_count`, or `session_id`. |
| `order` | `desc` | `asc` or `desc`. |
| `band` | unset | `excellent`, `good`, `fair`, `poor`, or `unscored`. |
| `harness` | unset | Free-form, exact string match. |

Unknown query parameters are ignored. An offset beyond the result set returns a
`200` envelope with `data: []` and `meta.count: 0`.

Run properties are defined by the schema. Required core fields are `session_id`,
`rubric_version`, `iteration_count`, `scored`, `score`, and `band`; the current Go
DTO also emits harness/model/wave metadata, iteration bounds, last update,
`iter_log_dir`, and `mean_cache_hit_rate`.

**Responses:** `200`, `304`, `400 bad_request`, `500 internal`.

## `GET /api/v1/observability/runs/{session_id}`

Returns one run detail conforming to
[`dashboard-run.schema.json`](../schemas/dashboard-run.schema.json). Unlike the
summary endpoint, detail includes `per_iteration`, an array of
`{iteration, scored, score, band}` references.

The session ID is looked up exactly across configured roots. Session IDs are
expected to be globally unique; if the same ID occurs in more than one root, the
store keeps the first configured root and logs a warning.

**Responses:** `200`, `304`, `404 not_found`, `500 internal`.

## `GET /api/v1/observability/runs/{session_id}/iterations`

Returns that session's iterations in ascending iteration order. Rows conform to
[`dashboard-iteration.schema.json`](../schemas/dashboard-iteration.schema.json),
with detail-only `verifiers`, `breakdown`, `integrity`, and `objective` omitted.
An iteration without a score sidecar is represented as `scored: false`,
`score: null`, and `band: "unscored"` rather than an error.

| Parameter | Default | Constraint |
|---|---:|---|
| `limit` | `50` | Integer from 1 through 500. |
| `offset` | `0` | Integer greater than or equal to 0. |

An offset beyond the session returns an empty `200` page.

**Responses:** `200`, `304`, `400 bad_request`, `404 not_found`, `500 internal`.

## `GET /api/v1/observability/iterations/{n}`

Returns one iteration detail conforming to
[`dashboard-iteration.schema.json`](../schemas/dashboard-iteration.schema.json).
`n` must be an integer greater than or equal to 1.

| Parameter | Default | Constraint |
|---|---:|---|
| `iter_log_dir` | first configured root | If present, must equal a configured root after path normalization. |

The detail shape includes checkpoint metadata and token usage plus, when
available, `verifiers`, per-signal `breakdown`, claimed-vs-observed `integrity`,
and transcript-derived `objective` checks. Consult the schema for the complete
nested property constraints and signal enums.

The production store uses recompute-on-miss: a missing, corrupt, or older score
sidecar triggers synchronous scoring for this detail request. If recomputation
fails, the valid raw iteration still returns; successful sidecar persistence is
best effort in the background. This behavior is defined in
[`internal/dashboard/store/recompute.go`](../internal/dashboard/store/recompute.go).

**Responses:** `200`, `304`, `400 bad_request`, `404 not_found`, `500 internal`.

## `GET /api/v1/observability/rubric`

Returns the active rubric conforming to
[`dashboard-rubric.schema.json`](../schemas/dashboard-rubric.schema.json):

- `version`
- `combination` (`weighted_mean_renormalized`)
- ordered `signals` with `id`, `label`, `weight`, `description`, and `two_way`
- descending `bands` with `name` and inclusive `min`

The HTTP and envelope ETag is exactly the rubric `version`.

**Responses:** `200`, `304`, `500 internal`.

## `GET /api/v1/observability/health`

Returns rich operator state in the normal success envelope. There is no separate
health JSON Schema; the shape comes from `store.Health` in
[`internal/dashboard/store/dto.go`](../internal/dashboard/store/dto.go):

| Field | Type | Meaning |
|---|---|---|
| `status` | string | `"ok"` while the process is serving. |
| `run_count` | integer | Addressable sessions across configured roots. |
| `iteration_count` | integer | Parsed iteration records across configured roots. |
| `last_iter_log_mtime` | string or null | Newest contributing file mtime in RFC3339 UTC. |
| `subscriber_count` | integer | Currently connected dashboard SSE clients. |
| `rubric_version` | string | Active scoring rubric version. |
| `roots` | string array | Configured iter-log roots in priority order. |

The handler deliberately does not return a 5xx for a store health failure. It
logs the failure and degrades to an enveloped liveness payload with `status:
"ok"` and `roots: []`.

**Responses:** `200`, `304`.

## `GET /api/v1/observability/events`

Opens the server-to-client push channel. The handler and frame format are defined
in [`internal/dashboard/handlers/stream.go`](../internal/dashboard/handlers/stream.go).

**Successful response headers:**

```text
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

On connection, the server first flushes a roughly 2 KiB SSE comment to defeat
intermediary buffering. Each subsequent event is a frame:

```text
event: iteration.scored
id: 0
data: {"type":"iteration.scored","seq":0,"ts":"2026-07-17T12:00:00Z","payload":{"session_id":"sess-1","iteration":4,"band":"good"}}

```

The `event:` value mirrors `data.type`; `id:` mirrors `data.seq`. The Go event
struct always emits `type`, `seq`, `ts`, and `payload`. `ts` is whole-second UTC
RFC3339. Sequence numbers are contiguous per live connection, begin at 0, and
reset after reconnect. `Last-Event-ID` is not a replay cursor: the server keeps
no event history, so clients must refetch after reconnecting.

The event object and payload alternatives are grounded in
[`dashboard-event.schema.json`](../schemas/dashboard-event.schema.json). The
registered taxonomy constants are in
[`internal/dashboard/events/broker.go`](../internal/dashboard/events/broker.go):

| SSE event / `data.type` | Payload | Meaning and producer source |
|---|---|---|
| `iteration.scored` | `{"session_id": string, "iteration": integer, "band"?: band}` | New iteration/score data. Standalone file mapping: [`watch.go`](../internal/dashboard/watch/watch.go). R3 `iteration.scored` translation: [`r3bridge.go`](../internal/dashboard/events/r3bridge.go). |
| `session.updated` | `{"session_id": string}` | A session score sidecar changed. Produced by the standalone watcher in [`watch.go`](../internal/dashboard/watch/watch.go). |
| `score.recomputed` | Same as `iteration.scored` | An existing iteration score sidecar changed in place. Produced by the standalone watcher in [`watch.go`](../internal/dashboard/watch/watch.go). |
| `rubric.changed` | `{"rubric_version": string}` | A new rubric version requires a full client reload. R3 translates `rescore.done` in [`r3bridge.go`](../internal/dashboard/events/r3bridge.go). |
| `heartbeat` | `{}` | Broker keepalive every 15 seconds; defined in [`broker.go`](../internal/dashboard/events/broker.go). |

The optional `band` enum is `excellent`, `good`, `fair`, `poor`, or `unscored`.
The schema requires non-empty `session_id` values. The current standalone and R3
bridges resolve the session ID from `iter-N.yaml` on a best-effort basis and can
emit an empty string when the record is missing or unparseable; that
implementation edge is explicit in `watch.go` and `r3bridge.go` rather than being
silently broadened in this reference.

The broker invalidates cached store state before sending a data event. Each
subscriber has a bounded buffer; the first overflow closes that stream. There is
no silent gap followed by later delivery and no server-side replay. A client
should reconnect and refetch affected HTTP queries.

**Responses:** `200` streaming, `503 internal` when no broker is composed, `500
internal` when the HTTP transport cannot flush.
