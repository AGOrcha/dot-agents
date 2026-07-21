import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const forwardedArgs = [];
let assertionFile;
for (let index = 2; index < process.argv.length; index += 1) {
  if (process.argv[index] === "--assertion-file") {
    assertionFile = process.argv[index + 1];
    if (!assertionFile) {
      throw new Error("--assertion-file requires a path");
    }
    index += 1;
  } else {
    forwardedArgs.push(process.argv[index]);
  }
}

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

const base64Url = (value) =>
  Buffer.from(typeof value === "string" ? value : JSON.stringify(value)).toString("base64url");
const now = Math.floor(Date.now() / 1000);
const encodedHeader = base64Url({ alg: "RS256", kid: "obs-dev-1", typ: "JWT" });
const encodedClaims = base64Url({
  iss: "https://obs.test.invalid",
  aud: ["obs-dev"],
  sub: "fixture-cli",
  iat: now,
  nbf: now - 1,
  exp: now + 300,
});
const signed = new TextEncoder().encode(`${encodedHeader}.${encodedClaims}`);
const signature = await crypto.subtle.sign(
  "RSASSA-PKCS1-v1_5",
  keyPair.privateKey,
  signed,
);
const assertion = `${encodedHeader}.${encodedClaims}.${Buffer.from(signature).toString("base64url")}`;

assertionFile ??= fileURLToPath(
  new URL("../.wrangler/obs-fixture.jwt", import.meta.url),
);
await mkdir(dirname(assertionFile), { recursive: true });
await writeFile(assertionFile, `${assertion}\n`, { mode: 0o600 });
console.log(`Fixture assertion written to ${assertionFile}`);

const wrangler = fileURLToPath(new URL("../node_modules/.bin/wrangler", import.meta.url));
const child = spawn(
  wrangler,
  [
    "dev",
    ...forwardedArgs,
    "--host",
    "localhost",
    "--var",
    "OBS_AUTH_MODE:fixture-jwt",
    "--var",
    "ENVIRONMENT:development",
    "--var",
    `OBS_FIXTURE_JWKS:${JSON.stringify({ keys: [publicJwk] })}`,
  ],
  { stdio: "inherit" },
);

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => child.kill(signal));
}

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code ?? 1);
  }
});
