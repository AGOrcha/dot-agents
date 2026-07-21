# obs-dashboard-cf-deploy — implementation results

End-to-end deployment of the single-tenant observability dashboard at
`https://obs.agorcha.dev` (CF Worker + Durable Objects + D1 + CF Access), plus the
`da observability` CLI, workflow push hook, and history backfill. Spec:
`.agents/workflow/specs/obs-dashboard-cf-deploy/design.md` (contracts D1–D6).

## Tasks (12/12 complete)

| Task | PR | What |
|---|---|---|
| o1 spec + provider contracts | #443/#444 | Canonical spec (5 contracts), single-tenant + per-project routing |
| o2 CF Access app | #451 | `agorcha-obs` app + bound service-token policy (Terraform, `-target`) |
| o3 agentsrc observability block | #454 | `.agentsrc.json` observability + credential-ref (Go lifecycle + schema) |
| o4 Worker scaffold | #455 | `obs/` wrangler + DO/D1 bindings + ingest route + DDL |
| o5 ingest DO+D1 | #458 | Idempotent write-through, sessions projection, WS broadcast, read routes |
| o6 auth gate | #461 | Dual-mode fail-closed CF Access JWT verifier (RS256/JWKS/iss/aud/exp/nbf) |
| o7 dashboard frontend | #464 | Injectable `ObservabilityTransport` (SSE local / WS remote); ASSETS serving |
| o8 da observability CLI | #459 | `login\|sync\|status`, outbox drain state machine, credential-ref loader |
| o9 workflow push hook | #463 | Best-effort publish on checkpoint / verify-record (never changes exit) |
| o10 historical backfill | #465 | `sync --full` replay; wipe+rebuild byte/ETag equivalence |
| o11 CF Access IaC posture | #453 | Terraform completeness; per-project-token + retention v1-deferral docs |
| o12 e2e verify + close | #469 (hotfix) | Local live-proof + real go-live; JWKS fetch-bind fix |

Support: #452/#456/#462/#466 (plan-state), #467 (`globalflagcov` register `commands/observability`).

## o12 verification

**`go test ./...`** green (after #467 registered the new command package in the
`globalflagcov` analyzer).

**Local live-proof** (merged master, `wrangler dev` fixture auth) — all 8 done
criteria PASS: config round-trip + https-guard (DC2); ingest dedupe / distinct /
foreign-reject with 1 D1 row (DC3); auth fail-closed incl. `alg:none`/HS256,
wrong-aud, encoded/dupe/case path variants (DC7); reused SPA + `{data,meta}` +
unversioned-alias 404 (DC5); live WebSocket delivery **20.2 ms** (DC6); CLI status +
sync-then-delete + dedupe + hook best-effort isolation (DC4); `sync --full`
wipe+rebuild **byte-identical body + identical ETag** (DC8).

**Real go-live** — deployed to `obs.agorcha.dev`:
- Worker `agorcha-obs`, bindings `PROJECT_DO` (DO), `OBS_DB` (D1
  `df2898b7-ea7b-427e-86f9-30647c7468da`), `ASSETS`; vars production / access mode /
  audTag `5eeb249e…` / team `usepayout.cloudflareaccess.com`. Remote D1 migrated
  (`0001_initial`, `0002_seed_active_rubric`).
- No-token request → **302 to CF Access login** (gate live).
- `da observability status` (real `agorcha-obs-cli` service token) → **reachable +
  authed + HTTP 200**.
- Real service-token ingest → **accepted:1**, re-POST → **deduped:1**, distinct →
  accepted; remote D1 = **2 rows**; authed `/runs` returns the session.

### Production bug caught only by the real edge (fix: #469)

`CloudflareAccessJwksProvider` stored `fetch` on the instance and called
`this.fetcher(...)`; the Workers runtime rejects that with **"Illegal invocation"**,
so every real CF Access JWKS verification threw and the Worker returned 403 even
though Access injected a valid assertion (`present=true len=845`). Local/fixture
smokes used `StaticJwksProvider` + test doubles and could not surface it — the
real-edge deploy was the only test that could. Fixed by binding fetch to
`globalThis`; added a regression test asserting the provider never calls fetch with
itself as `this`. Live re-smoke after the fix: 200 + accepted/deduped as above.

## Deferred (per spec)

Public unauthenticated stats; multi-account/multi-tenant routing; per-project tokens
within one backend; R2 raw transcripts; browser-driven token provisioning; `da
service` credential proxy; event replay/cursors; bidirectional WS commands;
retention configurability (v1 constants fixed).
