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

**Two access paths (important — they use different credentials):**

- **Human, in a browser** → Cloudflare Access SSO (GitHub). The developer browses `agorcha.dev/internal/*` directly; the Access JWT cookie satisfies the gate. **No service token involved.** This already works once the gate is live (dm5-prod) and needs nothing from this spec.
- **Agent / `da`, programmatically** → the per-user CF Access *service token* (the `CF-Access-Client-Id/Secret` headers). This is the path this spec enables. It only works when **`da` itself is the HTTP client** — a raw agent `WebFetch` will not carry those headers and will 401. So agent/programmatic internal-docs access must flow **through `da`**.

**The gap (this spec).** Two gaps, both on the agent path:

1. **No provisioner.** Nothing gets a developer from "I have a GitHub login" to "the service token is in my store". Today they'd hand-obtain a JWT, hand-POST `/internal/provision` with curl, copy the secret, and hand-write the encrypted credstore under the two magic ids. No `da` command writes the credstore at all yet.
2. **No consumer.** The attach client (PR #83) currently has **no caller** — nothing in `da` actually fetches `/internal/*`, so a provisioned token does nothing on its own. And dm4's `.md`/`llms.txt` endpoints are public-only, so there is no agent-readable *internal* surface to fetch yet.

This spec designs both halves of the agent path: (1) an interactive command that authenticates via CF Access, calls the minting route, and stores the returned pair under the ids `docsaccess` consumes (auto-wiring every subsequent `da` run); and (2) a consumer command + gated agent-readable surface so the stored token is actually *used* to read internal docs.

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

### Option B — `da` runs the loopback browser flow itself

`da` opens the system browser to the Access login URL with a `redirect` back to a transient `http://127.0.0.1:<port>/` callback it spins up, and captures the issued token on that callback (the same handshake `cloudflared access login` performs internally). A device-code poll is the headless-shell variant.

- **This is the idiomatic CLI/IDE auth pattern.** `gh auth login`, the VS Code account flows, and Claude Code's own sign-in all use a browser→loopback-callback handshake. It is the pattern developers already expect from a CLI, and it removes the extra-binary prerequisite.
- **Pros:** No `cloudflared` dependency. Full control over UX, the callback, and where the token lives. **Best fit for the eventual daemon + auth-proxy (§10):** a long-lived `da` daemon can own the loopback/session and proxy auth centrally, rather than shelling to `cloudflared` per call — so building B is the natural precursor to that layer, not throwaway work.
- **Cons:** More implementation + security surface than A (a local callback server, state/PKCE, token handling we own). The exact CF Access loopback/redirect contract for an app-token must be pinned (it's what `cloudflared` drives, so it exists, but it's less publicly documented than a standard OAuth provider) — the main spike risk. A device-code fallback is still needed for no-browser shells.

### Option C — Ask the user to paste a token

Prompt the developer to run `cloudflared access token` (or grab the JWT from the browser) and paste it into `da`.

- **Pros:** Zero browser/process orchestration in `da`. Works anywhere a JWT can be produced.
- **Cons:** Poor UX (manual copy of a long-lived-ish bearer secret through the terminal/clipboard — easy to leak into shell history/scrollback). Still requires `cloudflared` or equivalent to *produce* the token, so it inherits Option A's dependency without Option A's automation. Error-prone (truncated/expired pastes).

### Recommendation

Two-horizon decision (revised 2026-06-23):

- **Near-term (this command, ship fast): Option A** + Option C fallback. Lowest-code, lowest-risk way to deliver the interactive developer flow now; reuses Cloudflare's own SSO client; `da` owns only the `/internal/provision` POST + credstore write. Degrades to a paste prompt / printed `cloudflared` commands when `cloudflared` is absent.
- **Strategic target: Option B (loopback).** This is the standard CLI/IDE auth pattern (gh, VS Code, Claude Code) and the right fit once the **daemon + auth-proxy** (§10) exists — the daemon owns the loopback/session and proxies auth, removing the `cloudflared` dependency entirely. Option B is **not rejected** (the earlier draft was wrong to call it speculative): it is deferred behind a spike to pin the CF Access loopback/redirect contract, and is the intended end state. Build A first so the feature lands; move to B with the daemon.

Design so the SSO leg is a swappable seam (the JWT acquirer is an interface — A today, B later) so the consumer/command code does not change when the mechanism does. This is OQ-1.

---

## 3. Command surface (decision: name, shape, UX)

**Decision: a new `da docs` command group** with two verbs — `da docs login` (provision, this section) and `da docs get`/`sync` (consume, §3a). The docs-access counterpart to the obs proposal's `da observability login`. Rationale: discoverable, scoped to the docs-access concern, verb-consistent with the established `<noun> login` pattern, and does not overload `da init`/`da refresh` with an interactive browser flow (those must stay non-interactive — see §5). A `--flag on da init` was considered and rejected: init runs in scaffolding/CI contexts where an unexpected browser pop is hostile; provisioning is a deliberate, occasional act, not part of every init.

Behavioral requirements for the command (not its flags' exact spelling — that's plan tier):

- **Default (interactive):** run the CF Access SSO leg (Option A), POST `/internal/provision`, store the returned pair, confirm success. Idempotent and safe to re-run (§7).
- **A way to opt out of the auto-SSO and paste a JWT** (Option C fallback) for environments without `cloudflared`.
- **A status/check mode** that reports whether the credstore currently holds usable docsaccess credentials and whether they authenticate, WITHOUT minting — so a developer can diagnose before re-provisioning.
- **Honor the existing global flags** the command tree already threads (dry-run, yes/non-interactive, verbose, force) consistently with sibling commands. Dry-run must describe what would happen and mint nothing.
- **Host override:** respect the same docs-host override `docsaccess` honors (`DA_DOCS_HOST`) so the command works against staging/preview, keeping the SSO target and the provision target in lockstep.

**Output (UX requirements):**

- Print the minted token's *name* and the credential ids it was stored under. NEVER print `client_secret` (or `client_id` beyond what's needed to confirm). Consistent with the client half's "secret value is never logged" posture.
- On success, tell the developer they are done — no manual edit, no env var to set — and that `da docs get`/`sync` (and any da-mediated fetch) will now reach `/internal/*`.
- On each failure mode from the Worker, map the status to a specific, actionable message (see §9). Never dump a raw secret or a raw JWT on error.

## 3a. Consumer surface (decision: how the token actually gets used)

Provisioning is inert without a consumer — the attach client (PR #83) has no caller today. This spec therefore also commits the agent-read path:

- **Decision: a `da docs get <path>` / `da docs sync` consumer** that fetches the gated internal surface *through* `da`, so the attach client adds the service-token headers. `get` prints/saves one page; `sync` pulls the internal set into a local cache an agent can read. This is the only authenticated programmatic reader — agents must call `da`, not fetch the URL directly (a raw fetch is unauthenticated → 401).
- **Decision: the agent-readable internal surface is gated `.md` endpoints.** Extend dm4's `.md`/`llms.txt` emission to also produce the INTERNAL pages' raw markdown under `/internal/*` (behind the same gate dm5 enforces) — markdown is the right shape for agents and avoids scraping rendered HTML. The public `.md` surface (dm4) is unchanged; this adds the gated internal counterpart that `da docs get`/`sync` consumes.
- **Auth used by the consumer:** the stored service token via the attach client (NOT the SSO JWT — that's the human/browser path). HTTPS-only, same `DA_DOCS_HOST` override, same fail-loud-on-missing-credential behavior the attach client already enforces. On a 401 from stale/expired creds (§6), the consumer points the developer at `da docs login`.

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

## 6. Multi-device, idempotency, TTL, rotation, revocation

This is the subtle part: the CF API returns `client_secret` ONLY at creation time, so a secret can never be re-read or copied to another machine. That single fact drives the model below.

**Decision — per-DEVICE tokens, not one-per-developer.** Tokens are named `agorcha-agents-<user-slug>-<device>`, where `<user-slug>` is derived server-side from the verified JWT identity (unchanged from #93 — prevents minting under another user's name) and `<device>` is a sanitized suffix the client supplies (hostname or a stable locally-stored device id). Each device self-provisions its OWN token; **no secret ever moves between machines.** This is the SSH-key / GitHub-PAT model. The Access policy is `any_valid_service_token`, so every one of a developer's device tokens authenticates — no policy change. This dissolves the multi-device storage problem: forcing "one token per developer" would require exporting/syncing the create-only secret across devices, which is exactly the burden we avoid.

**Decision — server-enforced TTL (~30 days / one month).** The Worker mints with a bounded `duration` so secrets are short-lived: stale or leaked tokens self-expire and developers re-provision on a regular cadence (freshness + blast-radius reduction). The TTL is enforced **server-side** (a Worker constant/var on the mint call) — the client cannot request a longer-lived token (privilege boundary). Exact CF API `duration` format and any min/max are to be confirmed at implementation (t6) against the live API; the intent is ~720h. Expired tokens fail at the edge (401); the consumer (§3a) and `da docs login --status` surface "expired — re-run `da docs login`". The client SHOULD persist the (non-secret) expiry so `--status` can warn *before* expiry, not only after.

**Other requirements:**

- **Re-provision = rotation, per device.** Re-running on the same device rotates THAT device's named token (delete-then-create), so reinstalls/rotations do not accumulate orphans on a single machine. Across devices, tokens accumulate by design (one per device).
- **Lost-secret recovery** is just re-provision on that device (the secret is create-only); it rotates the device token and re-stores it.
- **Revocation:** lost laptop → delete that one device token; offboard a developer → delete all `agorcha-agents-<user-slug>-*` by prefix. The TTL also bounds a forgotten device: its token dies within ~30 days regardless. Whether `da` ships a developer-facing deprovision in v1 or revocation stays a maintainer action is OQ-3; the lever is deletion of the named token(s).
- **Local clear:** removing the two ids from the local store disables that device's `da` access without revoking the edge token. `da docs login` (or a sibling verb) supports clearing local creds.

This resolves OQ-2: the constraint is **one live token per device** (rotate-on-reprovision per device + server TTL), NOT one per developer. The Worker route gains a device suffix + delete-then-create + duration (a small extension — see §12/OQ-2 and the plan's t6).

---

## 7. Security considerations

- **Never log secrets.** Neither `client_secret`, `client_id` (beyond confirmation), nor the CF Access JWT may appear in logs, dry-run output, error messages, or verbose mode. This extends the client half's stated discipline ("the secret value is never logged; only the credential id and the gating decision are surfaced").
- **HTTPS-only.** The `/internal/provision` POST and any verification request must be HTTPS — consistent with the client half's `ErrInsecureScheme` posture (CF-Access headers and the JWT are bearer-equivalent secrets; refuse plaintext). A non-HTTPS docs host must fail loudly before any secret/JWT is transmitted.
- **JWT handling.** The JWT is a short-lived bearer credential. `da` should hold it only for the duration of the provision call and not persist it itself (the cloudflared cache is cloudflared's; `da` does not copy it elsewhere). Paste-fallback input must be read without echoing to scrollback where feasible.
- **At-rest.** The minted pair lands in the existing encrypted, keychain-wrapped store (0600 discipline) — no plaintext drop.
- **No mint-anything secret in the binary.** Already enforced server-side (the scoped CF API token is a Worker secret, per dm6 notes / PR #93). `da` never holds a mint-capable credential — it only holds its own per-device service token. This spec must not introduce any client-side privileged key.
- **Short-lived secrets (server-enforced TTL ~30 days).** Minted tokens expire so a leaked or forgotten secret has a bounded lifetime and developers rotate on a regular cadence (§6). The TTL is set on the mint call by the Worker; the client cannot extend it. This trades a periodic `da docs login` for a much smaller leak blast-radius.
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
- **The `da service` daemon + auth-proxy injection layer** (proposal §5.5). Direct-attach via the credstore (PR #83) stands; the proxy is a later refactor and does not change this spec's storage contract. This layer is the home for **Option B (loopback) auth** (§2): the long-lived daemon owns the loopback/session and proxies auth, retiring the cloudflared dependency. The near-term command's JWT-acquirer seam is built so this swap is mechanism-only.
- **Auto-renewal of the service token.** Tokens carry a server-enforced ~30-day TTL (§6); renewal is a manual re-provision (`da docs login`), prompted by `--status` / a consumer 401. A background auto-renewer is out of scope (it would need a non-interactive auth path, which is the deferred headless work).
- **Generalizing to non-docs credentials** (obs, sources). The obs proposal's `da observability login` is a sibling, not this work; if a shared credential-write/login path emerges, both benefit — but this spec only commits the docs-access flow.

---

## 11. Done criteria (verifiable)

1. A developer with a valid GitHub identity, starting from a machine with no docsaccess credentials, can run a single `da` command and end with a working `agorcha-agents-<user-slug>-<device>` token whose `client_id`/`client_secret` are stored under `cf-access-client-id` / `cf-access-client-secret` in the encrypted store.
2. Immediately afterward, `da docs get`/`sync` (and any da-mediated `/internal/*` fetch through the attach client) succeeds (not 401 / not a login redirect) with NO further developer action — demonstrating auto-wire AND that a consumer actually exists.
3. `da init` and `da refresh` run non-interactively (no browser) and never mint; they consume the stored creds via the existing attach client and, when creds are absent, fail loud or hint (existing behavior) rather than hang on a prompt.
4. **Multi-device:** a second device run by the same developer can independently provision its own token; both devices authenticate concurrently and neither invalidates the other (no secret syncing).
5. **Rotation:** re-running on the SAME device rotates that device's token (delete-then-create) and leaves no orphaned/duplicate token for that device.
6. **TTL:** minted tokens carry the server-enforced ~30-day expiry; an expired token is rejected at the edge, and `da docs login --status` reports it as expired and prompts re-provision (ideally warning before expiry).
7. No secret (`client_secret`, `client_id` beyond confirmation) and no JWT appears in any output, log, dry-run, or error path; provisioning/consuming over a non-HTTPS host is refused before any secret/JWT leaves the process.
8. When `cloudflared` is absent, the command degrades to a documented manual path instead of crashing.
9. Each Worker failure status (401/422/503/502) produces a distinct, actionable message.
10. Dry-run mints nothing and writes nothing, but describes the flow.

---

## 12. Open questions

- **OQ-1 (auth mechanism) — two-horizon (see §2 revised).** Near-term Option A (cloudflared) + paste fallback to ship now; strategic target Option B (loopback browser→localhost callback — the gh/VS Code/Claude pattern), moved onto the daemon + auth-proxy, which removes the cloudflared dependency. Build behind a swappable JWT-acquirer seam so the command/consumer code is unchanged when A→B. Remaining: confirm A is acceptable as the interim dependency, and schedule the B spike (pin the CF Access loopback/redirect contract) with the daemon work.
- **OQ-2 (rotation ownership) — RESOLVED (2026-06-23).** Model is **per-device**, not per-developer (§6): name `agorcha-agents-<user-slug>-<device>`, re-provision rotates that device's token via Worker-side delete-then-create, tokens accumulate across devices, revoke by name/prefix. The Worker (#93, merged) gains: (a) an optional client-supplied device suffix appended to the JWT-derived user slug, (b) delete-then-create for that exact name, and (c) a server-enforced `duration` (~30-day TTL). This is the t6 scope. Remaining sub-question: exact CF API `duration` format + any min/max — confirm at implementation, not a blocker for the plan.
- **OQ-3 (deprovision in v1?).** Does v1 ship a developer-facing deprovision/revoke (delete my edge token), or is revocation a maintainer-only action (delete the named token) for now? Local-clear (remove the two ids) is in scope regardless.
- **OQ-4 (platform store coverage).** On platforms where the credstore's OS-keychain step is unavailable, what is the supported persistence target for these ids — fail with guidance, or a documented alternate write path? Inherited from credstore; must be answered so the command's behavior is defined everywhere.
- **OQ-5 (verification-after-mint).** Should the command always do a post-mint authenticated `/internal/*` probe (catches policy mismatch immediately, costs one request and a moment), or only on an opt-in flag?

---

## 13. Relationship to other artifacts

- **Consumes:** the `/internal/provision` Worker route (PR #93) and its `{client_id, client_secret, name}` contract + status codes; the `internal/docsaccess` attach client (PR #83) and its credstore ids; the `credstore` loader/store (external-agent-sources).
- **Fulfills:** D8 of `docs-starlight-migration` ("auto-wired into developers' setup … without hand-managing tokens") and the second, deferred half of `dm6-agent-internal-access`.
- **Sibling pattern:** `da observability login` (obs proposal §6.3) — same browser→server-mint→credstore shape; a shared login/credential-write path may later be factored out, but is not required by this spec.
- **Next tier:** a plan at `workflow/plans/dm6-da-sso-autowire/` resolves OQ-1..OQ-5, fixes the command/flag names and file scopes, and breaks the work into tasks; its success criteria must trace back to §11.
