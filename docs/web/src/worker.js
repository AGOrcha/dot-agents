// Cloudflare Worker for the agorcha.dev docs site — a Cloudflare Access gate.
//
// The site is served from a SINGLE merged asset directory (./dist) via the one
// ASSETS binding declared in wrangler.jsonc. Cloudflare permits only one
// static-assets binding per Worker, so internal pages live UNDER the same dir at
// /internal/* (the deploy's merge step copies dist-internal/internal/ -> dist/
// internal/ while keeping dist's CLEAN public pagefind index). This handler
// path-routes:
//
//   1. /schemas/*  -> ASSETS, unauthenticated (public carve-out, checked FIRST)
//   2. /internal/* -> served ONLY after a valid Cloudflare Access JWT verifies;
//                     otherwise 401. NEVER served without a valid JWT.
//   3. everything else -> ASSETS, unauthenticated (public site)
//
// SECURITY NOTE: static assets bypass the Worker by default. wrangler.jsonc sets
// assets.run_worker_first for /internal/* so this fetch handler ALWAYS runs
// before an internal asset can be served — without that, /internal/* leaks.

// PUBLIC schema allowlist: ONLY these /schemas/<name> files are served
// unauthenticated. Every other schema (dashboard-*, workflow-*, verification-*)
// describes internal operational DTOs — auth/config keys, delegation-bundle and
// run/rubric shapes — and must NOT be exposed on the public tree. An explicit
// allowlist fails closed: a newly added schema is internal until listed here.
const PUBLIC_SCHEMAS = new Set([
  'agentsrc.schema.json', // the user-authored .agentsrc.json config contract
  'hook.schema.json', // documented publicly in HOOKS.md
  'plugin.schema.json', // documented publicly in PLUGIN_CONTRACT.md
]);

/**
 * Whether a normalized /schemas path is on the PUBLIC allowlist. The bare
 * "/schemas" or "/schemas/" listing is NOT public (it would enumerate internal
 * schema filenames); only an explicit allowlisted file is.
 *
 * @param {string} norm  normalized lower-case pathname (from normalizePathForGate)
 * @returns {boolean}
 */
export function isPublicSchemaPath(norm) {
  const prefix = '/schemas/';
  if (!norm.startsWith(prefix)) return false;
  const rest = norm.slice(prefix.length);
  // Exactly one segment (a file directly under /schemas/), on the allowlist.
  if (rest === '' || rest.includes('/')) return false;
  return PUBLIC_SCHEMAS.has(rest);
}

/**
 * Normalize a raw URL pathname for security classification, FAIL-CLOSED.
 *
 * Returns the canonical lower-segment path to classify, or `null` when the path
 * is non-canonical / hostile in a way we will not try to interpret (it must then
 * be treated as gated, never public). We refuse to serve, rather than guess, any
 * path that:
 *   - still contains a percent-encoding after URL's one decode pass (double
 *     encoding, e.g. %252f) — an attacker layering encodings to dodge a prefix;
 *   - contains a literal NUL;
 *   - contains a backslash (a separator on some stacks);
 *   - contains an empty segment ("//", embedded empty) other than a single
 *     trailing slash;
 *   - contains a "." or ".." dot-segment (traversal).
 * A decoded '/' or '\' that an attacker hid as %2f / %5c is, after URL's decode
 * pass, a literal separator here and is caught by the segment scan (it splits
 * "/internal%2fsecret" into ["", "internal", "secret"] -> gated).
 *
 * @param {string} rawPath  URL().pathname (already one-pass %-decoded by URL)
 * @returns {string | null}  canonical "/lower/case/path" or null if hostile
 */
export function normalizePathForGate(rawPath) {
  if (typeof rawPath !== 'string' || rawPath === '') return null;
  if (rawPath.includes('\0')) return null;
  if (rawPath.includes('\\')) return null;
  // Residual percent sign after URL's decode pass => double-encoded => refuse.
  if (rawPath.includes('%')) return null;

  const segments = rawPath.split('/');
  // rawPath begins with '/', so segments[0] === ''. Inspect the rest.
  for (let i = 1; i < segments.length; i++) {
    const seg = segments[i];
    if (seg === '') {
      // Permit only a single trailing empty segment (path ended in '/').
      if (i === segments.length - 1) continue;
      return null;
    }
    if (seg === '.' || seg === '..') return null;
  }
  return rawPath.toLowerCase();
}

/**
 * Classify a request path into a routing decision. Pure + synchronous so it can
 * be unit-tested without spinning a Worker. FAIL-CLOSED: a hostile/non-canonical
 * path (normalizePathForGate -> null) is classified 'internal' so it hits the
 * JWT gate (and is rejected) rather than being served by ASSETS as 'public'.
 *
 * @param {string} pathname  URL().pathname of the request
 * @returns {'schemas' | 'internal' | 'public'}
 */
export function classifyPath(pathname) {
  const norm = normalizePathForGate(pathname);
  if (norm === null) {
    return 'internal'; // could not canonicalize -> never public; gate it
  }
  // Internal check BEFORE the public branches (defense in depth).
  if (norm === '/internal' || norm.startsWith('/internal/')) {
    return 'internal';
  }
  // Schemas: only the explicit public allowlist is public; the bare listing and
  // all internal schemas are gated.
  if (norm === '/schemas' || norm.startsWith('/schemas/')) {
    return isPublicSchemaPath(norm) ? 'schemas' : 'internal';
  }
  return 'public';
}

/**
 * Decode a base64url string to bytes (Worker/WebCrypto-safe, no Node Buffer).
 *
 * @param {string} b64url
 * @returns {Uint8Array}
 */
function base64UrlToBytes(b64url) {
  const b64 = b64url.replace(/-/g, '+').replace(/_/g, '/');
  const pad = b64.length % 4 === 0 ? '' : '='.repeat(4 - (b64.length % 4));
  const bin = atob(b64 + pad);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

/**
 * Decode a base64url-encoded JSON segment (JWT header/payload).
 *
 * @param {string} b64url
 * @returns {any}
 */
function decodeJsonSegment(b64url) {
  const bytes = base64UrlToBytes(b64url);
  return JSON.parse(new TextDecoder().decode(bytes));
}

// Module-scope JWKS cache: keyed by team-domain certs URL, holds the parsed
// JWKS plus an expiry. Survives across requests within a Worker isolate, so we
// fetch Cloudflare Access certs at most once per TTL rather than per request.
const JWKS_TTL_MS = 60 * 60 * 1000; // 1h
/** @type {Map<string, { keys: any[], expiresAt: number }>} */
const jwksCache = new Map();

/**
 * Fetch (and cache) the Cloudflare Access JWKS for a team domain.
 *
 * @param {string} teamDomain  e.g. "myteam.cloudflareaccess.com"
 * @returns {Promise<any[]>}   array of JWKs
 */
async function getJwks(teamDomain) {
  const certsUrl = `https://${teamDomain}/cdn-cgi/access/certs`;
  const cached = jwksCache.get(certsUrl);
  if (cached && cached.expiresAt > Date.now()) {
    return cached.keys;
  }
  const resp = await fetch(certsUrl);
  if (!resp.ok) {
    throw new Error(`JWKS fetch failed: ${resp.status}`);
  }
  const data = await resp.json();
  const keys = Array.isArray(data.keys) ? data.keys : [];
  jwksCache.set(certsUrl, { keys, expiresAt: Date.now() + JWKS_TTL_MS });
  return keys;
}

/**
 * Read the Cloudflare Access JWT from a request: the Cf-Access-Jwt-Assertion
 * header (preferred), falling back to the CF_Authorization cookie.
 *
 * @param {Request} request
 * @returns {string | null}
 */
function extractAccessToken(request) {
  const header = request.headers.get('Cf-Access-Jwt-Assertion');
  if (header) return header;
  const cookie = request.headers.get('Cookie') || '';
  const match = cookie.match(/(?:^|;\s*)CF_Authorization=([^;]+)/);
  return match ? match[1] : null;
}

/**
 * Verify a Cloudflare Access JWT: RS256 signature against the team JWKS, plus
 * aud / iss / exp claim checks. Uses WebCrypto only (no npm dependency).
 *
 * @param {Request} request
 * @param {{ CF_ACCESS_AUD: string, CF_ACCESS_TEAM_DOMAIN: string }} env
 * @returns {Promise<{ ok: boolean, reason?: string }>}
 */
export async function verifyAccessJwt(request, env) {
  const token = extractAccessToken(request);
  if (!token) return { ok: false, reason: 'no-token' };

  const parts = token.split('.');
  if (parts.length !== 3) return { ok: false, reason: 'malformed-token' };
  const [headerB64, payloadB64, signatureB64] = parts;

  let header;
  let payload;
  try {
    header = decodeJsonSegment(headerB64);
    payload = decodeJsonSegment(payloadB64);
  } catch {
    return { ok: false, reason: 'undecodable-token' };
  }

  // Pin the algorithm to RS256 — reject "none", HS* (HMAC key-confusion where an
  // attacker signs with the public key as an HMAC secret), and any other alg.
  if (header.alg !== 'RS256') return { ok: false, reason: 'unexpected-alg' };
  // kid must be a present, non-empty string so the JWK lookup is an explicit
  // match — never let an absent kid match an absent-kid JWK (undefined===undefined).
  if (typeof header.kid !== 'string' || header.kid === '') {
    return { ok: false, reason: 'missing-kid' };
  }

  // Config must be set. If the maintainer placeholder is still in place (or the
  // vars are empty), fail closed rather than build a bogus issuer/JWKS URL.
  if (
    !env.CF_ACCESS_AUD ||
    !env.CF_ACCESS_TEAM_DOMAIN ||
    env.CF_ACCESS_TEAM_DOMAIN.startsWith('REPLACE-ME')
  ) {
    return { ok: false, reason: 'gate-unconfigured' };
  }

  // Claim checks (cheap; do before the crypto where possible, but signature is
  // still required below — a forged unsigned token must never pass).
  const now = Math.floor(Date.now() / 1000);
  if (typeof payload.exp !== 'number' || payload.exp <= now) {
    return { ok: false, reason: 'expired' };
  }
  if (typeof payload.nbf === 'number' && payload.nbf > now + 60) {
    return { ok: false, reason: 'not-yet-valid' };
  }
  const expectedIss = `https://${env.CF_ACCESS_TEAM_DOMAIN}`;
  if (payload.iss !== expectedIss) {
    return { ok: false, reason: 'bad-issuer' };
  }
  // aud may be a string or an array; require it to include the configured audTag.
  const aud = payload.aud;
  const audList = Array.isArray(aud) ? aud : aud == null ? [] : [aud];
  if (!audList.includes(env.CF_ACCESS_AUD)) {
    return { ok: false, reason: 'bad-audience' };
  }

  // Signature verification (RS256) against the matching JWK by kid.
  let keys;
  try {
    keys = await getJwks(env.CF_ACCESS_TEAM_DOMAIN);
  } catch {
    return { ok: false, reason: 'jwks-unavailable' };
  }
  const jwk = keys.find((k) => typeof k.kid === 'string' && k.kid === header.kid);
  if (!jwk) return { ok: false, reason: 'unknown-kid' };
  // The matched key must be an RSA key; if it advertises an alg, it must be
  // RS256. Defends against a JWKS entry of the wrong type/alg being coerced.
  if (jwk.kty !== 'RSA') return { ok: false, reason: 'bad-jwk-kty' };
  if (jwk.alg !== undefined && jwk.alg !== 'RS256') {
    return { ok: false, reason: 'bad-jwk-alg' };
  }

  let cryptoKey;
  try {
    cryptoKey = await crypto.subtle.importKey(
      'jwk',
      jwk,
      { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-256' },
      false,
      ['verify'],
    );
  } catch {
    return { ok: false, reason: 'bad-jwk' };
  }

  const signature = base64UrlToBytes(signatureB64);
  const signed = new TextEncoder().encode(`${headerB64}.${payloadB64}`);
  let valid;
  try {
    valid = await crypto.subtle.verify(
      'RSASSA-PKCS1-v1_5',
      cryptoKey,
      signature,
      signed,
    );
  } catch {
    return { ok: false, reason: 'verify-error' };
  }
  if (!valid) return { ok: false, reason: 'bad-signature' };

  return { ok: true };
}

/**
 * Build the 401 response for a blocked /internal/* request.
 *
 * @returns {Response}
 */
function unauthorized() {
  return new Response('Unauthorized', {
    status: 401,
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
}

export default {
  /**
   * @param {Request} request
   * @param {{
   *   ASSETS: { fetch: (req: Request) => Promise<Response> },
   *   CF_ACCESS_AUD: string,
   *   CF_ACCESS_TEAM_DOMAIN: string,
   * }} env
   */
  async fetch(request, env) {
    const { pathname } = new URL(request.url);
    const route = classifyPath(pathname);

    // 1. Schemas: always public (carve-out checked first).
    if (route === 'schemas') {
      return env.ASSETS.fetch(request);
    }

    // 2. Internal: require a valid Cloudflare Access JWT before serving.
    if (route === 'internal') {
      const result = await verifyAccessJwt(request, env);
      if (!result.ok) {
        return unauthorized();
      }
      return env.ASSETS.fetch(request);
    }

    // 3. Public: everything else.
    return env.ASSETS.fetch(request);
  },
};
