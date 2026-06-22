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

/**
 * Classify a request path into a routing decision. Pure + synchronous so it can
 * be unit-tested without spinning a Worker.
 *
 * @param {string} pathname  URL().pathname of the request
 * @returns {'schemas' | 'internal' | 'public'}
 */
export function classifyPath(pathname) {
  // Schemas carve-out FIRST: always public, even though it is a sub-path.
  if (pathname === '/schemas' || pathname.startsWith('/schemas/')) {
    return 'schemas';
  }
  // Gate ONLY '/internal' exactly or '/internal/...' — '/internalfoo' is public.
  if (pathname === '/internal' || pathname.startsWith('/internal/')) {
    return 'internal';
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

  if (header.alg !== 'RS256') return { ok: false, reason: 'unexpected-alg' };

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
  const jwk = keys.find((k) => k.kid === header.kid);
  if (!jwk) return { ok: false, reason: 'unknown-kid' };

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
