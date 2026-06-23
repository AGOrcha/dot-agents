# da-side SSO auto-wire — design (spec)

**Spec id:** `dm6-da-sso-autowire`
**Status:** design artifact (spec tier — what & why, decisions, open questions, done criteria). A plan (`workflow/plans/dm6-da-sso-autowire/`) follows once this stabilizes.
**Tier:** spec, per workflow-artifact-model. This document deliberately carries NO file paths, function names, or task breakdown — those land in the plan.
**Parent:** `dm6-agent-internal-access` (TASKS.yaml, docs-starlight-migration). This spec covers the second half of dm6: the developer-facing self-provision + auto-wire. The client half (attach-only) shipped in PR #83; the Worker `/internal/provision` minting route shipped in PR #93.
**Gating contract this is designed against:** spec `docs-starlight-migration` D5/D8; the Worker `/internal/provision` route (PR #93); the `internal/docsaccess` client half (PR #83); the `credstore` loader/store (external-agent-sources).

---

## 1. Problem & why

A developer who works on dot-agents needs to read the maintainer-only `/internal/*` docs surface (Lessons, Specs, Proposals, internal design) from their agents. The edge gates that surface behind a Cloudflare Access service token: the request must carry `CF-Access-Client-Id` / `CF-Access-Client-Secret` headers.

Two of the three pieces now exist:

- **Attach (PR #83).** `internal/docsaccess` already decorates outbound `/internal/*` requests to the docs host with the two CF-Access headers, sourced from the credstore credential ids `cf-access-client-id` / `cf-access-client-secret`, fail-loud when either is missing, and refuses to attach over a non-HTTPS transport.
- **Mint (PR #93).** The docs Worker exposes `POST https://agorcha.dev/internal/provision`, gated by the same CF Access JWT (GitHub SSO) as the rest of `/internal/*`. On success it mints a *per-user* CF Access service token (named `agorcha-agents-<github-login|email-localpart>`) via the CF API using a server-held scoped token, and returns `{ client_id, client_secret, name }`. It is fail-closed (401 unauth, 405 wrong method, 422 no-identity, 503 mint-unconfigured, 502 CF API error).

**The gap (this spec).** There is no path that gets a developer from "I have a GitHub login" to "my agents can read `/internal/*`". Today a developer would have to hand-obtain a JWT, hand-POST `/internal/provision` with curl, copy the returned secret, and hand-write it into the encrypted credstore under the two magic ids. No `da` command writes the credstore at all yet. This spec designs the one interactive command that closes the loop: authenticate the developer via CF Access, call the minting route, store the returned pair under the ids `docsaccess` already consumes, and ensure every subsequent `da` run (including `da init` / `da refresh`) picks them up with zero further steps.

**Why it matters:** D8 promises the credential is "auto-wired into dot-agents developers' setup so their agents get internal-docs access without hand-managing tokens." Without this command, D8 is unfulfilled — the attach + mint halves are inert without a self-service bridge.

**Why now / why it's bounded:** The hard prerequisites (Access app + policies, the JWT gate, the minting route, the attach client, the encrypted store) all exist. What remains is a single interactive developer workflow. Headless/CI auto-provision is explicitly out of scope (see §10).

---

## 2. The core question: how does a CLI obtain a CF Access JWT for a browser-SSO app?

The Worker's gate verifies a Cloudflare Access **application JWT** (issued after GitHub SSO), not a service token. So `da` must present such a JWT to `/internal/provision`. The CLI is not a browser. Three ways to bridge that:

### Option A — Delegate to `cloudflared access` (RECOMMENDED)

`cloudflared` is Cloudflare's own client for exactly this. `cloudflared access login <app-url>` opens the system browser, runs the IdP (GitHub) SSO flow, and on success caches an app-scoped JWT at `~/.cloudflared/<domain>-token`, valid for the session duration the Access policy sets. `cloudflared access token --app=<app-url>` then prints the cached JWT to stdout (re-triggering login if absent/expired). The JWT is presented to the origin as the `cf-access-token` request header (equivalently the `CF_Authorization` cookie) — which is exactly what the Worker's JWT verifier reads.

`da` shells out: ensure a fresh token (`access login` if needed), read it (`access token`), then POST `/internal/provision` with `cf-access-token: <jwt>`.

- **Pros:** Uses the *official, supported* SSO client — no bespoke OAuth/device-code code to maintain, no IdP secrets in `da`. Token caching, refresh-on-expiry, and the browser handshake are Cloudflare's problem. The JWT shape is guaranteed to satisfy the Worker gate (same tool the gate is designed for). Matches the team's stated direction (dm6 notes: "da runs the CLI device-flow yielding the user's CF Access identity"). Smallest surface in `da`.
- **Cons:** Hard dependency on `cloudflared` being installed and on `PATH` (extra prerequisite; cross-platform availability varies — see §8). Parsing/handling another process's exit codes and stderr. The cached-token file is a (short-lived) bearer secret on disk owned by cloudflared, outside `da`'s control.

### Option B — `da` implements the OAuth/device-code flow itself

`da` registers/uses an OAuth client against the Access app and runs a device-code or loopback-redirect flow directly (open browser to the Access login URL, capture the JWT via `http://localhost:<port>/callback` or a device-code poll).

- **Pros:** No `cloudflared` dependency. Full control over UX and where the token lives.
- **Cons:** Largest implementation + maintenance surface (OAuth client registration, PKCE, loopback server or device-code polling, IdP-specific quirks, token verification). Re-implements what `cloudflared` already does correctly. Higher security-review burden (we'd own the token handling end to end). Cloudflare's Access app-token issuance is not a documented stable public OAuth endpoint to build against — fragile.

### Option C — Ask the user to paste a token

Prompt the developer to run `cloudflared access token` (or grab the JWT from the browser) and paste it into `da`.

- **Pros:** Zero browser/process orchestration in `da`. Works anywhere a JWT can be produced.
- **Cons:** Poor UX (manual copy of a long-lived-ish bearer secret through the terminal/clipboard — easy to leak into shell history/scrollback). Still requires `cloudflared` or equivalent to *produce* the token, so it inherits Option A's dependency without Option A's automation. Error-prone (truncated/expired pastes).

### Recommendation

**Option A (delegate to `cloudflared access`), with Option C as an explicit manual fallback.** It is the lowest-code, lowest-risk path that reuses Cloudflare's own SSO client and aligns with the already-ratified dm6 direction. `da` orchestrates `cloudflared` for the SSO leg and owns only the `/internal/provision` POST and the credstore write. When `cloudflared` is absent or the developer prefers not to install it, `da` falls back to a paste prompt (Option C) and/or prints the exact `cloudflared` commands to run — so the feature degrades gracefully rather than hard-failing. Option B is rejected as speculative re-implementation.

---

## 3. Command surface (decision: name, shape, UX)

**Decision: a new `da docs login` command** (the docs-access counterpart to the obs proposal's `da observability login`). Rationale: it is discoverable, scoped to the docs-access concern, verb-consistent with the established `<noun> login` pattern in the obs proposal, and does not overload `da init`/`da refresh` with an interactive browser flow (those must stay non-interactive — see §5). A `--flag on da init` was considered and rejected: init runs in scaffolding/CI contexts where an unexpected browser pop is hostile; provisioning is a deliberate, occasional act, not part of every init.

Behavioral requirements for the command (not its flags' exact spelling — that's plan tier):

- **Default (interactive):** run the CF Access SSO leg (Option A), POST `/internal/provision`, store the returned pair, confirm success. Idempotent and safe to re-run (§7).
- **A way to opt out of the auto-SSO and paste a JWT** (Option C fallback) for environments without `cloudflared`.
- **A status/check mode** that reports whether the credstore currently holds usable docsaccess credentials and whether they authenticate, WITHOUT minting — so a developer can diagnose before re-provisioning.
- **Honor the existing global flags** the command tree already threads (dry-run, yes/non-interactive, verbose, force) consistently with sibling commands. Dry-run must describe what would happen and mint nothing.
- **Host override:** respect the same docs-host override `docsaccess` honors (`DA_DOCS_HOST`) so the command works against staging/preview, keeping the SSO target and the provision target in lockstep.

**Output (UX requirements):**

- Print the minted token's *name* and the credential ids it was stored under. NEVER print `client_secret` (or `client_id` beyond what's needed to confirm). Consistent with the client half's "secret value is never logged" posture.
- On success, tell the developer they are done — no manual edit, no env var to set — and that `da` (incl. init/refresh) will now reach `/internal/*`.
- On each failure mode from the Worker, map the status to a specific, actionable message (see §9). Never dump a raw secret or a raw JWT on error.

---

## 4. End-to-end flow (sequence)

1. Developer runs the provision command.
2. `da` determines the provision/SSO target from the configured docs host (default `agorcha.dev`, override `DA_DOCS_HOST`) and derives the Access app URL for the `/internal/*` surface.
3. **SSO leg (Option A):** `da` ensures a valid app JWT exists (triggering the browser login if needed) and reads the cached JWT. If `cloudflared` is unavailable, `da` falls back to the paste prompt (Option C) or prints exact instructions.
4. **Provision leg:** `da` POSTs to `/internal/provision` over HTTPS, presenting the JWT in the header the Worker gate reads. The Worker verifies the JWT, derives `agorcha-agents-<slug>`, mints (or the CF API returns) the per-user service token, and responds `{ client_id, client_secret, name }`.
5. **Store leg:** `da` writes `client_id` → `cf-access-client-id` and `client_secret` → `cf-access-client-secret` into the encrypted credstore (the same store/ids the attach client resolves), atomically (both or neither).
6. **Confirm:** `da` reports success with the token name; optionally verifies by issuing one authenticated `/internal/*` request and asserting it is not a 401/login-redirect.
7. **Auto-wire (no further action):** the next time any `da` code path (including `da init` / `da refresh`, or an agent) hits the docs host `/internal/*`, the attach client resolves the two ids from the store and attaches them. No env var, no config edit.

```
developer            da CLI              cloudflared        Worker /internal/provision      CF API
   |  da docs login    |                      |                       |                       |
   |------------------>|  access login/token  |                       |                       |
   |                   |--------------------->| (browser SSO, GitHub) |                       |
   |   browser SSO     |<---------------------|  app JWT (cached)     |                       |
   |                   |  POST /internal/provision  (cf-access-token: JWT, HTTPS)             |
   |                   |------------------------------------------->| verify JWT, derive name |
   |                   |                      |                       |  mint service token   |
   |                   |                      |                       |---------------------->|
   |                   |                      |                       |<--- {id, secret} -----|
   |                   |<-- 200 {client_id, client_secret, name} ----|                       |
   |                   |  store both under credstore ids (atomic)    |                       |
   |  success (name)   |                      |                       |                       |
   |<------------------|                      |                       |                       |
```

---

## 5. Credential storage + auto-wire touchpoints

**Storage:** the returned pair is written to the existing encrypted credstore under the *exact* ids the attach client already resolves (`cf-access-client-id`, `cf-access-client-secret`). No new storage format, no new file, no new loader — the store and its OS-keychain-wrapped encryption are reused as-is. The write is all-or-nothing so the store never holds a half-set pair (mirrors the attach client's "never half-set" invariant).

**Auto-wire (the key property):** because the attach client resolves these ids at request time through the standard loader chain (env → plaintext file → encrypted store → resolver), *no wiring change is needed* for subsequent runs to pick the creds up — writing the store IS the auto-wire. The touchpoints to confirm (not necessarily to modify):

- **`da init`** must NOT trigger provisioning (no browser in scaffolding). It MAY, at most, surface a non-fatal hint that internal-docs access is unprovisioned and point at the provision command. Init succeeds regardless.
- **`da refresh`** likewise must remain non-interactive and must not mint. If a refresh path fetches `/internal/*`, it relies on the already-stored creds via the attach client; a missing-credential error there is fail-loud (existing behavior), with a hint to run the provision command.
- **The provision command** is the only writer of these ids.

**Decision:** auto-wire is achieved purely by writing the two credstore ids; init/refresh are NOT taught to provision. Their only change (if any) is an optional diagnostic hint.

---

## 6. Idempotency, rotation, revocation

This is the subtle part because the Worker mints by a *fixed per-user name* (`agorcha-agents-<slug>`), and the CF API returns `client_secret` ONLY at creation time.

**Decisions / requirements:**

- **Re-provision is the supported rotation path.** Running the command again is safe and yields a working credential. After it completes, the developer's local store holds a credential that authenticates.
- **Old-token revocation must be addressed, not ignored.** Because re-minting under the same name may either (a) collide/error on the CF API or (b) create a second token, leaving a stale token live, the design must converge on "one live token per developer." The desired end state after a re-provision: exactly one valid `agorcha-agents-<slug>` token, and the local store holds its secret. Whether the *Worker* enforces this (delete-then-create / rotate-secret on the existing token) or the *client* drives it (a force/rotate mode) is an open question (§9, OQ-2) — but the spec REQUIRES that re-provision does not silently accumulate orphan tokens.
- **Lost secret recovery:** because the secret is only shown at creation, a developer who lost their local secret cannot "re-read" it — recovery is necessarily a rotation (mint a new secret/token), which must invalidate the old one. The command's re-run path is this recovery path.
- **Deprovision / explicit revocation** (delete my token, stop authenticating) is desirable for offboarding. The policy accepts *any valid* service token, so revocation = delete that developer's token at the edge. Whether `da` offers a deprovision command in v1 or this stays a maintainer action is an open question (§9, OQ-3). At minimum the spec notes that deletion of the named token is the revocation lever.
- **Local clear:** removing the two ids from the local store disables `da`'s access without revoking the edge token. The command (or a sibling) should support clearing local creds.

---

## 7. Security considerations

- **Never log secrets.** Neither `client_secret`, `client_id` (beyond confirmation), nor the CF Access JWT may appear in logs, dry-run output, error messages, or verbose mode. This extends the client half's stated discipline ("the secret value is never logged; only the credential id and the gating decision are surfaced").
- **HTTPS-only.** The `/internal/provision` POST and any verification request must be HTTPS — consistent with the client half's `ErrInsecureScheme` posture (CF-Access headers and the JWT are bearer-equivalent secrets; refuse plaintext). A non-HTTPS docs host must fail loudly before any secret/JWT is transmitted.
- **JWT handling.** The JWT is a short-lived bearer credential. `da` should hold it only for the duration of the provision call and not persist it itself (the cloudflared cache is cloudflared's; `da` does not copy it elsewhere). Paste-fallback input must be read without echoing to scrollback where feasible.
- **At-rest.** The minted pair lands in the existing encrypted, keychain-wrapped store (0600 discipline) — no plaintext drop.
- **No mint-anything secret in the binary.** Already enforced server-side (the scoped CF API token is a Worker secret, per dm6 notes / PR #93). `da` never holds a mint-capable credential — it only holds its own per-user service token. This spec must not introduce any client-side privileged key.
- **Least disclosure on failure.** Map Worker statuses to messages without revealing whether the endpoint exists to an unauthenticated caller (the Worker already returns 401 pre-method-check; `da` should not paper over that).

---

## 8. Cross-platform notes

- **`cloudflared` availability:** macOS (brew), Linux (pkg/binary), Windows (binary/winget) all have `cloudflared`, but it is not preinstalled. The command must detect its absence and degrade to the paste fallback (Option C) with a clear install pointer, rather than crash. Detection and the fallback are first-class, not an afterthought.
- **Browser launch:** opening the system browser differs per OS; delegating the browser leg to `cloudflared` (Option A) sidesteps `da` needing its own cross-platform browser-open logic. The paste fallback covers truly headless-but-interactive shells (SSH) where no browser is reachable — the developer runs `cloudflared access login` on a machine with a browser and pastes the JWT.
- **Keychain:** the encrypted store's key is wrapped by the OS keychain, which exists on macOS and Windows (per the credstore build tags) but NOT on the non-darwin/non-windows path today, where the encrypted-store step may be disabled. On such platforms the store-write target may be unavailable — the command must detect this and fail with a clear message (or fall back to a supported credstore write path) rather than silently succeed without persisting. This constraint is inherited from credstore, not introduced here, but the command must surface it (§9, OQ-4).

---

## 9. Error / edge cases

- **`cloudflared` missing / not on PATH** → degrade to paste fallback + install hint; do not crash.
- **SSO leg fails / user cancels browser** → clear "authentication did not complete" message; mint nothing; store unchanged.
- **Worker 401** (JWT invalid/expired) → "your Access session is not valid; re-run login"; do not disclose endpoint internals.
- **Worker 422 no-identity** → "your Access identity has no usable GitHub login or email claim; contact a maintainer about the Access policy."
- **Worker 503 mint-unconfigured** → "the server is not configured to mint tokens yet (maintainer must set the Worker secret)"; this is a server-side gap, not a developer error.
- **Worker 502** (CF API error/unreachable/incomplete) → "the minting backend failed; retry later"; surface the Worker's reason code, never a secret.
- **Re-provision collision** (token name already exists at CF) → resolved per OQ-2; must not strand an orphan or leave the developer without a working secret.
- **Partial store write** → atomic; on failure the store is left as it was and the command reports the mint succeeded but local persistence failed (with the recovery being re-run, since the secret is now unrecoverable — flag this loudly).
- **Non-HTTPS docs host** → refuse before transmitting JWT/secret (ErrInsecureScheme parity).
- **Keychain/store unavailable on platform** → clear failure (OQ-4).
- **Verification request after store** returns 401/login-redirect → warn that the minted token did not authenticate (possible policy mismatch), keep the stored creds, point at diagnosis.

---

## 10. Deferred / out of scope

- **Headless / CI auto-provision.** Per the dm6 handoff ("headless deferred"). CI could later use GitHub OIDC (no shared secret) to obtain identity, and a production headless agent may need a different mechanism (TBD). Do not build speculatively. This spec is the *interactive developer* path only.
- **The `da service` auth-proxy injection layer** (proposal §5.5). Direct-attach via the credstore (PR #83) stands; the proxy is a later refactor and does not change this spec's storage contract.
- **Auto-renewal of the service token.** Per-user service tokens are long-lived; rotation is manual re-provision (§6). A background renewer is out of scope.
- **Generalizing to non-docs credentials** (obs, sources). The obs proposal's `da observability login` is a sibling, not this work; if a shared credential-write/login path emerges, both benefit — but this spec only commits the docs-access flow.

---

## 11. Done criteria (verifiable)

1. A developer with a valid GitHub identity, starting from a machine with no docsaccess credentials, can run a single `da` command and end with a working `agorcha-agents-<slug>` token whose `client_id`/`client_secret` are stored under `cf-access-client-id` / `cf-access-client-secret` in the encrypted store.
2. Immediately afterward, an authenticated `/internal/*` request through the attach client succeeds (not 401 / not a login redirect) with NO further developer action — demonstrating auto-wire.
3. `da init` and `da refresh` run non-interactively (no browser) and never mint; they consume the stored creds via the existing attach client and, when creds are absent, fail loud or hint (existing behavior) rather than hang on a prompt.
4. Re-running the provision command yields a working credential and does NOT leave an orphaned/duplicate live token for that developer (one live token per developer — per OQ-2 resolution).
5. No secret (`client_secret`, `client_id` beyond confirmation) and no JWT appears in any output, log, dry-run, or error path; provisioning over a non-HTTPS host is refused before any secret/JWT leaves the process.
6. When `cloudflared` is absent, the command degrades to a documented manual path instead of crashing.
7. Each Worker failure status (401/422/503/502) produces a distinct, actionable message.
8. Dry-run mints nothing and writes nothing, but describes the flow.

---

## 12. Open questions

- **OQ-1 (auth mechanism confirmation).** Confirm Option A is acceptable as a hard-ish dependency on `cloudflared` (with Option C fallback), versus the team wanting a zero-extra-binary path. The dm6 notes say "device-flow"; `cloudflared access` is the concrete, supported realization — confirm this satisfies that intent. *(Recommendation: yes — Option A + C fallback.)*
- **OQ-2 (rotation ownership).** On re-provision, who guarantees "one live token per developer"? Does the Worker delete-then-recreate / rotate the secret on the existing `agorcha-agents-<slug>` token (cleanest, but PR #93 currently only creates), or does the client drive an explicit rotate/force path? This must be settled before the plan, because the Worker contract may need a small extension (the current route only creates).
- **OQ-3 (deprovision in v1?).** Does v1 ship a developer-facing deprovision/revoke (delete my edge token), or is revocation a maintainer-only action (delete the named token) for now? Local-clear (remove the two ids) is in scope regardless.
- **OQ-4 (platform store coverage).** On platforms where the credstore's OS-keychain step is unavailable, what is the supported persistence target for these ids — fail with guidance, or a documented alternate write path? Inherited from credstore; must be answered so the command's behavior is defined everywhere.
- **OQ-5 (verification-after-mint).** Should the command always do a post-mint authenticated `/internal/*` probe (catches policy mismatch immediately, costs one request and a moment), or only on an opt-in flag?

---

## 13. Relationship to other artifacts

- **Consumes:** the `/internal/provision` Worker route (PR #93) and its `{client_id, client_secret, name}` contract + status codes; the `internal/docsaccess` attach client (PR #83) and its credstore ids; the `credstore` loader/store (external-agent-sources).
- **Fulfills:** D8 of `docs-starlight-migration` ("auto-wired into developers' setup … without hand-managing tokens") and the second, deferred half of `dm6-agent-internal-access`.
- **Sibling pattern:** `da observability login` (obs proposal §6.3) — same browser→server-mint→credstore shape; a shared login/credential-write path may later be factored out, but is not required by this spec.
- **Next tier:** a plan at `workflow/plans/dm6-da-sso-autowire/` resolves OQ-1..OQ-5, fixes the command/flag names and file scopes, and breaks the work into tasks; its success criteria must trace back to §11.
