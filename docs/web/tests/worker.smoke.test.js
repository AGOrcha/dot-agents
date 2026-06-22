// LIVE Worker smoke — runs the REAL src/worker.js through Miniflare against a
// real static-assets directory, so it validates the actual asset-vs-worker
// routing (not just the handler with a stubbed ASSETS). This is the guard the
// Codex security review asked for: a misrouted /internal asset, or a
// run_worker_first regression, must surface here as a served-200 instead of 401.
//
// The team domain is the production placeholder, so JWT verification fails
// closed — exactly the posture until the maintainer sets the real team domain.
// The invariant under test: unauthenticated GET /internal/<page> is 401 (NOT
// 200, NOT a 3xx login redirect, NOT an empty body), while public + allowlisted
// schemas serve unauthenticated.

import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { Miniflare } from 'miniflare';
import { mkdtemp, mkdir, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const workerPath = path.join(here, '..', 'src', 'worker.js');

let mf;
let distDir;

before(async () => {
  // Build a minimal fixture dist tree that mirrors the merged output: a public
  // page, an allowlisted public schema, an internal schema, and a gated internal
  // page. The marker bodies make a leak unambiguous.
  distDir = await mkdtemp(path.join(tmpdir(), 'worker-smoke-'));
  await writeFile(path.join(distDir, 'index.html'), 'PUBLIC-HOME');
  await mkdir(path.join(distDir, 'guides'), { recursive: true });
  await writeFile(path.join(distDir, 'guides', 'index.html'), 'PUBLIC-GUIDE');
  await mkdir(path.join(distDir, 'schemas'), { recursive: true });
  await writeFile(path.join(distDir, 'schemas', 'hook.schema.json'), '{"public":true}');
  await writeFile(
    path.join(distDir, 'schemas', 'workflow-delegation-bundle.schema.json'),
    '{"internal":true}',
  );
  await mkdir(path.join(distDir, 'internal', 'lessons'), { recursive: true });
  await writeFile(
    path.join(distDir, 'internal', 'lessons', 'index.html'),
    'INTERNAL-SECRET-LESSON',
  );

  const script = await readFile(workerPath, 'utf8');

  mf = new Miniflare({
    modules: true,
    script,
    scriptPath: workerPath,
    assets: {
      directory: distDir,
      binding: 'ASSETS',
      // The worker MUST run before assets so the gate always applies — even for a
      // request that matches an existing asset file (e.g. dist/internal/lessons/
      // index.html). Miniflare models Cloudflare's `run_worker_first: true` via
      // routerConfig.invoke_user_worker_ahead_of_assets. The production
      // wrangler.jsonc uses the same fail-closed posture (run_worker_first: true).
      routerConfig: {
        has_user_worker: true,
        invoke_user_worker_ahead_of_assets: true,
      },
    },
    bindings: {
      CF_ACCESS_AUD:
        '50476bf0ad28d003fbf4cfb1d36ed2554907f7f2e0e6c699f5fb4bc4b8d1a6ff',
      // Placeholder team domain -> verification fails closed (production posture).
      CF_ACCESS_TEAM_DOMAIN: 'REPLACE-ME.cloudflareaccess.com',
    },
  });
  // Touch the instance so startup errors surface in before().
  await mf.ready;
});

after(async () => {
  if (mf) await mf.dispose();
  if (distDir) await rm(distDir, { recursive: true, force: true });
});

async function get(p) {
  return mf.dispatchFetch(`https://agorcha.dev${p}`, { redirect: 'manual' });
}

test('LIVE: unauthenticated /internal/<page> is 401 (not 200/3xx/empty)', async () => {
  const resp = await get('/internal/lessons/');
  assert.equal(resp.status, 401, 'internal page must be 401 unauthenticated');
  // A login redirect (3xx) or an empty body would be a misconfig masquerading as
  // "protected" — treat both as failures.
  assert.ok(resp.status < 300 || resp.status >= 400, 'must not be a 3xx redirect');
  const body = await resp.text();
  assert.ok(
    !body.includes('INTERNAL-SECRET-LESSON'),
    'internal content must NOT be served',
  );
});

test('LIVE: the bare /internal is 401 (run_worker_first covers /internal)', async () => {
  const resp = await get('/internal');
  assert.equal(resp.status, 401);
});

test('LIVE: encoded-slash /internal%2fsecret is 401 (no bypass)', async () => {
  const resp = await get('/internal%2flessons');
  assert.equal(resp.status, 401);
});

test('LIVE: public home serves unauthenticated (200)', async () => {
  const resp = await get('/');
  assert.equal(resp.status, 200);
  assert.equal(await resp.text(), 'PUBLIC-HOME');
});

test('LIVE: allowlisted public schema serves unauthenticated (200)', async () => {
  const resp = await get('/schemas/hook.schema.json');
  assert.equal(resp.status, 200);
  assert.equal(await resp.text(), '{"public":true}');
});

test('LIVE: internal schema is 401 (not exposed)', async () => {
  const resp = await get('/schemas/workflow-delegation-bundle.schema.json');
  assert.equal(resp.status, 401);
  const body = await resp.text();
  assert.ok(!body.includes('"internal":true'), 'internal schema must not be served');
});
