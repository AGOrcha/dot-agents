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

## Decision: an OpenAPI 3.1 spec per resource-family

The prose above governs **structure** (paths, verbs, envelope, versioning). The **machine-readable
contract** for each domain is a versioned **OpenAPI 3.1 spec**, one per resource-family:

```
schemas/openapi/<domain>.v1.yaml
```

(under the repo's existing `schemas/` tree — see `[[schema-usage]]`; the sibling of
`schemas/agentsrc.schema.json` and the `workflow-*.schema.json` set.) One file per domain —
`workflow`, `review`, `kg`, `observability`, `config`, `registry` — each describing that family's
paths, payloads, and status codes.

- **One spec PER family, all at the same `/api/v1/` envelope.** The `.v1` in the filename tracks
  the single product version, not a per-domain version — consistent with the version-first decision
  (per-domain versioning stays rejected). When the product bumps to `v2`, every family's spec moves
  to `<domain>.v2.yaml` together.
- **The OpenAPI doc is the source of truth for paths, payloads, and status codes.** Where this prose
  convention and a spec disagree on structure, the prose wins (it is the cross-cutting rule); within
  that structure, the OpenAPI file is the authoritative shape a consumer codes against.
- **All consumers validate against it:** the CLI, the MCP server, external-agent-sources, and any
  generated clients. A surface's own contract doc (e.g. `design/API.md`) prose-describes intent; the
  OpenAPI spec is what tests and generators consume.

This makes the per-surface contracts (`[[r2-observability-dashboard]]` etc.) accountable to a
checkable artifact rather than prose alone, and keeps `[[api-conventions]]` the governing rule with
the OpenAPI files as its enforced instances.

---

## Decision: auto-sync OpenAPI ↔ published web docs

The published API reference on the agorcha.dev docs site (the canonical Astro docs) MUST be
**generated from** `schemas/openapi/*.yaml`, never hand-authored — so the published reference can
never drift from the contract. **Reuse the existing schema-sync wheel; do not build a parallel
pipeline.**

- **Ride the existing `docs/web/scripts/copy-schemas.sh` flow.** That prebuild hook already copies
  repo-root `schemas/*.json` into `docs/web/public/schemas/` (served at `agorcha.dev/schemas/...`)
  on every `npm run build`, and the agorcha Cloudflare build picks them up. The OpenAPI specs live
  in the same `schemas/` tree (`schemas/openapi/<domain>.v1.yaml`) and ride the same wheel — the
  required change is a **one-line glob extension** so `copy-schemas.sh` also copies `openapi/*.yaml`
  (e.g. add `"${SRC}"/openapi/*.yaml` to the `schemas=(…)` glob) alongside the existing `*.json`.
  The "auto-sync with web docs" requirement is satisfied by that existing copy-on-build pipeline
  plus a render step over the copied specs — not a new build.
- **Render step over the copied specs, with a pluggable renderer.** An Astro integration renders the
  copied `schemas/openapi/*.yaml` into the API reference pages. **Default to Scalar**
  (scalar.com — modern, OpenAPI-3.1-native, good static/Astro integration, open-source). Keep the
  renderer **pluggable via build config**: **Swagger UI** selectable as the classic alternative, and
  **Redoc** as an optional clean three-panel middle-ground. Scalar is the default; Swagger/Redoc are
  per-deployment opt-in toggles, never hard-wired.
- **Regeneration is tied to the docs deploy** — the agorcha Cloudflare Worker build (see
  `[[agorcha-public-vs-internal-and-obs-deploy]]`) — because it runs through the same prebuild hook.
  Every API change that lands a spec edit regenerates the published reference on the next deploy;
  there is no manual docs-update step to forget.
- This is the **API analogue of the release-task docs-accuracy pass** (`[[release-gated-plans-convention]]`):
  just as a release verifies docs match shipped behavior, the contract and its published docs stay
  in lockstep automatically — the spec is edited once and the reference follows.

Cross-ref the schema-sync wheel (`docs/web/scripts/copy-schemas.sh`), the docs-site work
(`[[docs-site-usability-review]]`), and the agorcha deploy (`[[agorcha-public-vs-internal-and-obs-deploy]]`).

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
- `[[schema-usage]]` — the `schemas/` tree that hosts `schemas/openapi/<domain>.v1.yaml`, the
  machine-readable per-family contract.
- `[[docs-site-usability-review]]` / `[[agorcha-public-vs-internal-and-obs-deploy]]` — the docs site
  that auto-renders the OpenAPI specs (via the existing `docs/web/scripts/copy-schemas.sh` prebuild
  wheel, glob extended to `openapi/*.yaml`; Scalar default renderer, Swagger UI/Redoc opt-in) and the
  Cloudflare Worker build that regenerates the published API reference on every deploy.
- `[[release-gated-plans-convention]]` — the release docs-accuracy pass this OpenAPI↔docs auto-sync
  is the API analogue of.

## Published API reference URL (agorcha public surface)

The Scalar-rendered OpenAPI reference (per the OpenAPI-per-family + docs-autosync
conventions above) is published at a **public** agorcha route:

```
agorcha.dev/api/                  PUBLIC   (rendered API reference — Scalar over schemas/openapi/*.yaml)
```

This is the rendered *reference documentation*, served by the docs Worker — it hosts
no live endpoints, so it does not collide with the live API contract paths
(`/api/v1/<domain>/<resource>`, which are served by the service/obs hosts, e.g. the
gated `obs.agorcha.dev/api/*`). It sits alongside the existing public routes
(`/`, `/demos/*`, `/canonical/*`, `/schemas/*`) and rebuilds on every docs deploy via
the extended `copy-schemas.sh` wheel. Add this row to the agorcha URL structure
(`[[agorcha-public-vs-internal-and-obs-deploy]]` "Concrete URL shape"): `/api/` PUBLIC,
no CF Access (same posture as `/schemas/*` — reference material is public).
