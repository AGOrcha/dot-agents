#!/usr/bin/env bash
# GoReleaser post-build hook: Authenticode-sign a Windows binary with Azure
# Trusted Signing.
#
# GATED — a no-op unless TRUSTED_SIGNING_PROFILE is set. This keeps releases
# working before the Public Trust certificate profile exists (identity
# validation takes 1-7 days). Once the profile is created and the repo
# variables are populated, signing activates with no further code change.
#
# Invoked by .goreleaser.yaml as: scripts/sign-windows.sh <path-to-da.exe>
#
# Auth: the dotnet `sign` tool uses Azure DefaultAzureCredential. In CI that is
# satisfied by the azure/login OIDC step (federated, secretless) which logs in
# the `az` CLI; DefaultAzureCredential falls through to AzureCliCredential.
# Locally, a prior `az login` is enough.
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

# The dotnet `sign` tool (https://github.com/dotnet/sign) signs PE files with
# Trusted Signing and runs cross-platform, so it works from the ubuntu release
# runner. The workflow installs it; this guard keeps local runs honest.
command -v sign >/dev/null 2>&1 || {
  echo "sign-windows: the 'sign' dotnet tool is not on PATH; install with" >&2
  echo "  dotnet tool install --global sign --prerelease" >&2
  exit 1
}

echo "sign-windows: Authenticode-signing ${binary} via Trusted Signing profile ${TRUSTED_SIGNING_PROFILE}"
sign code trusted-signing \
  --trusted-signing-endpoint "${TRUSTED_SIGNING_ENDPOINT}" \
  --trusted-signing-account "${TRUSTED_SIGNING_ACCOUNT}" \
  --trusted-signing-certificate-profile "${TRUSTED_SIGNING_PROFILE}" \
  "${binary}"
