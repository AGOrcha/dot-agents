# obs/ — observability dashboard Worker (obs.agorcha.dev)

The remote deploy target for the workflow observability dashboard. A **single-tenant**
Cloudflare Worker that ingests, stores, and serves this repo's iteration/score
telemetry, fronted by Cloudflare Access. Distinct deploy target from `docs/web`
(the internal docs site) — separate Worker, own domain.

Canonical contract: `.agents/workflow/specs/obs-dashboard-cf-deploy/design.md`
(contracts D1–D6). Access apps + audTags: `docs/cf-access-bootstrap.md`.

## Architecture

- **Worker** `agorcha-obs` on `obs.agorcha.dev` (`wrangler.jsonc`, `run_worker_first`
  fail-closed). `src/index.ts` dispatches the versioned `/api/v1/observability/*`
  surface (the same routes the local Go dashboard serves) plus `POST …/ingest`.
- **`ProjectDO`** (Durable Object) — one per `project_id` (`idFromName`); serializes a
  project's writes, holds the last-100-event buffer + hibernating WebSocket
  subscribers, and broadcasts only after the D1 commit.
- **D1** (`OBS_DB`) — append-only `iterations`/`scores`/`score_breakdown` + the
  replaceable `sessions` read projection + `rubrics`. Schema in `migrations/`
  (forward-only; applied by `wrangler d1 migrations apply`, never at request time).
- **Auth** (`src/auth-gate.ts` + `src/access-jwt.ts`) — CF Access is the sole
  production authenticator; the Worker validates only the injected
  `Cf-Access-Jwt-Assertion` JWT (RS256, JWKS, iss/aud/exp/nbf), fail-closed on every
  route incl. static assets + WebSocket upgrade. Ingest then enforces
  `project_id == OBS_PROJECT_ID` (foreign projects rejected).
- **Frontend** — the existing `web/dashboard` SPA, built with the WebSocket
  transport (`build:obs`) and served via the `ASSETS` binding.

## Single-tenant

`obs.agorcha.dev` serves only `github.com/AGOrcha/dot-agents` (`OBS_PROJECT_ID`).
Every repo points its own `.agentsrc.json` `observability.endpoint` at its own backend;
this is a reference instance, not a multi-project router.

## Local development

```sh
npm install
npx wrangler d1 migrations apply agorcha-obs-db --local
node scripts/fixture-dev.mjs --local          # fixture-JWT auth, ephemeral RSA keypair
```

`OBS_AUTH_MODE=fixture-jwt` is honored ONLY with `ENVIRONMENT=development` AND a
loopback host (defense in depth) — production ships `OBS_AUTH_MODE=access`. The CLI
uses the `DA_OBS_TEST_JWT` seam over loopback; a real `credential-ref` is never
resolved for an `http:` URL. `npm test` runs the auth/verifier unit suite;
`npm run typecheck` + `npx wrangler deploy --dry-run` validate config.

## Deploy (maintainer)

Needs a scoped `CLOUDFLARE_API_TOKEN` (Workers Scripts + D1 + Account Settings; zone:
Workers Routes + DNS + SSL) minted from the account key and revoked after — see
`docs/cf-access-bootstrap.md` and the `cloudflare-agorcha-account` memory. The remote
`d1_databases[].database_id` is committed in `wrangler.jsonc`.

```sh
npx wrangler d1 migrations apply agorcha-obs-db --remote
pnpm --dir ../web/dashboard run build:obs      # SPA into web/dashboard/dist (ASSETS)
npx wrangler deploy                             # creates the custom domain + DO + routes
```

Verify (real edge): no-token → 302 to CF Access; real service token → 200 + a real
ingest lands in D1 (`da observability status` / `sync`). A fixture/Miniflare smoke
does NOT exercise the real global-`fetch` JWKS path — see lesson
`cf-worker-auth-gate-fail-closed` §6.
