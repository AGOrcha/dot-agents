# agorcha.dev: public/internal docs split + observability server deployment architecture

**Status:** project-local proposal (draft)
**Created:** 2026-05-28
**Author:** agorcha-arch-design-worker (delegation `del-agorcha-public-vs-internal-and-obs-deploy-1779979925`)
**Parent task:** `pr10-branch-split/agorcha-public-vs-internal-and-obs-deploy`
**Related plans / proposals:**
- `.agents/workflow/plans/r2-observability-dashboard/` (draft — current spec assumes single-user `127.0.0.1`; this proposal proposes the multi-user deploy shape)
- Task `deploy-agorcha-dev-cloudflare` (PR #143 / worktree `cloudflare-deploy/docs/web/wrangler.jsonc` — agorcha.dev Worker landed)
- Task `docs-interactive-html` (PR #143 / worktree `docs-interactive-html/docs/web/` — Astro site with `/canonical/` + `/demos/` routes)
- `~/Documents/payout/po-cluster-manager/config/ingress/cloudflare-access-applications.yaml` — CF Access reference

This proposal is **not a formal spec yet**. Each section ends with a recommended decision the maintainer can ratify (or amend) before any of this fans out to implementation tasks.

---

## §1 — Public vs internal content split

### 1.1 Problem

`agorcha.dev` is a single public Worker fronting a static Astro build. Today everything Astro builds is reachable to anyone who lands on the site. The repo's `.agents/` tree carries a mix of:

- safe-public artifacts (demo walkthroughs, canonical specs we want third parties to read, schemas, CLI reference)
- legitimately-internal artifacts (raw drafts, fold-back notes referencing incident details, lessons that namedrop internal repos, half-cooked proposals, in-flight worktree status)

We need a stable rule for which content reaches `agorcha.dev/` vs which is gated behind CF Access at `agorcha.dev/internal/*`.

### 1.2 Options considered

| Approach | Pro | Con |
|---|---|---|
| (a) **Path-based** (`docs/public/*` vs `docs/internal/*`) | Trivial routing — Worker just checks URL prefix | Forces a physical reshuffling of existing tree; conflicts with the source-of-truth where `.agents/lessons/` and `docs/` live by feature, not by audience |
| (b) **Per-doc frontmatter flag** (`visibility: public \| internal` with **default = internal**) | Author marks intent at the document, source layout stays as-is, default-deny is safe | Requires Astro content collection schema change + a build-time partitioner |
| (c) **Content-tag enumeration in a manifest** (`docs/web/visibility.yaml` listing public paths) | Central audit point | Drifts from content; every new file requires editing two places |

### 1.3 Recommendation

**Adopt (b) — per-doc frontmatter `visibility` field, default = `internal`.**

Concretely:

1. Extend the Astro content collection schemas in `docs/web/src/content.config.ts` to add a `visibility: z.enum(['public', 'internal']).default('internal')` field on every collection (`docsCanonical`, `lessons`, `specs`, `proposals`, plus the demo collection).
2. At build time, partition into two outputs:
   - `dist/` — the existing public Astro build, **only** entries with `visibility: 'public'` reach the route tree. Internal entries are dropped from collections + route generators.
   - `dist-internal/` — a second build pass that includes everything (no filter). Worker serves this when the request matches `/internal/*` and CF Access auth passes.
3. The Worker (`docs/web/src/worker.js`) gains a `/internal/*` branch that serves from a second asset binding (`ASSETS_INTERNAL`) instead of `ASSETS`.
4. Pre-existing files have no frontmatter — they pick up `default = internal`, so the **fail-safe behaviour is "stays gated"**. Reclassifying to public is an explicit, reviewable change.

### 1.4 Pull-list — initial classification of current content

Public (recommend explicit `visibility: public`):

- `docs/DEMO_*.md` (5 files — already demo-positioned; explicit user-facing artifacts)
- `docs/GETTING_STARTED.md`, `docs/PROJECT_DIAGRAMS.md`, `docs/RELEASE_VERIFICATION.md`, `docs/GLOBAL_FLAG_CONTRACT.md` — surface a third party can usefully read
- `docs/adr/` (9 ADRs) — historical decisions are valuable public artifacts; reviewable, no surprise content
- `schemas/*.schema.json` — must stay public (editor tooling fetches them unauthenticated)
- `README.md` (rendered at `/`) — already implicitly public
- Eventually: a curated subset of `.agents/lessons/<name>/LESSON.md` (a "public lessons" curation pass — defer to a separate task)

Internal (recommend implicit / explicit `visibility: internal`):

- `.agents/proposals/*.md` — drafts; the whole point of "proposal" status is it's not yet ratified
- `.agents/workflow/specs/<id>/design.md` — in-flight specs leak implementation direction before maintainer sign-off
- `.agents/active/**` — fold-back, delegation bundles, verification artifacts: by definition the working set, not user-consumable
- `.agents/history/**` — useful internally for trend lines + observability source data; not a third-party artifact
- `.agents/lessons/<name>/LESSON.md` — default internal; whitelist individual lessons to public after curation
- `research/` — exploratory notes; default internal
- `docs/TYPESCRIPT_PORT_*.md` — parked work, keep internal

### 1.5 Decision

**RECOMMENDED:** per-doc frontmatter `visibility: public | internal`, default `internal`, enforced at Astro build time by partitioning into `dist/` (public) and `dist-internal/` (everything). Worker routes `/internal/*` to `dist-internal` only after CF Access auth.

---

## §2 — Routing topology on agorcha.dev

### 2.1 Trade-off: subdomain vs path

| | **Subdomain** (`internal.agorcha.dev`, `obs.agorcha.dev`) | **Path** (`agorcha.dev/internal/*`, `agorcha.dev/obs/*`) |
|---|---|---|
| DNS | A CNAME per surface; each is its own zone-level record | One DNS record (already exists) |
| CF Access app config | Cleaner — one app per hostname pattern (matches the payout convention; `publicHostnames: ["*.usepayout.com"]`) | One app with two `publicHostnames` patterns (`agorcha.dev/internal/*`, `agorcha.dev/obs/*`) — CF Access supports path patterns but `publicHostnames` semantics are hostname-anchored in the IaC schema |
| Worker count | Either separate Workers per subdomain (cleaner blast radius) or one Worker handling all hostnames | One Worker, one routing tree |
| Cookies / auth scope | Each subdomain is its own CF Access cookie scope unless you set the team's auth domain to cover the apex | Auth cookie covers everything; one login session |
| User-facing URL clarity | "obs is its own thing" reads more clearly | Path-based feels lower-status, "tucked away" |
| Schema serving (`/schemas/*` MUST stay public-no-auth) | Trivial — `schemas.agorcha.dev` or `agorcha.dev/schemas/*` either works | Trivial — same Worker, `/schemas/*` skips the auth check |
| Per-app CF Access policy granularity | High — separate audTags per subdomain | Lower — same audTag for the whole `/internal/*` family |

### 2.2 What the payout reference actually shows

`payout/po-cluster-manager/config/ingress/cloudflare-access-applications.yaml` runs **four separate `CloudflareAccessApplications` entries**, each with its own `audTag` + `publicHostnames` list. They mix hostname-anchored (`*uat.usepayout.com`) and path-anchored (`artifactory.usepayout.com/repo/repository/po-pypi`) patterns. So **CF Access supports both — the choice is operational, not technical.**

Notable: payout splits "general non-prod apps" (`nonprd-apps`) from "infrastructure" (`po-inf-apps`) from "artifactory" — separation is by **policy domain**, not by URL structure. That's the right heuristic.

### 2.3 Recommendation

**Subdomain split for distinct policy domains; path layout for things sharing one policy.**

Concrete URL shape:

```
agorcha.dev/                           PUBLIC          (docs root — current Worker)
agorcha.dev/demos/*                    PUBLIC          (demo walkthroughs)
agorcha.dev/canonical/*                PUBLIC          (only public-flagged entries)
agorcha.dev/schemas/*                  PUBLIC          (no auth — editor tooling)
agorcha.dev/internal/*                 CF ACCESS gated (maintainer team — "agorcha-maintainers" policy)
obs.agorcha.dev/                       CF ACCESS gated (observability dashboard — separate Worker, separate audTag)
obs.agorcha.dev/api/*                  CF ACCESS gated for UI XHRs; bearer-token auth for CLI POST
```

Why two surfaces (not three):

- `/internal/*` and `/canonical/internal-only/*` collapse into one policy ("you're a maintainer; you see everything not-public"). One app, one audTag.
- `obs.agorcha.dev` is a **different policy boundary**: the API there accepts bearer-token POSTs from CLI clients (not browser-shaped). Mixing browser-auth (CF Access) and bearer-auth on the same hostname forces the Worker to disambiguate per-route; on its own subdomain, the API path has clean auth semantics (bearer-or-CF-Access, dual-mode). Also matches the payout convention of one app per policy domain.
- `schemas.agorcha.dev` is rejected — `agorcha.dev/schemas/*` is sufficient (zero-auth path is a one-line Worker `if` before the CF Access check).

### 2.4 Decision

**RECOMMENDED:**
- `agorcha.dev/*` (public docs) — existing Worker, just gains `/internal/*` branch behind CF Access.
- `obs.agorcha.dev` — new Worker, new CF Access app, dual-mode auth (CF Access for browser, bearer token for CLI POST).
- `agorcha.dev/schemas/*` stays unauth (zero-auth carve-out in Worker before CF Access check).

---

## §3 — CF Access Application provisioning

### 3.1 IaC vs manual — the payout pattern in context

The payout YAML is **declarative IaC** (`CloudflareAccessApplications` is a custom CRD applied by their infra controller). Each entry carries:

- `name` — operator-facing identifier
- `teamName: usepayout` — the Cloudflare Zero Trust team
- `id` — CF-assigned UUID (returned by the API on create; pinned in YAML afterwards)
- `audTag` — JWT audience for the app, used by downstream services to verify the CF Access JWT
- `publicHostnames` — patterns the app gates

`audTag` matters most: it is what a Worker (or any downstream verifier) uses to check that the JWT it received was issued for this specific app. Without the right audTag, `cloudflare/workers-access` JWT verification rejects the cookie.

### 3.2 Options

- (a) **Replicate the payout IaC pattern** — port the `CloudflareAccessApplications` CRD shape to a Terraform/Pulumi module in this repo. Heavy: requires a controller + secret-bearing provider creds.
- (b) **Manual bootstrap via CF dashboard** for v1 — fast, well-documented; commit the resulting audTag + policy summary to repo docs as the source of truth.
- (c) **`cloudflare-go` / `wrangler access`** for IaC later — moderate weight; runs as part of the existing GH Actions deploy pipeline, no new platform.

### 3.3 Recommendation

**(b) manual bootstrap now, (c) `cloudflare-go` IaC later — once there are >2 apps to manage.** Document everything created manually in `docs/cf-access-bootstrap.md` so it can be rebuilt deterministically.

### 3.4 Concrete bootstrap steps (CF dashboard click-path)

Two apps to create. Both live under the existing CF Zero Trust team (presumably `agorcha` or a new one — check `dash.cloudflare.com → Zero Trust → Settings → General → Team name`).

**App 1: agorcha-internal-docs**

- *Zero Trust → Access → Applications → Add an application → Self-hosted*
- Application name: `agorcha-internal-docs`
- Subdomain: blank (apex)
- Domain: `agorcha.dev`
- Path: `/internal/*`
- Application Audience tag: (auto-generated; **copy this** — this is `audTag`, paste into `docs/cf-access-bootstrap.md`)
- Identity providers: GitHub (preferred — already used by the user) + One-time PIN (fallback for non-GH maintainers)
- Add a policy:
  - Name: `maintainers`
  - Action: `Allow`
  - Include: `Emails ending in @<your maintainer domain>` **OR** `Emails: nikashprakash1@gmail.com` (per `userEmail` in CLAUDE.md memory) for v1
  - Session duration: 24h
- Save.

**App 2: agorcha-obs**

- Same flow, Subdomain: `obs`, Domain: `agorcha.dev`, Path: `*`
- Identity providers: same as above
- Add a policy:
  - Name: `obs-maintainers`
  - Action: `Allow`
  - Include: same rule as app 1 (kept separate so it can diverge later — e.g. a read-only "reviewer" policy added later applies only here)
- **Add a Service Auth policy** (this is what lets the CLI POST):
  - Name: `obs-cli-service-tokens`
  - Action: `Service Auth`
  - Include: `Service Token` → `agorcha-obs-cli` (create via Access → Service Auth → Service Tokens before the policy; the token's `Client-ID` + `Client-Secret` go to the CLI; see §5)
- Save; **copy the audTag** for this app.

### 3.5 Recommended team-rule vs email-rule choice (open question §8 resolution)

**For v1: email-rule with explicit maintainer emails.** Reason: there is no "agorcha team" elsewhere — payout's `teamName: usepayout` reflects an existing org with team membership records. AGOrcha-the-org on GitHub is the closest analog, but CF Access GitHub integration grants on org-membership-at-auth-time, which requires the user to be visible in the org. For v1, an explicit email allowlist is lower-friction and easy to widen to `Members of GitHub org: AGOrcha` later.

### 3.6 Document the audTag

Add a tracked file `docs/cf-access-bootstrap.md` with:

```yaml
# Source-of-truth for CF Access apps gating agorcha.dev / obs.agorcha.dev
# Bootstrap method: manual (CF dashboard, 2026-05-XX).
# IaC migration: tracked in task <future>.

apps:
  - name: agorcha-internal-docs
    domain: agorcha.dev
    path: /internal/*
    audTag: <fill-in-after-bootstrap>
    policies: [maintainers]

  - name: agorcha-obs
    domain: obs.agorcha.dev
    path: "*"
    audTag: <fill-in-after-bootstrap>
    policies: [obs-maintainers, obs-cli-service-tokens]
```

This file is the contract Workers verify against (JWT `aud` claim must equal audTag).

### 3.7 Decision

**RECOMMENDED:** manual bootstrap of two CF Access apps (`agorcha-internal-docs`, `agorcha-obs`); audTag + policy summary committed to `docs/cf-access-bootstrap.md`; identity = GitHub + OTP, allow rule = explicit maintainer emails for v1; obs gains a Service Auth policy for CLI bearer-token POSTs. IaC migration (`cloudflare-go`-based) deferred to a follow-up task once both apps are stable.

---

## §4 — Observability server architecture

### 4.1 Context: the existing r2-observability-dashboard design

`r2-observability-dashboard/design.md` (currently draft) chose:

- Read-through from `.agents/active/iteration-log/` + score sidecars on local disk (no new store)
- In-process LRU cache (~256 entries, TTL 30s)
- SSE for live updates
- Service binds to `127.0.0.1` — single-user, no auth, no remote

That model is **correct for the local single-user case** but does not generalise to "agorcha.dev hosts the dashboard, multiple managed projects feed in." For the deploy this proposal addresses, the storage assumption flips: there is no local disk to read from — the data has to be sent to the server.

The cleanest framing: **r2 v1 stays as designed (local single-user); this proposal adds an r2-v2 / "remote" mode that's a different deployment of the same UI, backed by a CF-native store.**

### 4.2 Runtime options

| Option | Storage | Notes |
|---|---|---|
| (a) Worker + Durable Object (DO) for per-project state | DO storage (KV-shaped, transactional) | DO per project gives strong tenant isolation; SQLite-in-DO (beta as of 2026-Q1) makes relational queries viable |
| (b) Worker + D1 (SQLite at edge) | D1 (a single regional SQLite) | Best for cross-project aggregate queries; simpler ingest API |
| (c) Worker + R2 for blob ingest + Worker for query | R2 (object store) | Best for high-volume raw events; query layer needs to roll up |
| (d) External service (Fly.io, Render) + Worker proxy | Postgres or similar | Maximum flexibility, maximum operational burden |

### 4.3 Free-tier feasibility (the question §4 explicitly asks)

As of early-2026 Cloudflare pricing (verify before final design):

- **Durable Objects free tier:** 1M req/mo, 400k GB-s of duration, 1GB storage on Workers Free plan; SQLite-in-DO storage 5GB/account on paid Workers plan. Personal usage (single maintainer, a few managed projects, ≤ a few hundred iterations/day) sits very comfortably inside that.
- **D1 free tier:** 5GB storage, 5M rows-read/day, 100k rows-written/day on the free plan; 25 databases per account. The dot-agents repo's entire `.agents/history/` tree today is 4.5MB across ~945 files — even compressed 10× into a relational form (~450KB) plus 10 other managed projects of similar scale puts us at ~5MB. Three orders of magnitude under the cap.
- **Workers free tier:** 100k req/day. Dashboard load + SSE keep-alive + a CLI sync every iteration close puts a single maintainer at a few hundred req/day worst case.

**Verdict: free tier is sufficient for v1 personal use with significant headroom.** If we onboard >10 active projects each hitting hundreds of iterations/day, re-evaluate; the upgrade path is Workers Paid ($5/mo) which raises everything by 100×–1000×.

### 4.4 Capacity estimate from current usage

Baseline measurement: `/Users/nikashp/Documents/dot-agents/.agents/history/` = **4.5 MB, 945 files** representing roughly a year of plan history for the dot-agents repo (the most active repo on the maintainer's machine).

Conservative growth model:
- 10 managed repos × 5 MB/yr = 50 MB/yr raw
- D1 relational compression ~5× → ~10 MB/yr in D1
- After 5 years: 50 MB. **1% of the 5GB D1 free tier.**

SSE / DO event rate:
- Average 5 active runs × 20 iterations × 1 event/iteration = 100 events/day per project
- 10 projects × 100 = 1000 events/day
- DO duration per event ~50ms → 50s/day = 0.0006 GB-s/day. Trivial.

### 4.5 Recommendation

**Hybrid: Worker + Durable Object (per project) for live run state + D1 for historical aggregates and cross-project queries.**

Specifically:

- **DO per project** (DO ID = `project_id` hash) holds: in-flight iteration buffer (last 100 iterations), current SSE subscriber list, per-project rolling counters. State persists in DO storage; eviction TTL = 24h after last activity.
- **D1 single database** holds: append-only `iterations`, `sessions`, `scores`, `correction_observations` tables, keyed by `(project_id, iteration_id)`. Read-only from the dashboard; written-to by the ingest path (which lives in the DO; DO writes through to D1 on each accepted event).
- **R2 for raw transcript blobs** (deferred to v2; iter-log records reference R2 keys, but v1 only ingests metadata + scores, not raw transcripts).

Why this shape:
- DO provides per-project consistency without a central lock — natural sharding by tenant.
- D1 provides cross-project historical query in a single SQL surface (e.g., "score trend across all my projects").
- Both are CF-native — single deploy story, same Wrangler tooling, same dashboard, no second platform.
- Free-tier-sufficient with the capacity estimate above.

### 4.6 Why not the alternatives

- **Worker + DO only**: queries across projects require fan-out across DOs; not D1-shaped SQL. Pushes more logic into the Worker.
- **Worker + D1 only**: loses per-project state isolation for SSE; every subscriber bookkeeping becomes a D1 write. Higher write volume = closer to D1 free-tier write cap.
- **External service**: a single-user maintainer dashboard is not worth the second platform. CF stays in lock-step with the agorcha.dev Worker we already deploy.

### 4.7 Decision

**RECOMMENDED:** Worker + Durable Object (per project, sharded by `project_id`) + D1 (single shared database, project_id-keyed tables). R2 deferred to v2 (raw transcripts). All on Cloudflare free tier for personal use.

---

## §5 — Data flow: local CLI → remote server

### 5.1 Push vs pull vs hybrid

| Mode | When data moves | Pro | Con |
|---|---|---|---|
| Push | At iteration close (`da workflow checkpoint` / `da workflow verify record`) | Live UI; sub-2s update; no extra moving parts | Requires the CLI host to reach the network; needs auth tokens |
| Pull | GH Actions cron sync of `.agents/history/` to R2 | No live network coupling at iter-close time; survives offline iterations | Latency = cron interval; dashboard never shows "running now"; requires per-repo GH Actions setup |
| Hybrid | Push for live events; pull for historical backfill | Best of both | Two code paths to maintain |

### 5.2 Recommendation

**Hybrid — push for live state, optional pull (CLI-initiated, not cron) for historical backfill.**

Concretely:

1. **Push (live)**: extend `da workflow checkpoint` and `da workflow verify record` with a `--obs-publish` flag (default off; turned on by `.agentsrc.json` `observability.enabled: true`). On success, the CLI POSTs the new event to `obs.agorcha.dev/api/ingest`. **Best-effort**: a failed POST does not fail the local command — it logs a warning and queues to `.agents/active/obs-outbox/` for the next sync to retry.
2. **Sync (catch-up)**: new command `da observability sync`. Walks `.agents/active/obs-outbox/` + (with `--full`) `.agents/history/<plan>/` and POSTs anything the server doesn't already have. Idempotent — server dedupes by `(project_id, iteration_id, schema_hash)`.
3. **No cron / GH Actions pull**: out of scope for v1. The maintainer runs `da observability sync` if they want to backfill; that is enough for personal use.

### 5.3 Concrete payload schema for `da observability sync`

POST `obs.agorcha.dev/api/ingest` with body:

```json
{
  "schema_version": 1,
  "project_id": "github.com/NikashPrakash/dot-agents",
  "client": {
    "da_version": "0.4.0",
    "host_os": "darwin",
    "agent_runtime": "claude-code"
  },
  "events": [
    {
      "kind": "iteration.scored",
      "occurred_at": "2026-05-28T19:43:00Z",
      "plan_id": "pr10-branch-split",
      "task_id": "agorcha-public-vs-internal-and-obs-deploy",
      "iteration": 12,
      "payload": { "...iter-log entry conformant to schemas/workflow-iter-log.schema.json..." },
      "score_sidecar": { "...iter-N.score.yaml shape if present..." }
    }
  ]
}
```

- Auth header: `Cf-Access-Client-Id` + `Cf-Access-Client-Secret` (the service-token pair from §3.4 App 2's Service Auth policy)
- Response: `{ "accepted": N, "deduped": M, "rejected": [...] }` (200 always; rejections itemised, do not fail the request)
- Server idempotency key: `(project_id, plan_id, task_id, iteration, schema_hash)` — re-sending the same event is safe

`schema_version` reserved so the payload can evolve without breaking older clients; server accepts any version it understands.

### 5.4 Auth model — bearer token storage

The CF Access Service Token is a `(Client-ID, Client-Secret)` pair issued in step §3.4 App 2.

Storage options:
- **`.agentsrc.json` `observability.service_token`** — *no*: this file is committed to repo. Secret leak.
- **`.dev.vars` next to `.agentsrc.json` (gitignored)** — viable for local dev parity with Wrangler conventions.
- **OS keychain / `da observability login` writes to `~/.config/da/credentials.json` (0600 perms)** — cleanest. Matches how other CLI tools (`gh`, `op`) work.

**Recommendation:** `~/.config/da/credentials.json` with structure:

```json
{
  "observability": {
    "endpoint": "https://obs.agorcha.dev",
    "client_id": "<UUID>.access",
    "client_secret": "<secret>"
  }
}
```

`da observability login` prompts for the pair (or accepts `--from-env`); the file is mode 0600 and gitignored by being outside the repo. `.agentsrc.json` carries only `observability.endpoint` (non-secret) and `observability.enabled: true|false`. The CLI reads the secret from `~/.config/da/credentials.json`.

### 5.5 Decision

**RECOMMENDED:**
- Push live events on `da workflow checkpoint` / `verify record` (best-effort, queues on failure).
- `da observability sync` for catch-up / backfill (idempotent).
- No cron / GH Actions in v1.
- Bearer token = CF Access Service Token, stored in `~/.config/da/credentials.json` (mode 0600), endpoint in `.agentsrc.json`.

---

## §6 — Storage source-of-truth + multi-tenant key shape

### 6.1 Source of truth

**Local `.agents/history/` and `.agents/active/iteration-log/` remain canonical. The remote D1 + DO state is a derived cache.**

Consequences:

- A maintainer can wipe the remote D1 at any time and rebuild from local by running `da observability sync --full` across each managed project. Server holds no data that doesn't have a local origin.
- Local commands never block on remote state. If the network is down, work proceeds; sync catches up later.
- Disaster recovery for the remote is trivial: redeploy + resync.
- Schema migrations: when the iter-log schema changes (already at v2), the server-side ingest adds a new version handler; old records keep working because they record their `schema_version`.

### 6.2 Multi-tenant key shape

`project_id` is the partition key everywhere.

Recommended derivation: `project_id` = the canonical git remote URL normalised to `<host>/<owner>/<repo>` form (e.g., `github.com/NikashPrakash/dot-agents`). This is **already derived** by `internal/gitremote/` for the v2 config migration (per pr10-branch-split notes on PR #127).

- DO ID: `idFromName(project_id)` — gives a stable per-project DO instance
- D1 row key: `(project_id, plan_id, task_id, iteration)` tuple, with `project_id` as the leading column on every index
- Service Token issuance: one Service Token per project (created on first `da observability login --project <project_id>`). Server registers the token's Client-ID against the project_id allowlist on first use. **Per-project tokens contain the project they're authorised for**; cross-project POSTs are rejected.

**For v1 single-maintainer use, a single Service Token covering all the maintainer's projects is acceptable** — the simplification is: one token, server trusts it to write under any `project_id` the request carries. Per-project tokens are the v2 hardening for multi-maintainer scenarios.

### 6.3 Per-project token issuance flow (v1)

1. Maintainer runs `da observability login` once per machine.
2. Browser opens `obs.agorcha.dev/auth/cli-bootstrap` (CF Access protected — maintainer authenticates).
3. Server creates a Service Token via the CF API, returns `{client_id, client_secret}` in the page.
4. CLI receives the pair via a localhost callback (`http://localhost:8765/oauth/callback?...`) **or** via copy-paste prompt.
5. CLI writes to `~/.config/da/credentials.json`.
6. Subsequent `da observability sync` / push events use the token.

For v1 this can be **manual**: maintainer creates the Service Token via CF dashboard (§3.4 step), runs `da observability login --from-env CF_OBS_CLIENT_ID=... CF_OBS_CLIENT_SECRET=...`. The browser-flow above is v2 polish.

### 6.4 Decision

**RECOMMENDED:** Local-canonical, remote-cache model. `project_id` = canonical git remote URL form (already derived by `internal/gitremote/`). Single maintainer token in v1 (multi-token deferred). Manual `login --from-env` bootstrap in v1; browser callback in v2.

---

## §7 — Implementation sequence after this proposal lands

Next-task list with deps. Each is a candidate task on the `pr10-branch-split` plan (or a new plan once `release-v0-4-0` ships):

1. **`cf-access-bootstrap-manual`** — `[no deps]` — manual CF dashboard click-path per §3.4; commit `docs/cf-access-bootstrap.md` with audTags + policy summary. **Maintainer-must (cannot delegate fully — needs CF dashboard auth).**

2. **`docs-frontmatter-visibility`** — `[no deps]` — Astro content collection schema change (add `visibility` field, default internal); partition build into `dist/` + `dist-internal/`; update Worker to serve both. Classify existing content per §1.4.

3. **`worker-cf-access-gate-internal`** — `[deps: 1, 2]` — Worker code change: `/internal/*` branch verifies CF Access JWT against audTag from §3.4 App 1; routes to `ASSETS_INTERNAL` binding. CI deploys both `dist/` and `dist-internal/`.

4. **`r2-observability-deployed-mode-spec-amendment`** — `[deps: 1]` — amend `.agents/workflow/plans/r2-observability-dashboard/design.md` (or fork to a v2 plan) to formalise the CF-Worker + DO + D1 architecture per §4.5. Promote draft to next-impl.

5. **`obs-worker-scaffold`** — `[deps: 1, 4]` — new directory `obs/` (top-level or under `docs/web/`); Wrangler config for `obs.agorcha.dev`; DO + D1 bindings; minimal ingest endpoint accepting `POST /api/ingest`; reject everything else.

6. **`obs-dashboard-frontend`** — `[deps: 5]` — Astro/React UI served at `obs.agorcha.dev/` (or move r2's existing React/TS plan here); reads from `/api/runs`, `/api/iterations/...`; SSE channel; CF Access protected per §3.4.

7. **`da-observability-cli`** — `[deps: 5]` — new commands: `da observability login`, `da observability sync`, `da observability status`. Read endpoint from `.agentsrc.json`, secret from `~/.config/da/credentials.json`.

8. **`workflow-push-hook`** — `[deps: 7]` — wire `da workflow checkpoint` + `da workflow verify record` to POST live events when `.agentsrc.json` has `observability.enabled: true`. Best-effort; queues on failure.

9. **`obs-historical-backfill`** — `[deps: 7]` — `da observability sync --full` walks `.agents/history/<plan>/` and replays everything. Server dedupes.

10. **`cf-access-iac-migrate`** — `[deps: 1, eventually]` — once a third CF Access app exists, port the manual bootstrap to `cloudflare-go` or Terraform. Until then, `docs/cf-access-bootstrap.md` is the source of truth.

**Wave shape:** waves 1-3 are "land the gating" (small, can ship together); waves 4-6 are "land the obs server" (bigger); waves 7-9 are "wire the CLI" (depend on server existing); wave 10 is "hardening."

---

## §8 — Open questions

Restated from task notes + new ones surfaced:

### Resolved by this proposal

- ~~**Should internal docs require team membership (CF Access team rule) or per-email (CF Access email rule)?**~~ — §3.5: per-email allowlist for v1; widen to GitHub org membership when AGOrcha org has maintainer membership records.
- ~~**Per-doc flag vs path-based vs manifest?**~~ — §1.3: per-doc frontmatter `visibility`, default internal.
- ~~**Subdomain vs path for internal/obs?**~~ — §2.3: internal as path on `agorcha.dev`; obs as subdomain `obs.agorcha.dev`.
- ~~**DO+D1 vs alternatives?**~~ — §4.5: hybrid DO+D1.
- ~~**Push vs pull?**~~ — §5.2: hybrid (push live + manual sync catch-up).
- ~~**Where does the bearer token live?**~~ — §5.4: `~/.config/da/credentials.json` (0600).

### Still open

- **Does observability data ever need to be public** (e.g., aggregated cross-project metrics visible without auth)? Current proposal says no. If the answer becomes "yes" later, add an unauth `obs.agorcha.dev/public-stats` route serving D1 aggregate views only. Defer.
- **Multi-account CF model**: are any managed projects ever owned by different CF accounts? Probably no for v1. If yes later, the audTag-per-app pattern generalises but the D1 has to be partitioned per CF account. Defer.
- **Schema versioning for the ingest payload**: the proposal reserves `schema_version: 1`; the rule "server accepts versions it understands" needs to be explicit about behaviour for unknown versions (reject vs warn-and-store-raw). Recommend reject-with-clear-error so the CLI knows to upgrade. Confirm at impl time.
- **`.agentsrc.json` `observability` block**: this is a new top-level field. Per `~/.agents/rules/dot-agents/schema-usage.md` AgentsRC field lifecycle, adding it requires: struct + core mirror + UnmarshalJSON + MarshalJSON + `agentsRCKnown` map + `schemas/agentsrc.schema.json` update. Track as a sub-task of wave 7.
- **Where the obs Worker source lives**: `obs/` at repo root (sibling of `docs/`) vs `docs/web/obs/` (sub-route of the docs site)? Recommend `obs/` at repo root — different deploy target (Worker + DO + D1, different Wrangler config) — but ratify at scaffolding time.
- **CF Access cookie scope when crossing `agorcha.dev` ↔ `obs.agorcha.dev`**: a maintainer logged in to one is not automatically logged in to the other unless the CF Zero Trust team's auth domain spans both. Confirm during §3.4 bootstrap that the auth domain is set to `.agorcha.dev` (apex) so the cookie covers both subdomains.
- **CLI fallback when offline / `obs.agorcha.dev` unreachable**: §5.2 says queue to `.agents/active/obs-outbox/`. Need to spec the outbox file format (one event per file? one batched file?) and the retention policy (forever vs N days). Defer to wave 8 impl.
- **Push at every iteration close vs throttled**: at very high iteration rates (rapid-fire ralph loops) push-on-every-event could hit the Workers free-tier 100k req/day. Add a `observability.push_throttle_seconds` knob (default 0 = no throttle) to coalesce events at the CLI side. Defer to wave 8.

---

## Tone + status reminder

This is a project-local proposal per `~/.agents/rules/dot-agents/proposal-routing.md` (not a global proposal — it targets this repo's deploy topology + the agorcha.dev surface, not a shared `~/.agents/` resource). It is not yet a formal spec per the workflow artifact model — the maintainer reviews + ratifies, and only then does §7's wave 4 promote the relevant slices to `workflow/specs/<id>/design.md`.

Where this proposal says **RECOMMENDED**, that is the decision the maintainer can ratify with a single "ok, do that"; if any are wrong, amending them here before any of §7 fans out costs near-zero.
