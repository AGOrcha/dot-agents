const JWKS_TTL_MS = 60 * 60 * 1000;
const TEAM_DOMAIN_PATTERN = /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.cloudflareaccess\.com$/;

export interface AccessJwksProvider {
  getJwks(): Promise<JsonWebKey[]>;
}

export interface AccessJwtVerifierOptions {
  issuer: string;
  audience: string;
  clockSkewSeconds?: number;
  now?: () => number;
}

interface JwtHeader {
  alg?: unknown;
  kid?: unknown;
}

interface JwtClaims {
  aud?: unknown;
  exp?: unknown;
  iss?: unknown;
  nbf?: unknown;
  sub?: unknown;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function decodeBase64Url(value: string): Uint8Array<ArrayBuffer> {
  if (value.length === 0 || !/^[A-Za-z0-9_-]+$/.test(value) || value.length % 4 === 1) {
    throw new Error("invalid Access assertion");
  }
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
  const decoded = atob(padded);
  const bytes = new Uint8Array(new ArrayBuffer(decoded.length));
  for (let index = 0; index < decoded.length; index += 1) {
    bytes[index] = decoded.charCodeAt(index);
  }
  return bytes;
}

function decodeJsonSegment(value: string): Record<string, unknown> {
  const decoded: unknown = JSON.parse(new TextDecoder().decode(decodeBase64Url(value)));
  if (!isRecord(decoded)) {
    throw new Error("invalid Access assertion");
  }
  return decoded;
}

function isNumericDate(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function audienceContains(audience: unknown, expected: string): boolean {
  if (typeof audience === "string") {
    return audience === expected;
  }
  return Array.isArray(audience) && audience.some((value) => value === expected);
}

export class StaticJwksProvider implements AccessJwksProvider {
  private readonly jwks: JsonWebKey[];

  constructor(jwks: JsonWebKey[]) {
    this.jwks = jwks;
  }
  async getJwks(): Promise<JsonWebKey[]> {
    return this.jwks;
  }
}

export class CloudflareAccessJwksProvider implements AccessJwksProvider {
  private cachedJwks: JsonWebKey[] = [];
  private expiresAt = 0;
  private inFlight: Promise<JsonWebKey[]> | null = null;
  private readonly teamDomain: string;
  private readonly fetcher: typeof fetch;

  constructor(teamDomain: string, fetcher: typeof fetch = fetch) {
    if (!TEAM_DOMAIN_PATTERN.test(teamDomain)) {
      throw new Error("invalid Cloudflare Access team domain");
    }
    this.teamDomain = teamDomain;
    this.fetcher = fetcher;
  }

  async getJwks(): Promise<JsonWebKey[]> {
    if (this.expiresAt > Date.now()) {
      return this.cachedJwks;
    }
    if (this.inFlight !== null) {
      return this.inFlight;
    }

    this.inFlight = this.refresh();
    try {
      return await this.inFlight;
    } finally {
      this.inFlight = null;
    }
  }

  private async refresh(): Promise<JsonWebKey[]> {
    const response = await this.fetcher(
      `https://${this.teamDomain}/cdn-cgi/access/certs`,
    );
    if (!response.ok) {
      throw new Error(`Cloudflare Access JWKS request failed with ${response.status}`);
    }

    const document: unknown = await response.json();
    if (!isRecord(document) || !Array.isArray(document.keys)) {
      throw new Error("Cloudflare Access JWKS response is malformed");
    }

    const keysByKid = new Map<string, JsonWebKey>();
    for (const candidate of document.keys) {
      if (!isRecord(candidate)) {
        throw new Error("Cloudflare Access JWKS response is malformed");
      }
      const key = candidate as JsonWebKey;
      const kid = Reflect.get(key, "kid");
      if (typeof kid !== "string" || kid.length === 0) {
        continue;
      }
      if (keysByKid.has(kid)) {
        throw new Error("Cloudflare Access JWKS contains a duplicate kid");
      }
      keysByKid.set(kid, key);
    }

    this.cachedJwks = [...keysByKid.values()];
    this.expiresAt = Date.now() + JWKS_TTL_MS;
    return this.cachedJwks;
  }
}

export class AccessJwtVerifier {
  private readonly clockSkewSeconds: number;
  private readonly now: () => number;
  private readonly jwksProvider: AccessJwksProvider;
  private readonly options: AccessJwtVerifierOptions;

  constructor(
    jwksProvider: AccessJwksProvider,
    options: AccessJwtVerifierOptions,
  ) {
    if (options.issuer.length === 0 || options.audience.length === 0) {
      throw new Error("Access JWT verifier configuration is incomplete");
    }
    this.jwksProvider = jwksProvider;
    this.options = options;
    this.clockSkewSeconds = options.clockSkewSeconds ?? 60;
    this.now = options.now ?? (() => Date.now());
  }

  async verify(assertion: string): Promise<{ subject: string }> {
    const parts = assertion.split(".");
    if (parts.length !== 3) {
      throw new Error("invalid Access assertion");
    }

    const [encodedHeader, encodedClaims, encodedSignature] = parts;
    const header = decodeJsonSegment(encodedHeader) as JwtHeader;
    const claims = decodeJsonSegment(encodedClaims) as JwtClaims;

    if (header.alg !== "RS256") {
      throw new Error("invalid Access assertion");
    }
    if (typeof header.kid !== "string" || header.kid.trim().length === 0) {
      throw new Error("invalid Access assertion");
    }

    const now = Math.floor(this.now() / 1000);
    if (!isNumericDate(claims.exp) || claims.exp <= now) {
      throw new Error("invalid Access assertion");
    }
    if (
      claims.nbf !== undefined &&
      (!isNumericDate(claims.nbf) || claims.nbf > now + this.clockSkewSeconds)
    ) {
      throw new Error("invalid Access assertion");
    }
    if (claims.iss !== this.options.issuer) {
      throw new Error("invalid Access assertion");
    }
    if (!audienceContains(claims.aud, this.options.audience)) {
      throw new Error("invalid Access assertion");
    }

    const jwks = await this.jwksProvider.getJwks();
    const matchingKeys = jwks.filter(
      (key) => Reflect.get(key, "kid") === header.kid,
    );
    if (matchingKeys.length !== 1) {
      throw new Error("invalid Access assertion");
    }
    const [jwk] = matchingKeys;
    if (jwk.kty !== "RSA" || (jwk.alg !== undefined && jwk.alg !== "RS256")) {
      throw new Error("invalid Access assertion");
    }

    let verificationKey: CryptoKey;
    try {
      verificationKey = await crypto.subtle.importKey(
        "jwk",
        jwk,
        { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
        false,
        ["verify"],
      );
    } catch {
      throw new Error("invalid Access assertion");
    }

    const signed = new TextEncoder().encode(`${encodedHeader}.${encodedClaims}`);
    let validSignature: boolean;
    try {
      validSignature = await crypto.subtle.verify(
        "RSASSA-PKCS1-v1_5",
        verificationKey,
        decodeBase64Url(encodedSignature),
        signed,
      );
    } catch {
      throw new Error("invalid Access assertion");
    }
    if (!validSignature) {
      throw new Error("invalid Access assertion");
    }

    return { subject: typeof claims.sub === "string" ? claims.sub : "" };
  }
}
