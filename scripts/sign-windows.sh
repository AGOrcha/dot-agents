#!/usr/bin/env bash
# GoReleaser post-build hook: Authenticode-sign a Windows binary with jsign
# (https://ebourg.github.io/jsign/) against Azure Trusted Signing.
#
# jsign is a JVM tool (Linux-native, no .NET / kernel32 dependency) that
# natively supports Azure Trusted Signing via the TRUSTEDSIGNING storetype, so
# it runs cleanly on the ubuntu release runner AND keeps the existing Trusted
# Signing infrastructure (OIDC + the agorcha-public-trust profile). It replaces
# the Microsoft dotnet `sign` tool, whose stable 1.x line aborts on Linux with
# `System.DllNotFoundException: Unable to load shared library 'kernel32.dll'`
# (Kernel32.SetDllDirectoryW in AppInitializer; dotnet/sign#711) — that crash
# broke the 0.4.0 release.
#
# GATED — a no-op unless TRUSTED_SIGNING_PROFILE is set. This keeps releases
# working before the Public Trust certificate profile exists. Once the profile
# is created and the repo variables are populated, signing activates with no
# further code change.
#
# Invoked by .goreleaser.yaml as: scripts/sign-windows.sh <path-to-da.exe>
#
# Auth: jsign reads an Azure access token (for the codesigning resource) as the
# keystore password (--storepass). To keep the token out of the process argument
# list (and any `set -x` trace), it is passed by REFERENCE, never by value:
#
#   * TRUSTED_SIGNING_TOKEN_FILE -> jsign reads it via `--storepass file:<path>`
#     (preferred; this is how CI runs — see .github/workflows/auto-release.yml,
#     which mints the token at job time via OIDC + `az account get-access-token`
#     and writes it to a 0600 runner-local file).
#   * TRUSTED_SIGNING_TOKEN      -> jsign reads it via `--storepass env:VARNAME`
#     (back-compat / local convenience; still avoids a CLI-arg leak because only
#     the variable NAME is passed to jsign, not its value).
#
# Locally, `az login` followed by either:
#   az account get-access-token --resource https://codesigning.azure.net \
#     --query accessToken -o tsv > "$TRUSTED_SIGNING_TOKEN_FILE"
# or exporting TRUSTED_SIGNING_TOKEN is enough.
set -euo pipefail

binary="${1:?usage: sign-windows.sh <binary>}"

# The gate: no profile -> nothing to sign against yet. Exit clean so the release
# proceeds with an unsigned Windows binary (today's behavior).
if [[ -z "${TRUSTED_SIGNING_PROFILE:-}" ]]; then
  echo "sign-windows: TRUSTED_SIGNING_PROFILE unset -> skipping signing of ${binary}"
  exit 0
fi

: "${TRUSTED_SIGNING_ENDPOINT:?set TRUSTED_SIGNING_ENDPOINT (e.g. https://eus.codesigning.azure.net/)}"
: "${TRUSTED_SIGNING_ACCOUNT:?set TRUSTED_SIGNING_ACCOUNT}"

# Resolve the Azure access token by REFERENCE so the value never appears in this
# script's argument list. Prefer the runner-local file (CI); fall back to the
# env var for local use. Either way --storepass gets a jsign indirection prefix
# (file:/env:), not the secret itself.
if [[ -n "${TRUSTED_SIGNING_TOKEN_FILE:-}" ]]; then
  [[ -f "${TRUSTED_SIGNING_TOKEN_FILE}" ]] || {
    echo "sign-windows: TRUSTED_SIGNING_TOKEN_FILE=${TRUSTED_SIGNING_TOKEN_FILE} does not exist" >&2
    exit 1
  }
  storepass_ref="file:${TRUSTED_SIGNING_TOKEN_FILE}"
elif [[ -n "${TRUSTED_SIGNING_TOKEN:-}" ]]; then
  storepass_ref="env:TRUSTED_SIGNING_TOKEN"
else
  echo "sign-windows: set TRUSTED_SIGNING_TOKEN_FILE (path to the Azure access token for https://codesigning.azure.net) or TRUSTED_SIGNING_TOKEN" >&2
  exit 1
fi

# jsign rejects an endpoint with a trailing slash (it forms a broken path), so
# strip it. The endpoint variable carries the canonical https://...azure.net/.
endpoint="${TRUSTED_SIGNING_ENDPOINT%/}"

# jsign is launched via the pinned, checksum-verified jar the workflow stages at
# $JSIGN_JAR (a JRE is already on the runner for the Sonar scanner).
jsign_jar="${JSIGN_JAR:-/usr/local/lib/jsign.jar}"
[[ -f "${jsign_jar}" ]] || {
  echo "sign-windows: jsign jar not found at ${jsign_jar}; the workflow stages it (set JSIGN_JAR)" >&2
  exit 1
}
command -v java >/dev/null 2>&1 || {
  echo "sign-windows: java is not on PATH; jsign needs a JRE" >&2
  exit 1
}

# Trusted Signing certs live ~3 days, so the signature MUST be timestamped.
# jsign defaults to --tsmode Authenticode; with an RFC3161 timestamp server that
# mismatch makes jsign parse the RFC3161 response in Authenticode mode and crash
# (CMSException: Malformed content -> DLSequence cannot be cast to
# ASN1ObjectIdentifier in AuthenticodeTimestamper). So pin --tsmode RFC3161 to
# match the RFC3161 TSAs below.
#
# --tsaurl takes a comma-separated list for failover (jsign tries them in order);
# both defaults are public RFC3161 servers. TRUSTED_SIGNING_TSA, if set, fully
# overrides the list. --tsretries/--tsretrywait keep transient TSA blips from
# failing the release.
tsa="${TRUSTED_SIGNING_TSA:-http://timestamp.digicert.com,http://timestamp.sectigo.com}"
echo "sign-windows: Authenticode-signing ${binary} via jsign + Trusted Signing profile ${TRUSTED_SIGNING_PROFILE}"
java -jar "${jsign_jar}" \
  --storetype TRUSTEDSIGNING \
  --keystore "${endpoint}" \
  --storepass "${storepass_ref}" \
  --alias "${TRUSTED_SIGNING_ACCOUNT}/${TRUSTED_SIGNING_PROFILE}" \
  --alg SHA-256 \
  --tsmode RFC3161 \
  --tsaurl "${tsa}" \
  --tsretries 3 \
  --tsretrywait 10 \
  "${binary}"
