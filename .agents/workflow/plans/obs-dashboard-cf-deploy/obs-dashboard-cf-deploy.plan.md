# Observability dashboard: Cloudflare Worker deploy at obs.agorcha.dev

Planned 2026-07-18 from the maintainer-reviewed proposal
`.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` (obs waves 4-10 of its §7),
at the maintainer's request to schedule the full CF-Worker obs deploy into the task graph.

## Grounding

- **Design source:** the proposal (RECOMMENDED decisions, maintainer feedback folded via
  PR #156; not `da review`-ratified, ratified-to-build by this request). o1 formalizes it into
  a canonical spec.
- **Already landed (waves 1-3, NOT in this plan):** the `agorcha.dev/internal/*` CF Access
  gate — `docs/web/src/worker.js` has the `/internal` branch; `infra/cloudflare/` Terraform
  declares App 1 (`agorcha-internal-docs`). This plan is the **obs** side (App 2 + the obs
  Worker + the CLI), which is unstarted.
- **Coexists with, does not conflict, `dashboard-subsystem-and-bus-security`:** that plan owns
  the LOCAL dashboard (SSE, loopback, `da dashboard`/`da service` mount + slim runtime). This
  plan is the REMOTE multi-project deploy (Worker+DO+D1, WebSocket, CF Access, ingest). Proposal
  §4.1 keeps local SSE and uses WebSocket only for the remote deploy.

## Decisions (from the proposal)

- **CORRECTION (maintainer, 2026-07-18) — single-tenant per deployment.** `obs.agorcha.dev`
  serves ONLY dot-agents; it is NOT the proposal's central multi-tenant hub. Routing is
  per-project + client-side: each repo's `.agentsrc.json` `observability.endpoint` targets its
  OWN backend (dot-agents → obs.agorcha.dev; payout → payout's configured backend). The ingest
  schema + CLI + config stay generic/self-hostable; obs.agorcha.dev is the reference instance.
  This drops the DO-per-project fan-out + per-project token issuance from THIS deployment.
- **Topology (§2.4):** `obs.agorcha.dev` is its own Worker + its own CF Access app, dual-auth
  (CF Access JWT for browser, service token for CLI POST).
- **Runtime (§4.5, single-tenant):** Worker + a single Durable Object for live state + D1 for
  history, scoped to dot-agents; `project_id` retained as a column/idempotency-key component for
  portability + a defensive foreign-project reject. R2 deferred. Free-tier trivially sufficient.
- **Data flow (§5.2):** hybrid - best-effort push on checkpoint/verify-record, `da observability
  sync` for catch-up; no cron.
- **Auth (§5.4):** reuse the external-agent-sources credential model (auth by credential-ref;
  secret in ~/.config/da/credentials.json 0600). No parallel obs-specific loader.
- **Source of truth (§6.1):** local `.agents/history/` canonical; remote D1+DO is a derived
  cache (wipe + resync rebuilds).

## Task graph (concurrent-folded; dep-ordered)

Prereqs (no deps): `o1-obs-deploy-spec` (formalize spec), `o2-obs-cf-access-app` (Terraform CF
Access App 2; maintainer-must to apply), `o3-obs-agentsrc-schema` (observability config block).
Server: `o4-obs-worker-scaffold` [o1,o2] -> `o5-obs-ingest-do-d1` [o4] + `o6-obs-auth-gate`
[o4,o2] -> `o7-obs-dashboard-frontend` [o5,o6]. CLI: `o8-da-observability-cli` [o3,o4] ->
`o9-workflow-push-hook` [o8] + `o10-obs-historical-backfill` [o8,o5]. Hardening:
`o11-obs-cf-access-iac-hardening` [o2]. Close: `o12-obs-verify-close` [o5,o6,o7,o9,o10,o11].

## Cross-plan relations

- **Consumes** `dashboard-subsystem-and-bus-security` (the shared dashboard UI/core; o7 reuses
  it) and `r2-observability-dashboard` (the existing SPA + read model).
- **Consumes** `external-agent-sources` (the credential-ref auth shape; o3/o8) and
  `dm6-da-sso-autowire` (CF Access service-token provisioning; o8).
- **Extends** the landed `infra/cloudflare/` (App 1) with App 2 (o2/o11) and the docs Worker
  deploy pattern (`deploy-docs.yml`, `rotate-cloudflare-deploy-token.yml`).

## Verification standard (live smoke)

Same standard as the dashboard plan: a task is DONE only when the shipped Worker is spun up
(`wrangler dev` miniflare DO+D1, then a `wrangler deploy` PREVIEW) and exercised with
machine-checkable assertions - POST /api/ingest -> {accepted,deduped}; auth 401-without /
200-with a service token; wrong-aud JWT rejected; encoded-path variants gate; a WebSocket live
update < 2s; D1 dedup on re-POST; local-canonical rebuild (wipe remote + `sync --full`). CLI/
config via `go test`; CI green. `o12` runs the full set end-to-end against the preview deploy.

## Status

Draft. `o2` (CF Access apply) and any production `wrangler deploy` are maintainer-must
(need Cloudflare credentials); the rest is delegable. Activate after the proposal's obs
direction is confirmed and `dashboard-subsystem-and-bus-security` (the shared core) is landing.
