# Origin-validate any endpoint before attaching a credential when its URL comes from untrusted input

**Captured:** 2026-07-16
**Triggered by:** the package-artifact-install OCI auth/publish arc (t7, t8). The SAME
credential-leak class was found by cross-harness (Codex) review **four times** across t7 and t8 —
each a real leak the same-family tests passed straight over:

1. **t7 token realm** — `exchangeBearerToken` sent Basic/Bearer creds to the `realm` from the
   registry's `WWW-Authenticate` header with no scheme check (cleartext-downgrade + arbitrary host).
2. **t7 token redirect** — the token client followed redirects; Go preserves `Authorization` on a
   same-host different-port / subdomain redirect, so a realm could bounce the credential onward.
3. **t8 blob-upload `Location`** — `ociPushBlob` PUT the blob to the registry-controlled `Location`
   header verbatim with the push credential attached (arbitrary attacker origin).
4. **t8 push-client redirect** — the shared registry client auto-followed a registry-controlled 3xx
   on the credentialed POST/PUT push legs (same-host cleartext downgrade / subdomain hop).

## The mistake

Attaching a credential (`Authorization` header, Basic auth, a token) to an HTTP request whose
**destination URL is derived from untrusted input** — a response header (`WWW-Authenticate` realm,
`Location`), a redirect target, a manifest field — **without validating that destination's origin
first.** The endpoint is attacker-influenceable; the credential walks to wherever the attacker points.

Two non-obvious traps make this easy to get wrong even when you "added a check":

- **Go's redirect auth-stripping is host-only, not origin.** `net/http` strips `Authorization` on a
  *cross-domain* redirect but **preserves** it on a same-host redirect to a **different scheme or
  port** (an `https→http` cleartext downgrade) and to a **subdomain** (`isDomainOrSubdomain`,
  `url.Hostname()` drops the port). So "Go handles redirect safety" is false for the downgrade/
  subdomain cases, and a scheme-only redirect check misses the port/subdomain.
- **Scheme-relative / network-path URLs.** `//attacker.evil/steal` parses with an empty scheme
  (`IsAbs()==false`), so a naive "only guard absolute URLs" check skips it — but resolved against the
  base it swaps the **host** while keeping the base scheme. Guard the **resolved** URL, not the raw one.

## The rule

Whenever a credential is attached to a request whose URL came from untrusted input:

1. **Pin the destination origin.** Require the resolved URL to share **scheme + host + effective
   port** (apply the scheme's default port) with the configured/trusted endpoint before attaching the
   credential; reject cross-origin, an `https→http` downgrade, and embedded userinfo. Validate the
   **resolved** URL (covers scheme-relative `//host` network-path references). See `sameOCIOrigin` /
   `ociHostPort` / `resolveOCILocation` in `internal/config/artifact_bundle.go`.
2. **Refuse redirects on the credentialed request** (do not rely on Go's stripping). A request that
   carries a credential and has no legitimate redirect (auth token exchange, a push POST/PUT) gets a
   `CheckRedirect` that refuses **all** redirects — `rejectTokenEndpointRedirect` (`oci_auth.go`) and
   the push legs of `guardOCIRegistryRedirect` (`artifact_bundle.go`). Where a redirect IS legitimate
   (a pull blob→CDN GET), still refuse a **non-https** target (no cleartext credential); a cross-origin
   **https** hop is acceptable only because Go strips auth there and the bytes are digest-verified.
   Distinguish push from pull by `via[0].Method` (the ORIGINAL method — Go never mutates `via`
   entries, so a 303 POST→GET rewrite can't smuggle a push past a GET-only allow).
3. **Never echo the credential-bearing URL.** Strip query (may carry a session `_state` token) and
   userinfo from any URL that lands in an error string; `redactSecrets` only scrubs *registered*
   secrets. See `urlWithoutQuery` / `ociOriginString`.

## How to apply

- Grep for where a credential/`Authorization` is attached (`SetBasicAuth`, `Authorization` header,
  an auth arg threaded into an HTTP call); for each, trace the URL back to its source. If any hop of
  it comes from a response header / redirect / manifest, it needs the origin pin + a redirect policy.
- Every `http.Client` that ever carries a credential needs an explicit `CheckRedirect` — the zero
  value follows redirects. A missing `CheckRedirect` on a credentialed client is a finding.
- This class is nearly invisible to same-model self-review and to green tests (the happy path uses a
  benign registry). It was caught only by the cross-harness gate — see
  [[cross-harness-read-raw-not-reconciler]].
