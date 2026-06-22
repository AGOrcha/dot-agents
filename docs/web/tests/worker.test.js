// Cloudflare Access gate — Option A leak guard (MANDATORY regression test).
//
// Proves that the Worker's path-routing never serves /internal/* without a
// valid Cloudflare Access JWT, while public pages and /schemas/* serve
// unauthenticated. Runs hermetically via the Node built-in test runner
// (`node --test`) against the real handler exported from src/worker.js — no
// miniflare/wrangler dev, no new npm dependency.
//
// The core invariant: UNAUTHENTICATED GET /internal/<page> MUST be blocked
// (401), NOT 200 with internal content.

import { test } from 'node:test';
import assert from 'node:assert/strict';

import worker, { classifyPath, verifyAccessJwt } from '../src/worker.js';

// Stub ASSETS binding: returns 200 with a marker so a leak (asset served when
// it should have been gated) is unambiguous in assertions.
const ASSETS = {
  async fetch(request) {
    const { pathname } = new URL(request.url);
    return new Response(`ASSET:${pathname}`, { status: 200 });
  },
};

// Non-secret env identifiers. The team domain is a placeholder, so any real JWT
// verification (JWKS fetch / issuer match) cannot succeed — exactly the
// production posture until the maintainer sets the real team domain. The gate
// must still BLOCK /internal/* in that state (fail closed), never serve it.
const env = {
  ASSETS,
  CF_ACCESS_AUD:
    '50476bf0ad28d003fbf4cfb1d36ed2554907f7f2e0e6c699f5fb4bc4b8d1a6ff',
  CF_ACCESS_TEAM_DOMAIN: 'REPLACE-ME.cloudflareaccess.com',
};

function req(path, headers = {}) {
  return new Request(`https://agorcha.dev${path}`, { headers });
}

// --- classifyPath unit coverage (routing precedence + boundary safety) -------

test('classifyPath: an allowlisted /schemas file is the public carve-out', () => {
  assert.equal(classifyPath('/schemas/agentsrc.schema.json'), 'schemas');
  // The bare /schemas listing is NOT public — it would enumerate internal schema
  // filenames; only explicitly allowlisted files are served (see allowlist test).
  assert.equal(classifyPath('/schemas'), 'internal');
});

test('classifyPath: /internal and /internal/* are gated', () => {
  assert.equal(classifyPath('/internal'), 'internal');
  assert.equal(classifyPath('/internal/lessons/'), 'internal');
  assert.equal(
    classifyPath('/internal/lessons/use-existing-subdir-helpers/'),
    'internal',
  );
});

test('classifyPath: /internalfoo does NOT match the gate (boundary)', () => {
  assert.equal(classifyPath('/internalfoo'), 'public');
  assert.equal(classifyPath('/internal-docs'), 'public');
});

test('classifyPath: root and public pages are public', () => {
  assert.equal(classifyPath('/'), 'public');
  assert.equal(classifyPath('/guides/demo-overview/'), 'public');
});

// --- verifyAccessJwt: rejects absent/forged tokens (fail closed) -------------

test('verifyAccessJwt: no token -> not ok', async () => {
  const result = await verifyAccessJwt(req('/internal/lessons/'), env);
  assert.equal(result.ok, false);
  assert.equal(result.reason, 'no-token');
});

test('verifyAccessJwt: forged/garbage token -> not ok', async () => {
  const result = await verifyAccessJwt(
    req('/internal/lessons/', {
      'Cf-Access-Jwt-Assertion': 'not.a.jwt',
    }),
    env,
  );
  assert.equal(result.ok, false);
  assert.notEqual(result.reason, undefined);
});

test('verifyAccessJwt: well-formed but unsigned/expired token -> not ok', async () => {
  // Hand-roll a structurally valid JWT with an expired exp claim. Even if claim
  // order let it slip, the missing signature would fail — but exp catches it
  // first, proving we never trust an unverified payload.
  const b64u = (obj) =>
    Buffer.from(JSON.stringify(obj))
      .toString('base64')
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/, '');
  const header = b64u({ alg: 'RS256', kid: 'fake' });
  const payload = b64u({
    aud: [env.CF_ACCESS_AUD],
    iss: `https://${env.CF_ACCESS_TEAM_DOMAIN}`,
    exp: Math.floor(Date.now() / 1000) - 3600,
  });
  const token = `${header}.${payload}.AAAA`;
  const result = await verifyAccessJwt(
    req('/internal/lessons/', { 'Cf-Access-Jwt-Assertion': token }),
    env,
  );
  assert.equal(result.ok, false);
});

// --- fetch handler: the end-to-end leak guard --------------------------------

test('LEAK GUARD: unauthenticated GET /internal/<page> is BLOCKED (401, not 200)', async () => {
  const resp = await worker.fetch(
    req('/internal/lessons/use-existing-subdir-helpers/'),
    env,
  );
  assert.equal(
    resp.status,
    401,
    'unauthenticated /internal/* must be 401, never 200',
  );
  const body = await resp.text();
  assert.ok(
    !body.startsWith('ASSET:'),
    'internal asset must NOT have been served',
  );
});

test('LEAK GUARD: forged JWT on /internal/<page> is BLOCKED (401)', async () => {
  const resp = await worker.fetch(
    req('/internal/specs/', { 'Cf-Access-Jwt-Assertion': 'forged.jwt.value' }),
    env,
  );
  assert.equal(resp.status, 401);
});

test('GET /schemas/<x>.json serves unauthenticated (200)', async () => {
  const resp = await worker.fetch(req('/schemas/agentsrc.schema.json'), env);
  assert.equal(resp.status, 200);
  assert.equal(
    await resp.text(),
    'ASSET:/schemas/agentsrc.schema.json',
    'schemas must be served straight from ASSETS',
  );
});

test('GET / serves unauthenticated (200)', async () => {
  const resp = await worker.fetch(req('/'), env);
  assert.equal(resp.status, 200);
  assert.equal(await resp.text(), 'ASSET:/');
});

test('GET a public page serves unauthenticated (200)', async () => {
  const resp = await worker.fetch(req('/guides/demo-overview/'), env);
  assert.equal(resp.status, 200);
  assert.equal(await resp.text(), 'ASSET:/guides/demo-overview/');
});

test('boundary: /internalfoo is treated as public (served 200)', async () => {
  const resp = await worker.fetch(req('/internalfoo'), env);
  assert.equal(resp.status, 200);
  assert.equal(await resp.text(), 'ASSET:/internalfoo');
});

// --- MEDIUM: encoded / non-canonical path bypass (fail closed) ---------------
// Each of these normalizes onto (or could be coerced onto) the /internal prefix
// or is non-canonical. classifyPath MUST NOT return 'public' for any of them —
// they are gated (then the JWT gate rejects), never ASSETS-served.

test('classifyPath: encoded-slash variant of /internal is gated, not public', () => {
  // URL() decodes %2f to a literal '/', so this splits to ["", "internal", "secret"].
  assert.equal(classifyPath('/internal%2fsecret'), 'internal');
  assert.equal(classifyPath('/internal/secret'), 'internal');
});

test('classifyPath: double-encoded path is refused (gated), not public', () => {
  // %252f survives URL's one decode pass as the literal "%2f" — residual '%' => refuse.
  assert.equal(classifyPath('/internal%252fsecret'), 'internal');
  assert.equal(classifyPath('/%2finternal/x'), 'internal');
});

test('classifyPath: case variants of /internal are gated', () => {
  assert.equal(classifyPath('/INTERNAL/secret'), 'internal');
  assert.equal(classifyPath('/Internal'), 'internal');
});

test('classifyPath: leading double-slash, NUL, embedded space are refused (gated)', () => {
  assert.equal(classifyPath('//internal/'), 'internal');
  assert.equal(classifyPath('/internal' + '\x00'), 'internal');
  assert.equal(classifyPath('/foo//bar'), 'internal');
});

test('classifyPath: dot-segments and backslash are refused (gated)', () => {
  assert.equal(classifyPath('/public/../internal/x'), 'internal');
  assert.equal(classifyPath('/internal/../guides'), 'internal');
  assert.equal(classifyPath('/internal\\x'), 'internal');
});

test('classifyPath: empty/garbage paths are gated; clean public stays public', () => {
  assert.equal(classifyPath(''), 'internal');
  assert.equal(classifyPath('/guides/x/'), 'public');
});

test('fetch: encoded-slash /internal bypass is BLOCKED (401, not ASSET)', async () => {
  const resp = await worker.fetch(req('/internal%2fsecret'), env);
  assert.equal(resp.status, 401, 'encoded-slash internal must gate, never 200');
  const body = await resp.text();
  assert.ok(!body.startsWith('ASSET:'), 'must not be served from ASSETS');
});

test('fetch: case-variant /INTERNAL bypass is BLOCKED (401)', async () => {
  const resp = await worker.fetch(req('/INTERNAL/lessons/'), env);
  assert.equal(resp.status, 401);
});

// --- LOW: /schemas/* allowlist (internal schema shapes not exposed) ----------

test('classifyPath: only allowlisted schemas are public; others gated', () => {
  assert.equal(classifyPath('/schemas/agentsrc.schema.json'), 'schemas');
  assert.equal(classifyPath('/schemas/hook.schema.json'), 'schemas');
  assert.equal(classifyPath('/schemas/plugin.schema.json'), 'schemas');
  // internal operational schemas must NOT be public.
  assert.equal(
    classifyPath('/schemas/workflow-delegation-bundle.schema.json'),
    'internal',
  );
  assert.equal(classifyPath('/schemas/dashboard-run.schema.json'), 'internal');
  assert.equal(classifyPath('/schemas/verification-result.schema.json'), 'internal');
  // the bare listing is gated (it would enumerate internal schema filenames).
  assert.equal(classifyPath('/schemas'), 'internal');
  assert.equal(classifyPath('/schemas/'), 'internal');
});

test('fetch: an internal schema is BLOCKED (401, not served)', async () => {
  const resp = await worker.fetch(
    req('/schemas/workflow-delegation-bundle.schema.json'),
    env,
  );
  assert.equal(resp.status, 401, 'internal schema shape must not be public');
  const body = await resp.text();
  assert.ok(!body.startsWith('ASSET:'));
});

test('fetch: an allowlisted public schema serves unauthenticated (200)', async () => {
  const resp = await worker.fetch(req('/schemas/hook.schema.json'), env);
  assert.equal(resp.status, 200);
  assert.equal(await resp.text(), 'ASSET:/schemas/hook.schema.json');
});

// --- JWT hardening (with a configured, non-placeholder env) -------------------

const configuredEnv = {
  ASSETS,
  CF_ACCESS_AUD: env.CF_ACCESS_AUD,
  CF_ACCESS_TEAM_DOMAIN: 'realteam.cloudflareaccess.com',
};

const b64u = (obj) =>
  Buffer.from(JSON.stringify(obj))
    .toString('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');

const futureExp = () => Math.floor(Date.now() / 1000) + 3600;
const pastExp = () => Math.floor(Date.now() / 1000) - 3600;

test('JWT: alg=none is rejected (no algorithm confusion)', async () => {
  const tok = `${b64u({ alg: 'none', kid: 'k1' })}.${b64u({ aud: [configuredEnv.CF_ACCESS_AUD] })}.`;
  const r = await verifyAccessJwt(
    req('/internal/x', { 'Cf-Access-Jwt-Assertion': tok }),
    configuredEnv,
  );
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'unexpected-alg');
});

test('JWT: HS256 (symmetric key-confusion) is rejected', async () => {
  const tok = `${b64u({ alg: 'HS256', kid: 'k1' })}.${b64u({ aud: [configuredEnv.CF_ACCESS_AUD] })}.sig`;
  const r = await verifyAccessJwt(
    req('/internal/x', { 'Cf-Access-Jwt-Assertion': tok }),
    configuredEnv,
  );
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'unexpected-alg');
});

test('JWT: missing kid is rejected (no undefined-kid match)', async () => {
  const tok = `${b64u({ alg: 'RS256' })}.${b64u({ aud: [configuredEnv.CF_ACCESS_AUD] })}.sig`;
  const r = await verifyAccessJwt(
    req('/internal/x', { 'Cf-Access-Jwt-Assertion': tok }),
    configuredEnv,
  );
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'missing-kid');
});

test('JWT: unconfigured gate (placeholder team domain) fails closed', async () => {
  const tok = `${b64u({ alg: 'RS256', kid: 'k1' })}.${b64u({ aud: [env.CF_ACCESS_AUD], exp: futureExp() })}.sig`;
  // env still carries the REPLACE-ME placeholder team domain.
  const r = await verifyAccessJwt(
    req('/internal/x', { 'Cf-Access-Jwt-Assertion': tok }),
    env,
  );
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'gate-unconfigured');
});

test('JWT: expired token rejected (configured env)', async () => {
  const tok = `${b64u({ alg: 'RS256', kid: 'k1' })}.${b64u({ aud: [configuredEnv.CF_ACCESS_AUD], iss: `https://${configuredEnv.CF_ACCESS_TEAM_DOMAIN}`, exp: pastExp() })}.sig`;
  const r = await verifyAccessJwt(
    req('/internal/x', { 'Cf-Access-Jwt-Assertion': tok }),
    configuredEnv,
  );
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'expired');
});

test('JWT: wrong audience rejected (configured env)', async () => {
  const tok = `${b64u({ alg: 'RS256', kid: 'k1' })}.${b64u({ aud: ['some-other-aud'], iss: `https://${configuredEnv.CF_ACCESS_TEAM_DOMAIN}`, exp: futureExp() })}.sig`;
  const r = await verifyAccessJwt(
    req('/internal/x', { 'Cf-Access-Jwt-Assertion': tok }),
    configuredEnv,
  );
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'bad-audience');
});

test('JWT: wrong issuer rejected (configured env)', async () => {
  const tok = `${b64u({ alg: 'RS256', kid: 'k1' })}.${b64u({ aud: [configuredEnv.CF_ACCESS_AUD], iss: 'https://evil.cloudflareaccess.com', exp: futureExp() })}.sig`;
  const r = await verifyAccessJwt(
    req('/internal/x', { 'Cf-Access-Jwt-Assertion': tok }),
    configuredEnv,
  );
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'bad-issuer');
});
