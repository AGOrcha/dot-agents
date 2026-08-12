import assert from "node:assert/strict";
import test from "node:test";
import { AccessJwtVerifier, CloudflareAccessJwksProvider, StaticJwksProvider } from "../src/access-jwt.ts";
import { enforceAccessGate } from "../src/auth-gate.ts";

const keyPair = await crypto.subtle.generateKey(
  {
    name: "RSASSA-PKCS1-v1_5",
    modulusLength: 2048,
    publicExponent: new Uint8Array([1, 0, 1]),
    hash: "SHA-256",
  },
  true,
  ["sign", "verify"],
);
const publicJwk = await crypto.subtle.exportKey("jwk", keyPair.publicKey);
Object.assign(publicJwk, { alg: "RS256", kid: "obs-dev-1", use: "sig" });
const fixtureJwks = { keys: [publicJwk] };
const fixtureEnv = {
  OBS_AUTH_MODE: "fixture-jwt",
  ENVIRONMENT: "development",
  OBS_FIXTURE_JWKS: JSON.stringify(fixtureJwks),
};

function base64Url(value: unknown): string {
  return Buffer.from(typeof value === "string" ? value : JSON.stringify(value)).toString(
    "base64url",
  );
}

async function signedAssertion(
  claimOverrides: Record<string, unknown> = {},
  headerOverrides: Record<string, unknown> = {},
): Promise<string> {
  const now = Math.floor(Date.now() / 1000);
  const encodedHeader = base64Url({
    alg: "RS256",
    kid: "obs-dev-1",
    typ: "JWT",
    ...headerOverrides,
  });
  const encodedClaims = base64Url({
    iss: "https://obs.test.invalid",
    aud: ["obs-dev"],
    sub: "fixture-cli",
    iat: now,
    nbf: now - 1,
    exp: now + 300,
    ...claimOverrides,
  });
  const signed = new TextEncoder().encode(`${encodedHeader}.${encodedClaims}`);
  const signature = await crypto.subtle.sign(
    "RSASSA-PKCS1-v1_5",
    keyPair.privateKey,
    signed,
  );
  return `${encodedHeader}.${encodedClaims}.${Buffer.from(signature).toString("base64url")}`;
}

async function gatedRequest(
  assertion: string | null,
  path = "/api/v1/observability/runs",
  env = fixtureEnv,
): Promise<{ response: Response; dispatches: number; dispatchedPath: string | null }> {
  const headers = new Headers();
  if (assertion !== null) {
    headers.set("Cf-Access-Jwt-Assertion", assertion);
  }
  const request = new Request(`https://localhost${path}`, { headers });
  let dispatches = 0;
  let dispatchedPath: string | null = null;
  const response = await enforceAccessGate(request, env, async (authorizedRequest) => {
    dispatches += 1;
    dispatchedPath = new URL(authorizedRequest.url).pathname;
    return new Response("dispatched", { status: 200 });
  });
  return { response, dispatches, dispatchedPath };
}

test("a valid signed fixture assertion reaches dispatch", async () => {
  const result = await gatedRequest(await signedAssertion());
  assert.equal(result.response.status, 200);
  assert.equal(result.dispatches, 1);
  assert.equal(result.dispatchedPath, "/api/v1/observability/runs");
});

// claims is a thunk, not a plain object: the fixture ARRAY is built once at
// module-load time (before crypto.subtle.generateKey and every earlier test
// have run), but a time-relative claim must be computed relative to when the
// test actually EXECUTES, not when the array literal was evaluated. A
// "beyond skew" nbf only has a clockSkewSeconds-sized (60s, access-jwt.ts)
// margin before the wall clock catches up to it and the gate would legally
// start accepting it — with a fixed +61 offset captured eagerly, ordinary CI
// setup latency (RSA keypair generation, prior subtests) can close that
// 1-second margin and flip the assertion from correctly-rejected (403) to
// incorrectly-accepted (200), observed as a flaky failure. "expired exp"
// only drifts further into the past as time passes, so it was never at risk
// the same way, but every fixture is lazy here for consistency and to avoid
// the same class of bug recurring for a future time-relative claim.
for (const fixture of [
  { name: "wrong audience", claims: () => ({ aud: ["other-app"] }) },
  { name: "wrong issuer", claims: () => ({ iss: "https://other.cloudflareaccess.com" }) },
  { name: "expired exp", claims: () => ({ exp: Math.floor(Date.now() / 1000) - 1 }) },
  { name: "nbf beyond skew", claims: () => ({ nbf: Math.floor(Date.now() / 1000) + 61 }) },
]) {
  test(`${fixture.name} is rejected before dispatch`, async () => {
    const result = await gatedRequest(await signedAssertion(fixture.claims()));
    assert.equal(result.response.status, 403);
    assert.equal(result.dispatches, 0);
  });
}

for (const algorithm of ["none", "HS256"]) {
  test(`alg ${algorithm} is rejected before dispatch`, async () => {
    const result = await gatedRequest(
      await signedAssertion({}, { alg: algorithm }),
    );
    assert.equal(result.response.status, 403);
    assert.equal(result.dispatches, 0);
  });
}

test("an assertion with an unknown kid is rejected", async () => {
  const result = await gatedRequest(
    await signedAssertion({}, { kid: "unknown-key" }),
  );
  assert.equal(result.response.status, 403);
  assert.equal(result.dispatches, 0);
});

test("a bad signature is rejected", async () => {
  const assertion = await signedAssertion();
  // Corrupt the FIRST char of the signature segment. A byte-aligned signature's
  // LAST base64url char carries only 2 significant bits (2048 mod 6 == 512 mod 6
  // == 2), so an A<->B flip there decodes to identical bytes ~25% of the time —
  // the tamper becomes a no-op and the "bad" signature verifies (a flaky false
  // pass). The first char is fully significant, so flipping it always changes the
  // decoded signature.
  const parts = assertion.split(".");
  parts[2] = (parts[2][0] === "A" ? "B" : "A") + parts[2].slice(1);
  const result = await gatedRequest(parts.join("."));
  assert.equal(result.response.status, 403);
  assert.equal(result.dispatches, 0);
});

test("a missing assertion fails closed", async () => {
  const result = await gatedRequest(null);
  assert.equal(result.response.status, 403);
  assert.equal(result.dispatches, 0);
});

for (const path of [
  "/api%2f..",
  "//api/v1/observability/runs",
  "/API/v1/observability/runs",
]) {
  test(`${path} cannot bypass the gate`, async () => {
    const result = await gatedRequest(null, path);
    assert.equal(result.response.status, 403);
    assert.equal(result.dispatches, 0);
  });
}

test("case and duplicate slash variants normalize before dispatch", async () => {
  const assertion = await signedAssertion();
  for (const path of [
    "//API/v1/observability/runs",
    "/api//v1/observability/runs",
  ]) {
    const result = await gatedRequest(assertion, path);
    assert.equal(result.response.status, 200);
    assert.equal(result.dispatchedPath, "/api/v1/observability/runs");
  }
});

test("fixture mode rejects production and non-loopback requests", async () => {
  const assertion = await signedAssertion();
  const production = await gatedRequest(assertion, "/api/v1/observability/runs", {
    ...fixtureEnv,
    ENVIRONMENT: "production",
  });
  assert.equal(production.response.status, 403);
  assert.equal(production.dispatches, 0);

  const request = new Request("https://obs.example.com/api/v1/observability/runs", {
    headers: { "Cf-Access-Jwt-Assertion": assertion },
  });
  let dispatches = 0;
  const response = await enforceAccessGate(request, fixtureEnv, async () => {
    dispatches += 1;
    return new Response(null, { status: 200 });
  });
  assert.equal(response.status, 403);
  assert.equal(dispatches, 0);
});

test("production access mode with no assertion fails closed", async () => {
  const request = new Request("https://obs.agorcha.dev/api/v1/observability/runs");
  let dispatches = 0;
  const response = await enforceAccessGate(
    request,
    {
      OBS_AUTH_MODE: "access",
      ENVIRONMENT: "production",
      OBS_ACCESS_AUD: "configured-audience",
      OBS_ACCESS_TEAM_DOMAIN: "usepayout.cloudflareaccess.com",
    },
    async () => {
      dispatches += 1;
      return new Response(null, { status: 200 });
    },
  );
  assert.equal(response.status, 403);
  assert.equal(dispatches, 0);
});

test("the verifier returns the signed subject through the static provider", async () => {
  const verifier = new AccessJwtVerifier(new StaticJwksProvider(fixtureJwks.keys), {
    issuer: "https://obs.test.invalid",
    audience: "obs-dev",
  });
  assert.deepEqual(await verifier.verify(await signedAssertion()), {
    subject: "fixture-cli",
  });
});

test("CloudflareAccessJwksProvider calls fetch with global this, not the provider (illegal-invocation guard)", async () => {
  // Regression: storing `fetch` on the instance and calling `this.fetcher(...)`
  // invokes it with the provider as `this`, which the Workers runtime rejects
  // with "Illegal invocation". The provider must bind fetch to globalThis.
  let capturedThis: unknown = "unset";
  function recordingFetch(this: unknown): Promise<Response> {
    capturedThis = this;
    return Promise.resolve(
      new Response(JSON.stringify({ keys: [{ kid: "k1", kty: "RSA" }] }), { status: 200 }),
    );
  }
  const provider = new CloudflareAccessJwksProvider(
    "team.cloudflareaccess.com",
    recordingFetch as unknown as typeof fetch,
  );
  await provider.getJwks();
  assert.notEqual(capturedThis, provider);
});
