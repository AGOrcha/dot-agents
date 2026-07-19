# Cloudflare observability dashboard deployment and provider contracts

**Spec ID:** `obs-dashboard-cf-deploy`
**Status:** accepted contract for plan `obs-dashboard-cf-deploy`
**Canonical root:** `.agents/workflow/specs/obs-dashboard-cf-deploy/`

## Problem

The existing observability dashboard is a local, loopback-only SPA backed by iteration-log and score-sidecar files. The remote deployment has no access to that filesystem, needs authenticated ingest, durable history, and Cloudflare-native live delivery, but must still run the same SPA and the same versioned dashboard API. Without provider contracts for credentials, D1, the offline outbox, the live transport, and local authentication, the downstream tasks would make incompatible decisions.

This spec promotes the Cloudflare deployment design from `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` §§3.4–6 and closes the five contract gaps enumerated by `.agents/workflow/plans/obs-dashboard-cf-deploy/TASKS.yaml` task `o1-obs-deploy-spec-and-contracts`.

## Goals

- Deploy the existing dashboard SPA at `https://obs.agorcha.dev` without forking its DTOs, query keys, or versioned routes.
- Keep local workflow history canonical and make remote DO/D1 state rebuildable by idempotent push and full sync.
- Keep outbound credentials in the shared credential store and make a Cloudflare Access service-token pair one atomic credential.
- Keep the wire protocol and provider seams generic enough for a repository to point at its own self-hosted backend.
- Fail closed: Cloudflare Access authenticates browser sessions and service-token pairs; the Worker validates only the resulting Access-issued JWT and rejects foreign-project writes.
- Make local and unit-test authentication cryptographically real but hermetic: signed fixture JWTs, no production secret, and no credential-bearing HTTP exception.

## Decisions

### D1 — Single tenant, client-side per-project routing

`obs.agorcha.dev` is the reference instance for **only** `github.com/AGOrcha/dot-agents`. It is not a shared multi-project router. Its Worker has an immutable deployment variable `OBS_PROJECT_ID=github.com/AGOrcha/dot-agents`; every ingest item whose `project_id` differs is rejected as `foreign_project` before a DO lookup or D1 write. The value is the canonical `<lowercase-host>/<path-without-.git>` form produced by `internal/gitremote/gitremote.go`.

Every repository selects its backend with its own `.agentsrc.json` `observability.endpoint`. A payout repository therefore points at payout's backend, not at `obs.agorcha.dev`. The ingest/API protocol and the contracts below remain generic and self-hostable; the deployment is single-tenant, not the protocol.

**Grounding:** `.agents/workflow/plans/obs-dashboard-cf-deploy/TASKS.yaml` lines 14–18 (maintainer correction); `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` §§6.1–6.2 (local canonical state and `project_id`); `internal/gitremote/gitremote.go` (canonical form).

### D2 — Credential-ref pair contract

#### Repository configuration

The exact committed shape is:

```json
{
  "observability": {
    "enabled": true,
    "endpoint": "https://obs.agorcha.dev",
    "push_throttle_seconds": 0,
    "auth": {
      "kind": "credential-ref",
      "id": "agorcha-obs"
    }
  }
}
```

`observability.auth` has `additionalProperties: false` and requires exactly two non-empty strings: `kind`, whose only value is `credential-ref`, and `id`, matching `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. An enabled non-loopback endpoint requires `auth`. A credential-ref endpoint must be absolute `https:`; the client checks the parsed scheme **before** constructing or calling the credential loader.

#### One id resolves one atomic credential

The shared `credstore.Loader.Resolve(id)` currently returns one string, and the encrypted store currently persists an `id -> string` map. Therefore `agorcha-obs` resolves to one compact JSON string, not two ids:

```json
{
  "kind": "cf-access-service-token",
  "client_id": "<UUID>.access",
  "client_secret": "<secret>"
}
```

The `kind` value is exactly `cf-access-service-token`. The parser is strict (`additionalProperties: false`); both `client_id` and `client_secret` are required, non-empty strings. The pair is usable only after the whole object validates. A partial pair is a hard credential error and attaches no header. Login serializes the complete object, calls one `Store.Set(id, serializedObject)`, then one atomic `Store.Save()`; readers can therefore observe the old pair or the new pair, never half of either. Environment/CI resolution uses the same JSON string in `DA_CREDENTIAL_AGORCHA_OBS` or as the value for `agorcha-obs` in `DA_CREDENTIALS_FILE`.

`~/.config/da/credentials.json` is the encrypted, versioned credstore envelope, not a plaintext `{credentials:{...}}` document. It is written mode `0600` by same-directory temp-file plus rename. The resolved plaintext object above exists only after loader resolution. No secret is written to `.agentsrc.json`, AGENTS_HOME, an outbox file, or a log.

For the reference provider, the client maps the resolved object to the two outbound headers `Cf-Access-Client-Id` and `Cf-Access-Client-Secret`. Those headers are sent only to the configured HTTPS origin. Cloudflare Access validates the pair and injects `Cf-Access-Jwt-Assertion`; the Worker never compares, stores, logs, or otherwise validates the raw pair.

#### Self-hosted providers through the same reference shape

The committed auth block remains `{ "kind": "credential-ref", "id": "..." }`. The object resolved by that one id is a strict discriminated union:

```ts
type ObservabilityCredential =
  | { kind: "cf-access-service-token"; client_id: string; client_secret: string }
  | { kind: "bearer"; token: string }
  | {
      kind: "oauth2-auth-code-pkce";
      access_token: string;
      refresh_token?: string;
      expires_at?: string; // RFC 3339 UTC
    }
  | {
      kind: "mtls";
      cert_pem: string;
      key_pem: string;
      ca_pem?: string;
      server_name?: string;
    };
```

`bearer` and `oauth2-auth-code-pkce` attach `Authorization: Bearer <token>` over HTTPS; the OAuth provider owns refresh before attachment. `mtls` builds the HTTPS client's certificate/key pair and optional trust roots before connecting. Every variant is one JSON string under one credential id, so refresh/rotation replaces the whole credential atomically. Provider mismatch is a hard error; there is no fallback to another kind.

The external-agent-sources spec **does not define `credential-ref` or a resolved secret schema**. It defines only the provider names and roles (`oauth2-auth-code-pkce`, `mtls`, `bearer`, `credential-helper`, device code). `schemas/agentsrc.schema.json` also leaves `sources[].auth` opaque. This spec therefore owns the minimal reusable `credential-ref` shape and the observability resolved-credential union until external-agent-sources explicitly adopts them; the shared credstore remains the implementation owner of id resolution and storage.

The dm6 docs flow is not reused. It has a docs-specific `/internal/provision` route and stores the pair under two separate ids, `cf-access-client-id` and `cf-access-client-secret`; its spec calls observability only a sibling pattern. Observability neither calls that provisioner nor claims its two-id layout. A future shared provisioner requires an explicit contract change.

**Grounding:** `.agents/workflow/specs/external-agent-sources/design.md` §§3–4 (shared auth/provider model, but no credential-ref); `schemas/agentsrc.schema.json` `sources[].auth` (opaque pass-through); `internal/credstore/loader.go` (`Resolve(id) string` and resolution order); `internal/credstore/store.go` (encrypted `map[string]string`, 0600 atomic save); `internal/docsaccess/client.go` (dm6's two fixed ids and HTTPS guard); `.agents/workflow/specs/dm6-da-sso-autowire/design.md` §§1, 11, 13 (docs-only provisioner and sibling status); `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` §5.4.

### D3 — Ingest, idempotency, DO, and D1 contract

#### Versioned ingest envelope

The write route is `POST /api/v1/observability/ingest`. The proposal's earlier `/api/ingest` spelling and the plan notes that repeat it are superseded: no new unversioned route is introduced beside the shared SPA's `/api/v1/observability/*` contract.

```json
{
  "schema_version": 1,
  "project_id": "github.com/AGOrcha/dot-agents",
  "client": {
    "da_version": "0.4.0",
    "host_os": "darwin",
    "agent_runtime": "claude-code"
  },
  "events": [
    {
      "kind": "iteration.scored",
      "occurred_at": "2026-05-28T19:43:00Z",
      "plan_id": "obs-dashboard-cf-deploy",
      "task_id": "o1-obs-deploy-spec-and-contracts",
      "iteration": 12,
      "schema_hash": "<64 lowercase hex characters>",
      "payload": {},
      "score_sidecar": {}
    }
  ]
}
```

`payload` must validate against `schemas/workflow-iter-log.schema.json`. Its `wave`, `task_id`, and `iteration` must equal the event's `plan_id`, `task_id`, and `iteration`. `schema_hash` is lowercase SHA-256 of RFC 8785 canonical JSON for exactly `{kind,occurred_at,plan_id,task_id,iteration,payload,score_sidecar}`, with a JSON `null` score sidecar included when absent. This content fingerprint makes the required `(project_id, plan_id, task_id, iteration, schema_hash)` tuple both an idempotency key and a version key when the same logical iteration is checkpointed or rescored.

V1 accepted input kinds are:

- `iteration.checkpointed`: requires `payload`; requires `score_sidecar: null`.
- `iteration.scored`: requires `payload` and a score sidecar matching `internal/scoring.PersistedScore`.
- `score.recomputed`: requires `payload` and a score sidecar; the logical iteration must already exist, and the new `schema_hash` must differ from the winning score version.

`session.updated`, `rubric.changed`, and `heartbeat` are live-output kinds only. An accepted checkpoint produces `session.updated`; an accepted first score produces `iteration.scored` then `session.updated`; an accepted recompute produces `score.recomputed` then `session.updated`. A rubric migration produces `rubric.changed`; the DO produces `heartbeat` every 15 seconds while sockets exist.

Unknown ingest `schema_version` and unknown input `kind` are permanent per-item rejections in v1. The server neither guesses nor stores raw unknown data.

The response is always item-addressable:

```json
{
  "accepted": 1,
  "deduped": 0,
  "rejected": [
    {
      "index": 2,
      "key": {
        "project_id": "...",
        "plan_id": "...",
        "task_id": "...",
        "iteration": 12,
        "schema_hash": "..."
      },
      "code": "foreign_project",
      "message": "event project does not match this deployment",
      "retryable": false
    }
  ]
}
```

A syntactically valid batch returns 200 even with per-item rejections. A malformed top-level body returns 400. `accepted + deduped + rejected.length` equals the input event count. The stable rejection codes are `unsupported_schema`, `unsupported_kind`, `invalid_event`, `foreign_project`, and `storage_unavailable`; only `storage_unavailable` is retryable.

The Worker uses `idFromName(project_id)` and sends each validated event to that project's DO. The DO serializes a project's writes. It commits the idempotency check, append rows, child rows, and session upsert as one D1 transaction; only after that commit succeeds does it update its last-100-event DO buffer and broadcast. There is no claimed cross-store transaction. The reference deployment rejects a foreign project before `idFromName`, so it can reach only one DO namespace member.

#### D1 migration DDL

The following is the normative first migration. D1 uses SQLite strict typing; all writes execute with foreign keys enabled.

```sql
PRAGMA foreign_keys = ON;

CREATE TABLE iterations (
  project_id                  TEXT    NOT NULL,
  plan_id                     TEXT    NOT NULL,
  task_id                     TEXT    NOT NULL,
  iteration                   INTEGER NOT NULL CHECK (iteration >= 1),
  schema_hash                 TEXT    NOT NULL
    CHECK (length(schema_hash) = 64 AND schema_hash NOT GLOB '*[^0-9a-f]*'),
  event_kind                  TEXT    NOT NULL
    CHECK (event_kind IN ('iteration.checkpointed', 'iteration.scored', 'score.recomputed')),
  ingest_schema_version       INTEGER NOT NULL CHECK (ingest_schema_version = 1),
  iteration_schema_version    INTEGER NOT NULL CHECK (iteration_schema_version >= 1),
  occurred_at                 TEXT    NOT NULL,
  ingested_at                 TEXT    NOT NULL,
  session_id                  TEXT    NOT NULL,
  date                        TEXT    NOT NULL,
  wave                        TEXT    NOT NULL,
  commit_sha                  TEXT    NOT NULL DEFAULT '',
  harness                     TEXT    NOT NULL DEFAULT '',
  model                       TEXT    NOT NULL DEFAULT '',
  files_changed               INTEGER NOT NULL DEFAULT 0 CHECK (files_changed >= 0),
  lines_added                 INTEGER NOT NULL DEFAULT 0 CHECK (lines_added >= 0),
  lines_removed               INTEGER NOT NULL DEFAULT 0 CHECK (lines_removed >= 0),
  retries                     INTEGER NOT NULL DEFAULT 0 CHECK (retries >= 0),
  integrity_observation_count INTEGER NOT NULL DEFAULT 0 CHECK (integrity_observation_count >= 0),
  transcript_turn_count       INTEGER CHECK (transcript_turn_count IS NULL OR transcript_turn_count >= 0),
  input_tokens                INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
  output_tokens               INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
  cache_read_tokens           INTEGER CHECK (cache_read_tokens IS NULL OR cache_read_tokens >= 0),
  cache_creation_tokens       INTEGER CHECK (cache_creation_tokens IS NULL OR cache_creation_tokens >= 0),
  cache_hit_rate              REAL    CHECK (cache_hit_rate IS NULL OR cache_hit_rate BETWEEN 0.0 AND 1.0),
  verifiers_json              TEXT    NOT NULL DEFAULT '[]'
    CHECK (json_valid(verifiers_json) AND json_type(verifiers_json) = 'array'),
  integrity_json              TEXT
    CHECK (integrity_json IS NULL OR (json_valid(integrity_json) AND json_type(integrity_json) = 'array')),
  objective_json              TEXT
    CHECK (objective_json IS NULL OR (json_valid(objective_json) AND json_type(objective_json) = 'object')),
  payload_json                TEXT    NOT NULL
    CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
  client_json                 TEXT    NOT NULL
    CHECK (json_valid(client_json) AND json_type(client_json) = 'object'),
  PRIMARY KEY (project_id, plan_id, task_id, iteration, schema_hash)
) WITHOUT ROWID;

CREATE INDEX iterations_by_project_session
  ON iterations (project_id, session_id, occurred_at DESC, iteration DESC);
CREATE INDEX iterations_by_project_logical_iteration
  ON iterations (project_id, plan_id, task_id, iteration, occurred_at DESC, ingested_at DESC);

CREATE TABLE scores (
  project_id       TEXT    NOT NULL,
  plan_id          TEXT    NOT NULL,
  task_id          TEXT    NOT NULL,
  iteration        INTEGER NOT NULL,
  schema_hash      TEXT    NOT NULL,
  rubric_version   TEXT    NOT NULL,
  scored           INTEGER NOT NULL CHECK (scored IN (0, 1)),
  score            REAL    CHECK (score IS NULL OR score BETWEEN 0.0 AND 1.0),
  band             TEXT    NOT NULL
    CHECK (band IN ('excellent', 'good', 'fair', 'poor', 'unscored')),
  linked_traces_to_outcomes INTEGER NOT NULL DEFAULT 0
    CHECK (linked_traces_to_outcomes IN (0, 1)),
  PRIMARY KEY (project_id, plan_id, task_id, iteration, schema_hash),
  FOREIGN KEY (project_id, plan_id, task_id, iteration, schema_hash)
    REFERENCES iterations (project_id, plan_id, task_id, iteration, schema_hash)
    ON DELETE CASCADE,
  CHECK ((scored = 0 AND score IS NULL AND band = 'unscored') OR
         (scored = 1 AND score IS NOT NULL AND band <> 'unscored'))
) WITHOUT ROWID;

CREATE INDEX scores_by_project_rubric
  ON scores (project_id, rubric_version, scored, score);

CREATE TABLE score_breakdown (
  project_id       TEXT    NOT NULL,
  plan_id          TEXT    NOT NULL,
  task_id          TEXT    NOT NULL,
  iteration        INTEGER NOT NULL,
  schema_hash      TEXT    NOT NULL,
  ordinal          INTEGER NOT NULL CHECK (ordinal >= 0),
  signal           TEXT    NOT NULL,
  label            TEXT    NOT NULL,
  present          INTEGER NOT NULL CHECK (present IN (0, 1)),
  sub_score        REAL    NOT NULL CHECK (sub_score BETWEEN 0.0 AND 1.0),
  detail           TEXT    NOT NULL DEFAULT '',
  nominal_weight   REAL    NOT NULL CHECK (nominal_weight BETWEEN 0.0 AND 1.0),
  effective_weight REAL    NOT NULL CHECK (effective_weight BETWEEN 0.0 AND 1.0),
  contribution     REAL    NOT NULL CHECK (contribution BETWEEN 0.0 AND 1.0),
  PRIMARY KEY (project_id, plan_id, task_id, iteration, schema_hash, ordinal),
  UNIQUE (project_id, plan_id, task_id, iteration, schema_hash, signal),
  FOREIGN KEY (project_id, plan_id, task_id, iteration, schema_hash)
    REFERENCES scores (project_id, plan_id, task_id, iteration, schema_hash)
    ON DELETE CASCADE
) WITHOUT ROWID;

CREATE TABLE sessions (
  project_id          TEXT    NOT NULL,
  session_id          TEXT    NOT NULL,
  plan_id             TEXT    NOT NULL,
  task_id             TEXT    NOT NULL,
  harness             TEXT    NOT NULL DEFAULT '',
  model               TEXT    NOT NULL DEFAULT '',
  wave                TEXT    NOT NULL DEFAULT '',
  rubric_version      TEXT    NOT NULL DEFAULT '',
  iteration_count     INTEGER NOT NULL CHECK (iteration_count >= 0),
  scored              INTEGER NOT NULL CHECK (scored IN (0, 1)),
  score               REAL    CHECK (score IS NULL OR score BETWEEN 0.0 AND 1.0),
  band                TEXT    NOT NULL
    CHECK (band IN ('excellent', 'good', 'fair', 'poor', 'unscored')),
  first_iteration     INTEGER CHECK (first_iteration IS NULL OR first_iteration >= 1),
  last_iteration      INTEGER CHECK (last_iteration IS NULL OR last_iteration >= 1),
  last_update         TEXT,
  iter_log_dir        TEXT    NOT NULL,
  mean_cache_hit_rate REAL    CHECK (mean_cache_hit_rate IS NULL OR mean_cache_hit_rate BETWEEN 0.0 AND 1.0),
  source_plan_id      TEXT    NOT NULL,
  source_task_id      TEXT    NOT NULL,
  source_iteration    INTEGER NOT NULL,
  source_schema_hash  TEXT    NOT NULL,
  updated_at          TEXT    NOT NULL,
  PRIMARY KEY (project_id, session_id),
  FOREIGN KEY (project_id, source_plan_id, source_task_id, source_iteration, source_schema_hash)
    REFERENCES iterations (project_id, plan_id, task_id, iteration, schema_hash),
  CHECK ((scored = 0 AND score IS NULL AND band = 'unscored') OR
         (scored = 1 AND score IS NOT NULL AND band <> 'unscored'))
) WITHOUT ROWID;

CREATE INDEX sessions_by_project_last_update
  ON sessions (project_id, last_update DESC, session_id);
CREATE INDEX sessions_by_project_score
  ON sessions (project_id, score DESC, session_id);
CREATE INDEX sessions_by_project_band
  ON sessions (project_id, band, last_update DESC, session_id);
CREATE INDEX sessions_by_project_harness
  ON sessions (project_id, harness, last_update DESC, session_id);

CREATE TABLE rubrics (
  project_id   TEXT    NOT NULL,
  version      TEXT    NOT NULL,
  active       INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
  document_json TEXT   NOT NULL
    CHECK (json_valid(document_json) AND json_type(document_json) = 'object'),
  created_at   TEXT    NOT NULL,
  PRIMARY KEY (project_id, version)
) WITHOUT ROWID;

CREATE UNIQUE INDEX rubrics_one_active_per_project
  ON rubrics (project_id) WHERE active = 1;
```

There is deliberately no `correction_observations` table in v1. The current iter-log and persisted score sources do not contain a standalone correction-observation record. Correction pressure is preserved losslessly as the `score_breakdown` row whose `signal='correction_pressure'`; claimed-vs-observed integrity rows, when present in a future enriched ingest payload, already have the versioned `iterations.integrity_json` slot matching the dashboard DTO. Creating an always-empty table would falsely promise data the source cannot supply.

`iterations`, `scores`, and `score_breakdown` are append-only versions. `sessions` is explicitly the replaceable read projection: in the same D1 transaction as an accepted version, it is recomputed and upserted from the winning iteration version per logical key, where the winner orders by `occurred_at DESC, ingested_at DESC, schema_hash DESC`. For a session, the projection uses all winning iterations; `rubric_version` is the most recent scored iteration's rubric, `score` is the mean of scored winning rows under that rubric, `band` is calculated from that rubric, and `mean_cache_hit_rate` is the mean of non-null winning rates. The latest winning iteration supplies harness/model/wave/plan/task and the source foreign key. An absent source session id maps deterministically to `legacy:<plan_id>:<task_id>`. `iter_log_dir` is the logical root `remote:<plan_id>`; `GET iterations/{n}?iter_log_dir=remote:<plan_id>` filters that plan. Without the parameter, the greatest winning `occurred_at` for `n` is selected.

Migration ownership is `obs/migrations/`, owned by the Worker/D1 task. Files are forward-only, zero-padded (`0001_initial.sql`, `0002_...sql`) and applied only by `wrangler d1 migrations apply <database> --local|--remote`. Worker request/startup code never runs DDL. The active rubric row is seeded by a migration from the same `DefaultRubric()` data used to generate the shared dashboard rubric DTO.

**Grounding:** `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` §§4.5, 5.3, 6.1–6.2; `.agents/workflow/plans/obs-dashboard-cf-deploy/TASKS.yaml` tasks o5/o7/o10; `schemas/workflow-iter-log.schema.json`; `internal/scoring/persist.go` (`PersistedScore`, contribution, and session shapes); `schemas/dashboard-run.schema.json`, `schemas/dashboard-iteration.schema.json`, `schemas/dashboard-rubric.schema.json`; `internal/dashboard/store/dto.go`.

### D4 — Crash-safe observability outbox

The outbox is `.agents/active/obs-outbox/`. It contains **one semantic event per file**, never a batch. The network sync layer may batch files for transport, but a failed write or one corrupt event cannot damage unrelated events.

#### File name and body

A ready file is named `<uuidv7>.obs-v1.json`; lowercase canonical UUIDv7 text makes lexicographic order creation-time order and supplies uniqueness. A writer first creates `.<uuidv7>.tmp` in the same directory with mode `0600` and exclusive create, writes the complete bytes, calls file sync, closes, renames to the ready name, and syncs the directory where supported. Interrupts can therefore leave only an ignored dot-temp file, never a partially readable ready file.

```json
{
  "outbox_version": 1,
  "id": "019b2774-2a00-7a00-8000-000000000001",
  "queued_at": "2026-07-19T12:00:00Z",
  "attempts": 0,
  "next_attempt_at": "2026-07-19T12:00:00Z",
  "last_error": null,
  "project_id": "github.com/AGOrcha/dot-agents",
  "client": {
    "da_version": "0.4.0",
    "host_os": "darwin",
    "agent_runtime": "claude-code"
  },
  "event": {
    "kind": "iteration.scored",
    "occurred_at": "2026-07-19T11:59:58Z",
    "plan_id": "obs-dashboard-cf-deploy",
    "task_id": "o1-obs-deploy-spec-and-contracts",
    "iteration": 12,
    "schema_hash": "<64 lowercase hex characters>",
    "payload": {},
    "score_sidecar": {}
  }
}
```

The body is strict (`additionalProperties: false` at every outbox-owned object). `id` is the canonical UUIDv7 from the filename stem and must match it. `event` is exactly one D3 ingest event; its hash is recomputed before send. Retry metadata is not part of `schema_hash`.

#### Drain, retry, deletion, corruption, and retention

- Drain ready files in filename order. Build a POST from at most 100 files without changing event order.
- Delete a file only after a parsed 200 response identifies its input index as accepted or deduped. Deletion failure is reported but safe: a later resend dedupes.
- A network error, 408, 429, 5xx, or `storage_unavailable` rejection retains the file. Atomically rewrite retry metadata with `attempts+1`, a sanitized `last_error`, and full-jitter exponential backoff in `[0, min(2^attempts seconds, 3600 seconds)]`; a valid `Retry-After` takes precedence up to 24 hours. Hook-driven drains honor `next_attempt_at`; explicit `da observability sync` ignores it for one forced attempt.
- 401/403 retains every file and stops that drain because credentials/configuration require operator action. A workflow hook still exits according to the local workflow result; explicit sync exits nonzero.
- A permanent item rejection is atomically moved to `.agents/active/obs-outbox/quarantine/<original-name>.rejected`; a locally malformed JSON file, unknown outbox version, filename/body UUID mismatch, or `schema_hash` mismatch moves to `<original-name>.corrupt`. Quarantine writes a sibling `<name>.reason.json` containing only code, message, and timestamp. Processing continues, and explicit sync exits nonzero after draining other valid files.
- Valid pending files have no age-based expiry: they remain until accepted/deduped or explicitly removed, because silent telemetry loss is worse than queue growth. Quarantine files and reason files are retained 30 days, then pruned during sync. Orphan dot-temp files older than 24 hours are removed during sync. Success deletes immediately. These are the v1 retention constants, not implementer choices.
- The outbox contains telemetry but no credential material. Local `.agents/history/` and iteration logs remain canonical; `sync --full` can reconstruct a deleted or lost outbox.

This follows the repository's already-shipped same-directory temp+rename pattern instead of inventing a weaker write path.

**Grounding:** `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` §§5.2, 6.1, 8; `.agents/workflow/plans/obs-dashboard-cf-deploy/TASKS.yaml` tasks o8–o10; `internal/scoring/persist.go` and `internal/credstore/store.go` (atomic temp-file + rename precedent).

### D5 — Shared SPA routes, DTO read model, and transport adapter

#### Exact HTTP surface

The remote Worker serves the seven existing routes verbatim, plus the versioned ingest route:

| Method | Route | `data` / transport |
|---|---|---|
| GET | `/api/v1/observability/runs` | `RunSummary[]` |
| GET | `/api/v1/observability/runs/{session_id}` | `RunDetail` |
| GET | `/api/v1/observability/runs/{session_id}/iterations` | `IterationSummary[]` |
| GET | `/api/v1/observability/iterations/{n}` | `IterationDetail` |
| GET | `/api/v1/observability/rubric` | `RubricDoc` |
| GET | `/api/v1/observability/health` | `Health` |
| GET/Upgrade | `/api/v1/observability/events` | SSE locally; WebSocket remotely |
| POST | `/api/v1/observability/ingest` | D3 ingest response |

There are no `/api/runs`, `/api/iterations`, `/api/stream`, or `/api/ingest` aliases.

Every successful JSON read is `{ "data": <DTO>, "meta": { "etag": "<opaque>", "count": <list-only integer> } }`; errors are `{ "error": { "code": "<stable token>", "message": "<human text>" } }`. ETag/If-None-Match, list pagination (`limit` 1–500 default 50, `offset` default 0), run filters/sort, and 304 behavior remain exactly the existing handler contract.

The Worker returns the existing schema fields unchanged:

- `RunSummary/RunDetail`: `session_id`, `harness`, `model`, `wave`, `rubric_version`, `iteration_count`, `scored`, `score`, `band`, `first_iteration`, `last_iteration`, `last_update`, `iter_log_dir`, optional detail-only `per_iteration[{iteration,scored,score,band}]`, and `mean_cache_hit_rate`.
- `IterationSummary/IterationDetail`: `iteration`, `session_id`, `schema_version`, `date`, `wave`, `task_id`, `commit`, `rubric_version`, `scored`, `score`, `band`, `files_changed`, `lines_added`, `lines_removed`, `retries`, `integrity_observation_count`, `transcript_turn_count`, `token_usage`, plus detail-only `verifiers`, `breakdown`, `integrity`, and `objective` with the exact nested shapes in `schemas/dashboard-iteration.schema.json`.
- `RubricDoc`: `version`, `combination`, `signals[{id,label,weight,description,two_way}]`, `bands[{name,min}]`.
- `Health`: `status`, `run_count`, `iteration_count`, `last_iter_log_mtime`, `subscriber_count`, `rubric_version`, `roots`. Remote `roots` is `[]`; `last_iter_log_mtime` is the greatest winning `occurred_at`; subscriber count comes from the project DO.
- Live events are `{type,seq,ts,payload}` and validate against `schemas/dashboard-event.schema.json`. The event vocabulary remains `iteration.scored`, `session.updated`, `score.recomputed`, `rubric.changed`, `heartbeat`.

D1 maps as follows: the `sessions` projection supplies run list/detail scalar fields; detail `per_iteration` is built from winning `iterations LEFT JOIN scores`; iteration DTOs come from the winning `iterations` row plus `scores` and ordered `score_breakdown`; `verifiers_json`, `integrity_json`, and `objective_json` decode directly into their optional detail fields. Missing score rows produce `scored:false`, `score:null`, `band:"unscored"`, `rubric_version:""`. `rubrics.document_json` supplies the active rubric. No remote-only field is added to a DTO.

#### Injectable transport seam

The seam is named `ObservabilityTransport`. It is the only object `useLiveUpdates` depends on:

```ts
export type DashboardEventType =
  | "iteration.scored"
  | "session.updated"
  | "score.recomputed"
  | "rubric.changed"
  | "heartbeat";

export interface ObservabilityTransportHandlers {
  onMessage(topic: DashboardEventType, event: R2DashboardSSEEventDTO): void;
  onOpen?(): void;
  onReconnect?(): void;
  onError?(delayMs: number): void;
}

export interface ObservabilityTransportOptions {
  topics: readonly DashboardEventType[];
  handlers: ObservabilityTransportHandlers;
}

export interface ObservabilityTransportConnection {
  close(): void;
  attempt(): number;
}

export interface ObservabilityTransport {
  connect(options: ObservabilityTransportOptions): ObservabilityTransportConnection;
}
```

`createSseObservabilityTransport()` is the local/default provider and wraps the current `createEventStream`/`EventSource` implementation at `/api/v1/observability/events`. `createWebSocketObservabilityTransport()` is provided by the obs deployment and converts the configured API origin to `wss:`, connects to the **same** `/api/v1/observability/events` path, and uses a DO Hibernation WebSocket. The application bootstrap injects one provider; components, query keys, DTO types, and invalidation logic are shared.

The WebSocket server sends one text frame per complete dashboard-event JSON object, in publish order. It sends a typed `heartbeat` event every 15 seconds. The browser sends no application frames in v1 and subscribes to all five topics; `topics` is a client-side dispatch filter so the interface remains identical to SSE. `seq` is strictly increasing per connection and resets after reconnect. Neither provider replays. Both implement exponential reconnect capped at 30 seconds and call `onReconnect` after any post-first open; the existing hook then invalidates all queries. Invalid JSON or a schema-invalid frame is treated as a transport error and reconnect, not passed to the UI.

This adapter replaces the current direct `createEventStream` call in `useLiveUpdates`; it does not fork or copy the SPA.

**Grounding:** `internal/dashboard/handlers/handlers.go` (literal base path and seven routes); `internal/dashboard/handlers/respond.go` (envelopes); `.agents/workflow/plans/r2-observability-dashboard/design/API.md` (methods, params, ETag, events); `schemas/dashboard-run.schema.json`, `schemas/dashboard-iteration.schema.json`, `schemas/dashboard-rubric.schema.json`, `schemas/dashboard-event.schema.json`; `web/dashboard/src/api/client.ts` (hard-coded versioned root); `web/dashboard/src/api/eventStream.ts` (current EventSource contract); `web/dashboard/src/hooks/useLiveUpdates.ts` (single live-update consumer/invalidation behavior).

### D6 — Authentication and local-development seam

Cloudflare Access is the sole production authenticator. Browser login and the CLI's service-token pair are validated at the Access edge. Both successful paths cause Access to inject `Cf-Access-Jwt-Assertion`. The Worker extracts that header and validates the Access-issued JWT: exactly RS256; non-empty `kid`; matching RSA JWKS key; signature; exact issuer `https://<team>.cloudflareaccess.com`; audience containing the obs app audTag; required unexpired `exp`; and `nbf` with at most 60 seconds clock skew. Missing/unavailable configuration or JWKS fails closed. It does not accept `Cf-Access-Client-Id` or `Cf-Access-Client-Secret` as origin authentication.

All static, read, WebSocket-upgrade, and ingest paths pass through the same JWT verifier before route dispatch. Ingest then applies D1's `OBS_PROJECT_ID` equality check. Path normalization/classification never creates an authentication carve-out.

The verifier has one injectable key source, not an auth bypass:

```ts
interface AccessJwksProvider {
  getJwks(): Promise<JsonWebKey[]>;
}

interface AccessJwtVerifier {
  verify(assertion: string): Promise<{ subject: string }>;
}
```

Production supplies `CloudflareAccessJwksProvider(teamDomain)`. Tests and local dev supply `StaticJwksProvider(publicJwks)` but execute the same parser, algorithm pin, claim checks, key selection, and signature verification.

The local fixture contract is:

- An ephemeral RSA-2048 keypair is generated at test/dev-process start. Only its public JWK is injected into `StaticJwksProvider`; no Cloudflare secret or service-token value exists.
- The fixture token header is `{ "alg":"RS256", "kid":"obs-dev-1", "typ":"JWT" }` and its claims are `{ "iss":"https://obs.test.invalid", "aud":["obs-dev"], "sub":"fixture-cli", "iat":now, "nbf":now-1, "exp":now+300 }`.
- `OBS_AUTH_MODE=fixture-jwt` is valid only with `ENVIRONMENT=development` and a loopback request host (`localhost`, `127.0.0.1`, or `[::1]`). Any fixture mode in a production environment or non-loopback host fails closed before routing. Production requires `OBS_AUTH_MODE=access`.
- Unit tests inject the assertion directly on an HTTPS `Request`. For `wrangler dev` over loopback HTTP, a dev helper passes the short-lived fixture JWT in `Cf-Access-Jwt-Assertion`. This is not a transport weakening because the fixture is non-production, five-minute, and unknown to Access.
- The CLI's only local exception is an explicit test seam `DA_OBS_TEST_JWT` when the endpoint is loopback and the process is in fixture mode. That seam attaches the fixture assertion directly and **never resolves** `observability.auth`. A credential-ref is never resolved or attached for any `http:` URL.
- The real `cf-access-service-token` pair is proven only against `https://obs.agorcha.dev` (or another Access-protected HTTPS hostname), where Access consumes it and the origin sees only the issued assertion.

**Grounding:** `docs/web/src/worker.js` (existing Access JWT extraction and RS256/JWKS/iss/aud/exp/nbf validation); `internal/docsaccess/client.go` and `internal/docsaccess/client_test.go` (resolve-after-HTTPS guard); `.agents/workflow/plans/obs-dashboard-cf-deploy/TASKS.yaml` o1/o6/o12; `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` §3.4 App 2.

## Requirements

1. **Configuration and credentials**
   - The AgentsRC lifecycle adds the exact D2 `observability` block and rejects unknown fields.
   - `push_throttle_seconds` is a non-negative integer, default 0; 0 means publish immediately. Throttling may coalesce network requests but never combines outbox files.
   - Credential storage, CI, direct CLI loading, and a future proxy all resolve the same id and the same strict credential object.
   - A client rejects a credential-bearing non-HTTPS endpoint before credential resolution.

2. **Authentication and tenant isolation**
   - Access validates raw browser/service credentials; the Worker validates only the resulting JWT.
   - Every route, including static assets and WebSocket upgrade, is fail-closed.
   - A valid JWT does not authorize a foreign `project_id`; the reference deployment accepts only `github.com/AGOrcha/dot-agents`.

3. **Ingest and persistence**
   - Each event validates before the DO/D1 write; the project DO serializes it, and the D1 mutation is one transaction under the five-part idempotency key.
   - Duplicate content does not mutate D1 or rebroadcast.
   - New content versions append source/score rows and atomically refresh the session projection.
   - D1 migrations are forward-only, source-controlled, and applied outside request handling.

4. **Offline behavior**
   - A publish failure never changes the success/failure result of the local checkpoint or verification command.
   - Every queued ready file is crash-safe and independently recoverable.
   - Explicit sync reports auth, corruption, quarantine, and retained transient failures while continuing safe work.
   - `sync --full` can rebuild the remote read model from local canonical history.

5. **Shared UI/API**
   - The Worker passes the existing handler/schema contract tests for all seven read/live routes.
   - The SPA build is reused; only `ObservabilityTransport` is injected.
   - SSE and WebSocket deliver the same dashboard-event object and identical reconnect/invalidation semantics.
   - No unversioned API alias exists.

## Open Questions

These questions do not alter any v1 contract above:

1. **Public aggregate statistics:** should a later deployment expose unauthenticated aggregate-only statistics? V1 says no and has no `/public-stats` route.
2. **Multiple Cloudflare accounts:** should one logical deployment span projects owned by different CF accounts? V1 is one project/backend/account; cross-account D1 partitioning is deferred.
3. **Future unknown-schema handling:** v1 rejects unknown versions and kinds. A later protocol version may choose reject, negotiate, or retain an audited raw envelope, but cannot silently change v1 behavior.
4. **Future retention configurability:** v1 retains valid pending outbox events indefinitely, quarantine for 30 days, and orphan temps for 24 hours. Whether team/org profiles make those durations configurable remains open; no implementation may choose different v1 defaults.
5. **Access cookie scope:** bootstrap must verify whether the Zero Trust auth domain/cookie covers both `agorcha.dev` and `obs.agorcha.dev`. This affects login UX, not Worker JWT validation.
6. **High-rate coalescing policy:** `push_throttle_seconds` exists and defaults to 0. The exact team/org default, if traffic ever approaches Workers quotas, remains open.

The proposal's former questions about the AgentsRC shape, Worker source root, outbox format, and v1 unknown-schema result are resolved here: the block is D2, Worker source is the plan-owned root `obs/`, outbox is D4, and unknown input is rejected.

## Done Criteria

1. The canonical spec exists at `.agents/workflow/specs/obs-dashboard-cf-deploy/design.md`, and downstream configuration, Worker, D1, CLI, hook, and backfill work cites the five contracts above without another shape decision.
2. A fixture AgentsRC document with D2 round-trips and validates; one `agorcha-obs` loader lookup returns the complete pair; a partial/wrong-kind object fails; an HTTP endpoint proves the loader was never called.
3. Against `wrangler dev` with the D6 signed fixture JWT, the versioned ingest route accepts a fixture, the identical repost reports one dedupe, D1 contains exactly one version, a distinct version is accepted, and a foreign project is rejected without a D1 row.
4. With the endpoint unreachable, a real checkpoint still exits according to its local result and leaves one parseable ready outbox file with no partial ready file. Restart plus sync accepts/deletes it; a corrupt file quarantines; transient failure persists retry metadata; successful/deduped files alone are deleted.
5. The reused SPA loads from `/api/v1/observability/runs`; all seven existing routes return the existing envelopes and schema-valid DTOs. No unversioned alias resolves.
6. The local composition receives a valid SSE event and the remote composition receives the same schema-valid event through the injected WebSocket provider. After an ingest, the authenticated remote UI receives the live frame and invalidates/renders within 2 seconds.
7. Production preview smoke proves fail-closed behavior: no assertion is rejected; wrong audience/issuer/algorithm/expired JWT is rejected; encoded/case/path variants cannot bypass auth; a real Access service-token pair succeeds only through the Access-protected HTTPS hostname and the Worker sees the assertion, not the raw pair.
8. `da observability sync --full` populates the run set, a second run is all dedupes, and wiping/recreating remote D1 followed by full sync reproduces the same versioned route payloads/ETags.
9. Focused schema, Worker, D1, CLI, and SPA tests pass; the shipped preview satisfies the plan's end-to-end live smoke, including D1 dedupe, WebSocket delivery under 2 seconds, auth rejection cases, and local-canonical rebuild.

## Deferred

- Public statistics and unauthenticated dashboard data.
- Multi-account/multi-tenant routing in one deployed Worker.
- Per-project service tokens within one backend; the reference deployment already enforces one configured project.
- R2 raw transcript storage and remote transcript-derived recomputation.
- Browser-driven service-token provisioning and a shared dm6/observability provisioner.
- `da service` credential proxy/injector; direct shared-credstore loading remains the fallback contract.
- Durable event replay/cursors. Both live providers reconnect and refetch in v1.
- Bidirectional dashboard commands over WebSocket; the remote socket is server-to-browser events only in v1.

## Related

- `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` — Access App 2, DO+D1, push/sync/auth, local-canonical state, retention, and original open questions.
- `.agents/workflow/plans/obs-dashboard-cf-deploy/TASKS.yaml` — consumers and live-smoke acceptance.
- `.agents/workflow/specs/external-agent-sources/design.md` — auth provider vocabulary and shared-auth direction; notably no credential-ref schema.
- `.agents/workflow/specs/dm6-da-sso-autowire/design.md` — separate docs-specific provisioning flow; sibling, not dependency.
- `.agents/workflow/plans/r2-observability-dashboard/design/API.md` — authoritative endpoint behavior.
- `schemas/agentsrc.schema.json` — current opaque auth pass-through.
- `schemas/workflow-iter-log.schema.json` — ingest payload source.
- `schemas/dashboard-run.schema.json`, `schemas/dashboard-iteration.schema.json`, `schemas/dashboard-rubric.schema.json`, `schemas/dashboard-event.schema.json` — unchanged SPA DTO/event contracts.
- `internal/credstore/loader.go`, `internal/credstore/store.go` — actual credential resolution/storage and atomic-write behavior.
- `internal/dashboard/handlers/`, `internal/dashboard/store/dto.go`, `web/dashboard/src/` — existing route, DTO, fetch, EventSource, and invalidation implementations.
