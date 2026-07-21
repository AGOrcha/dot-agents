# Auth-gating a Cloudflare Worker + static assets: fail-closed checklist

Lessons from an adversarial security review of the agorcha.dev docs CF Access
gate (Option A: one Worker, one static-assets dir, `/internal/*` gated by JWT).
Each item below was a REAL exploitable gap, not theory.

## 1. Static assets BYPASS the Worker by default — use `run_worker_first: true`

With a static-assets binding, an existing asset (e.g. `dist/internal/lessons/
index.html`) is served DIRECTLY, never reaching `worker.js` — so a path-based
JWT gate in the Worker is silently skipped. `run_worker_first` forces the
handler first.

- The **per-route array** form (`run_worker_first: ["/internal/*"]`) is a trap:
  it does NOT match the bare `/internal`, and glob matching against
  encoded/non-canonical variants is unreliable. Prefer `run_worker_first: true`
  (global). Per CF docs it "unconditionally invokes your Worker script" for
  every request even when an asset matches; the Worker then fast-paths public +
  allowlisted requests to `ASSETS` itself, so the only cost is one cheap
  classification.
- VERIFY it live: a unit test against a stubbed `ASSETS` does NOT prove the
  routing. Use **Miniflare** with `assets.routerConfig.invoke_user_worker_ahead_
  of_assets: true` (NOT `assetConfig.*`, and `has_user_worker: true`) and assert
  unauth `GET /internal/<existing-asset>` -> 401. Without that flag Miniflare
  serves the asset (200) or 404s before the worker — which is exactly the prod
  leak, so the smoke is meaningful.

## 2. Classify paths FAIL-CLOSED after decode+normalize

`URL().pathname` decodes ONE layer. A naive `pathname.startsWith('/internal/')`
is bypassed by `/INTERNAL/`, `/internal%2fsecret` (%2f -> literal '/'),
`/internal%252f..` (double-encoded, residual '%'), `//internal`, `/a/../internal`,
NUL, backslash. RULE: normalize and treat ANY non-canonical path as gated
(return the gated class), never as public:

- reject (-> gated) if, after URL's decode pass, the path still contains `%`
  (double-encoded), a NUL, a backslash, an empty segment (`//`) other than one
  trailing slash, or a `.`/`..` segment;
- lower-case before prefix comparison (case-insensitive `/internal`);
- check the gated prefix BEFORE any public carve-out (defense in depth).

## 3. Never carve out a whole directory as "public" — allowlist

A blanket public `/schemas/*` exposed INTERNAL operational schema shapes
(delegation-bundle, dashboard-run, verification DTO fields = auth/config keys).
RULE: an explicit PUBLIC allowlist (a `Set` of exact filenames). A new file is
internal until listed. The bare directory listing is gated too (it enumerates
internal filenames).

## 4. Bearer-equivalent headers are HTTPS-ONLY (client side)

A client that attaches service-token headers (`CF-Access-Client-Id/Secret`) must
refuse non-HTTPS. RULE: in the request decorator, if the gated request's
`URL.Scheme != "https"`, hard-error BEFORE resolving the credential (so the
secret is never even read on a transport that would expose it). The transport
wrapper must abort the round trip on that error (never fall through to send).

## 5. Hand-rolled JWT verification — the non-negotiables

If you verify a JWT with WebCrypto (no library), it MUST:
- pin `alg === 'RS256'` (reject `none` AND HS* — symmetric-key confusion where
  the attacker HMACs with the public key);
- require a present, non-empty `kid` and match it to a JWK by exact string
  (never let `undefined === undefined` match a kid-less JWK);
- require the matched JWK `kty === 'RSA'` and, if it declares `alg`, `=== RS256`;
- enforce `exp`, `nbf`, `iss` (== `https://<team>`), `aud` (includes the audTag);
- FAIL CLOSED on: missing/placeholder config, JWKS fetch failure, importKey
  failure, verify throwing, and an invalid signature.
Test each rejection path with a hand-built token (alg=none, HS256, missing kid,
wrong aud/iss, expired) asserting the specific failure reason.

## 6. The JWKS fetcher must be called with global `this` (Workers footgun)

A JWKS provider that stores `fetch` on the instance and calls it as a method —
`this.fetcher = fetch` then `this.fetcher(url)` — throws **"Illegal invocation:
function called with incorrect `this` reference"** in the Workers runtime, because
`fetch` requires its `this` to be the global object. Via §5's fail-closed rule that
thrown error becomes a **403 on a VALID assertion**: Access injects a correct JWT,
the Worker rejects it, and the whole gate looks broken for authenticated callers.
RULE: bind global builtins to the global scope when storing them —
`this.fetcher = fetcher.bind(globalThis)` (a no-op for arrow/closure test doubles,
the fix for the real global `fetch`). This applies to any global builtin captured
as a field then called via `this.`.

This bug is INVISIBLE to fixture/unit smokes: local fixture mode uses a
`StaticJwksProvider` (no fetch) and unit tests inject an arrow-function fetcher
(arrows ignore `this`), so both pass green — only the real Cloudflare Access edge,
hitting the real global `fetch` for the real JWKS endpoint, surfaces it. Caught only
by the obs-dashboard-cf-deploy real go-live deploy (see §Meta).

## Meta

A unit suite over a stubbed binding proves the HANDLER logic; it cannot prove the
RUNTIME routing (assets-vs-worker, base-path, redirects). For an auth gate, add a
real-runtime smoke (Miniflare/wrangler) — a `serve dist` static server bypasses
the Worker entirely and would pass a broken gate.

But even a Miniflare/fixture smoke has a blind spot: the **real Cloudflare Access
edge** (production/preview deploy behind the actual Access app, exercised with a
**real service token**) is the only test that runs the real global `fetch`, the real
JWKS fetch, and Access's real assertion injection. §6's `this`-binding bug shipped
through green unit + green fixture Miniflare smokes and was caught ONLY on the live
`obs.agorcha.dev` deploy. RULE for edge-auth features: **local-proof THEN go-live** —
treat a real-edge deploy smoke (no-token → 302/403; real token → 200 + a real write;
dedupe) as a mandatory verification stage, not optional. Sibling of
[[live-smoke-must-run-on-every-target-os]] extended to the network-auth edge.
