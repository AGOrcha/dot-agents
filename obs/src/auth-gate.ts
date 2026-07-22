import {
  AccessJwtVerifier,
  CloudflareAccessJwksProvider,
  StaticJwksProvider,
} from "./access-jwt";

const FIXTURE_ISSUER = "https://obs.test.invalid";
const FIXTURE_AUDIENCE = "obs-dev";

export interface AccessEnvironment {
  OBS_AUTH_MODE?: string;
  ENVIRONMENT?: string;
  OBS_ACCESS_AUD?: string;
  OBS_ACCESS_TEAM_DOMAIN?: string;
  OBS_FIXTURE_JWKS?: string;
}

const accessVerifiers = new Map<string, AccessJwtVerifier>();
let fixtureVerifierCache:
  | { serializedJwks: string; verifier: AccessJwtVerifier }
  | undefined;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isLoopback(hostname: string): boolean {
  return (
    hostname === "localhost" ||
    hostname === "127.0.0.1" ||
    hostname === "[::1]" ||
    hostname === "::1"
  );
}

function normalizeRequestPath(request: Request): Request | null {
  const url = new URL(request.url);
  let decodedPath: string;
  try {
    decodedPath = decodeURIComponent(url.pathname).replaceAll("\\", "/");
  } catch {
    return null;
  }
  if (!decodedPath.startsWith("/") || decodedPath.includes("\0")) {
    return null;
  }

  const segments: string[] = [];
  for (const segment of decodedPath.split("/")) {
    if (segment.length === 0 || segment === ".") {
      continue;
    }
    if (segment === "..") {
      segments.pop();
      continue;
    }
    segments.push(segment);
  }

  let normalizedPath = `/${segments.join("/")}`;
  if (normalizedPath.toLowerCase().startsWith("/api/")) {
    normalizedPath = normalizedPath.toLowerCase();
  }
  if (normalizedPath === url.pathname) {
    return request;
  }
  url.pathname = normalizedPath;
  return new Request(url, request);
}

function fixtureVerifier(serializedJwks: string): AccessJwtVerifier | null {
  if (fixtureVerifierCache?.serializedJwks === serializedJwks) {
    return fixtureVerifierCache.verifier;
  }

  try {
    const document: unknown = JSON.parse(serializedJwks);
    if (!isRecord(document) || !Array.isArray(document.keys)) {
      return null;
    }
    const jwks = document.keys.filter(isRecord) as JsonWebKey[];
    if (jwks.length !== document.keys.length || jwks.length === 0) {
      return null;
    }
    const verifier = new AccessJwtVerifier(new StaticJwksProvider(jwks), {
      issuer: FIXTURE_ISSUER,
      audience: FIXTURE_AUDIENCE,
    });
    fixtureVerifierCache = { serializedJwks, verifier };
    return verifier;
  } catch {
    return null;
  }
}

function configuredVerifier(
  request: Request,
  env: AccessEnvironment,
): AccessJwtVerifier | null {
  if (env.OBS_AUTH_MODE === "fixture-jwt") {
    if (
      env.ENVIRONMENT !== "development" ||
      !isLoopback(new URL(request.url).hostname) ||
      typeof env.OBS_FIXTURE_JWKS !== "string" ||
      env.OBS_FIXTURE_JWKS.length === 0
    ) {
      return null;
    }
    return fixtureVerifier(env.OBS_FIXTURE_JWKS);
  }

  if (env.OBS_AUTH_MODE !== "access") {
    return null;
  }
  if (
    typeof env.OBS_ACCESS_AUD !== "string" ||
    env.OBS_ACCESS_AUD.length === 0 ||
    typeof env.OBS_ACCESS_TEAM_DOMAIN !== "string" ||
    env.OBS_ACCESS_TEAM_DOMAIN.length === 0
  ) {
    return null;
  }

  const cacheKey = `${env.OBS_ACCESS_TEAM_DOMAIN}\n${env.OBS_ACCESS_AUD}`;
  const cached = accessVerifiers.get(cacheKey);
  if (cached !== undefined) {
    return cached;
  }
  try {
    const verifier = new AccessJwtVerifier(
      new CloudflareAccessJwksProvider(env.OBS_ACCESS_TEAM_DOMAIN),
      {
        issuer: `https://${env.OBS_ACCESS_TEAM_DOMAIN}`,
        audience: env.OBS_ACCESS_AUD,
      },
    );
    accessVerifiers.set(cacheKey, verifier);
    return verifier;
  } catch {
    return null;
  }
}

export async function verifyAccess(
  request: Request,
  env: AccessEnvironment,
): Promise<{ subject: string } | null> {
  const assertion = request.headers.get("Cf-Access-Jwt-Assertion");
  if (assertion === null || assertion.length === 0) {
    return null;
  }
  const verifier = configuredVerifier(request, env);
  if (verifier === null) {
    return null;
  }
  try {
    return await verifier.verify(assertion);
  } catch {
    return null;
  }
}

export async function enforceAccessGate(
  request: Request,
  env: AccessEnvironment,
  dispatch: (authorizedRequest: Request) => Promise<Response>,
): Promise<Response> {
  const normalizedRequest = normalizeRequestPath(request);
  if (normalizedRequest === null) {
    return new Response(
      JSON.stringify({ error: { code: "invalid_path", message: "request path is invalid" } }),
      { status: 400, headers: { "content-type": "application/json" } },
    );
  }

  const assertion = await verifyAccess(normalizedRequest, env);
  if (assertion === null) {
    return new Response(
      JSON.stringify({
        error: {
          code: "forbidden",
          message: "a verified Cloudflare Access assertion is required",
        },
      }),
      { status: 403, headers: { "content-type": "application/json" } },
    );
  }
  return dispatch(normalizedRequest);
}
