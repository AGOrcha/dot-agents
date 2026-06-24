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
# keystore password (--storepass). In CI the workflow obtains it via the
# azure/login OIDC step + `az account get-access-token` and exports
# TRUSTED_SIGNING_TOKEN. Locally, a prior `az login` plus exporting the same
# variable is enough.
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
: "${TRUSTED_SIGNING_TOKEN:?set TRUSTED_SIGNING_TOKEN (Azure access token for https://codesigning.azure.net)}"

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

# Trusted Signing certs live ~3 days, so jsign auto-enables RFC3161 timestamping;
# --tsaurl pins the timestamp authority explicitly for reproducibility.
echo "sign-windows: Authenticode-signing ${binary} via jsign + Trusted Signing profile ${TRUSTED_SIGNING_PROFILE}"
java -jar "${jsign_jar}" \
  --storetype TRUSTEDSIGNING \
  --keystore "${endpoint}" \
  --storepass "${TRUSTED_SIGNING_TOKEN}" \
  --alias "${TRUSTED_SIGNING_ACCOUNT}/${TRUSTED_SIGNING_PROFILE}" \
  --alg SHA-256 \
  --tsaurl "${TRUSTED_SIGNING_TSA:-http://timestamp.acs.microsoft.com}" \
  "${binary}"
