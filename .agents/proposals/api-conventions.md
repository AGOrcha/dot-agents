# HTTP API conventions — `/api/v1/<domain>/<resource>`

**Status:** project-local convention (ratified by maintainer ruling 2026-05-28).
**Routing rationale:** governs dot-agents-local HTTP surfaces (the daemon/service, the obs
dashboard, the agorcha obs server, the config/registry endpoints) per `[[proposal-routing]]` —
project scope, not a shared `~/.agents/` resource.
**Scope:** a cross-cutting convention **every** dot-agents HTTP surface follows. First consumers:
the r2 observability dashboard, the r3 `da service` host, the agorcha obs server, the future
config/package registry, the daemon auth proxy, and external-agent-sources read endpoints.

This is a convention doc — decisions and the rules they imply, not an implementation plan. Each
HTTP surface keeps its own contract doc (e.g. `[[r2-observability-dashboard]]`'s `design/API.md`);
those contracts MUST conform to the path structure, verb, envelope, and versioning rules below.

---

## Decision: `/api/v1/<domain>/<resource>`

All HTTP routes follow one shape:

```
/api/v1/<domain>/<resource>[/{id}[/<subresource>]]
```

- **`v1` is version-first, at the root** — one `/api/v1/` envelope for the whole product. The
  version segment is **always present in the URL** (no elision). One contract evolves; one number
  bumps.
- **`<domain>` is a namespace under the version** — a stable bounded context, not a version:
  `workflow`, `review`, `kg`, `observability`, `config`, `registry`.
- **`<resource>` is a plural-noun collection.** Instances are addressed by `{id}` path params.
  Sub-collections nest under an instance.

### Canonical examples

| Surface        | Path                                          | Meaning                          |
|----------------|-----------------------------------------------|----------------------------------|
| workflow       | `GET /api/v1/workflow/runs`                    | list workflow runs               |
| workflow       | `GET /api/v1/workflow/runs/{id}/iterations`    | iterations of a run              |
| review         | `GET /api/v1/review/runs/{id}/labels`          | labels on a review run           |
| review         | `GET /api/v1/review/users`                      | review users                     |
| kg             | `GET /api/v1/kg/notes`                          | list KG notes                    |
| kg             | `GET /api/v1/kg/notes/{id}`                     | one KG note                      |
| observability  | `GET /api/v1/observability/events`             | event stream (read-only)         |

---

## Rationale — why version-first, not per-domain

Per-domain versioning (`/api/workflow/v2/...` next to `/api/review/v1/...`) puts an independent
version on every namespace. That produces a **combinatorial drift matrix**: each domain advances
on its own clock, and every consumer — the dashboard, the CLI, the MCP server,
external-agent-sources, the public agorcha API — must track which version each domain is on and
negotiate them independently. N domains × M versions is N×M contracts to test and document.

One `/api/v1/` envelope collapses that to a single product version: **one contract to evolve, one
number to bump, one compatibility story for every consumer.** Adding a domain is additive under the
existing version; it does not fork the version space.

**Per-domain versioning is rejected.** Do not introduce `/api/<domain>/v{n}/...`. The version
segment lives at the root and nowhere else.

---

## REST verb conventions

| Verb     | Use                                    | Idempotent |
|----------|----------------------------------------|------------|
| `GET`    | list a collection / read an instance   | yes        |
| `POST`   | create in a collection                 | no         |
| `PATCH`  | partial update of an instance          | no         |
| `PUT`    | full replace of an instance            | yes        |
| `DELETE` | remove an instance                     | yes        |

- **Collections are plural nouns** (`/runs`, `/notes`, `/users`). No verbs in paths; the HTTP
  method is the verb.
- **Instances** are `/{collection}/{id}`; sub-collections nest (`/runs/{id}/iterations`).
- **Status codes:** `200` read/update OK, `201` created (with `Location`), `204` no content
  (delete), `400` bad request, `401`/`403` auth, `404` not found, `409` conflict, `500` server
  error. A missing optional value degrades to `null`, not `500`.
- **List query params:** `limit`/`offset` for paging (a surface MAY add opaque cursor params where
  scale demands it — document the choice in that surface's contract). Filtering/sorting via
  documented query params (`sort`, `order`, domain-specific filters).
- **Error envelope:** one shape across all surfaces — either a documented
  `{ "error": { "code": "<machine_token>", "message": "<human>" } }` or RFC 7807 `problem+json`.
  A surface picks one and documents it; `code` is stable, `message` may change.
  `Content-Type: application/json; charset=utf-8` on every JSON response (success and error).

---

## Event stream — `/api/v1/<domain>/events`

Every surface that emits a real-time channel exposes it at `/api/v1/<domain>/events` (e.g.
`/api/v1/observability/events`). The **path shape is fixed; the transport is chosen
per-deployment-scope:**

- **SSE for local single-user, read-only** streams. Server→client push over a long-lived
  `text/event-stream` response, riding the existing HTTP stack with native browser `EventSource`.
  This is r2's resolved decision for the loopback dashboard.
- **WebSocket for multi-node remote** deployments, per the merged agorcha-arch §4.1
  Durable-Object-Hibernation decision (see `[[agorcha-public-vs-internal-and-obs-deploy]]`). The
  hibernatable WS connection is the fan-out primitive for the multi-tenant obs server.

Same path, two transports keyed to deployment scope — a loopback build serves SSE at
`/api/v1/observability/events`; the remote multi-node build serves WS at the same path. Events are
a **versioned resource** carried by the registry-driven envelope of
`[[unified-pluggable-event-contract]]` — adding an event type is additive (a new registry entry),
not a version bump.

---

## Versioning policy

- **Bump `v1`→`v2` only on a breaking change:** changing a response shape incompatibly (renaming
  or removing a field, changing a type), removing an endpoint, or changing a path's semantics.
- **Additive changes do NOT bump:** new endpoints, new domains, new optional response fields, new
  optional query params, new event types. Consumers must tolerate unknown fields.
- **Deprecation:** announce a removed/changed endpoint with `Deprecation` and `Sunset` response
  headers (RFC 8594) and a documented deprecation window before the breaking version ships. The
  old version stays served through the window.

---

## Auth

Auth is referenced, not re-specified here:

- The read-credential model for ingest/read consumers follows external-agent-sources (see
  `[[config-distribution-model]]` / `[[org-config-resolution]]`).
- The daemon **auth-proxy** direction (a single front door terminating auth ahead of the domain
  handlers) is the planned multi-node posture per `[[agorcha-public-vs-internal-and-obs-deploy]]`
  (public/internal split + scopes). Loopback single-user builds may bind `127.0.0.1` with no
  authn; remote builds sit behind the auth proxy.

Each surface states its concrete auth posture in its own contract; this convention only fixes that
auth terminates **at or ahead of** the `/api/v1/` envelope, never per-domain.

---

## Relationship to other specs

- `[[r2-observability-dashboard]]` — first consumer; its `design/API.md` adopts
  `/api/v1/observability/<resource>` and SSE at `/api/v1/observability/events`.
- `[[r3-background-worker-service]]` — the HTTP host (`da service`) that mounts the `/api/v1/`
  router and the per-domain handler groups.
- `[[agorcha-public-vs-internal-and-obs-deploy]]` — the public/internal split, scopes, and the
  multi-node WS transport + auth-proxy direction.
- `[[unified-pluggable-event-contract]]` — the registry-driven, version-additive envelope every
  `/api/v1/<domain>/events` channel carries.
