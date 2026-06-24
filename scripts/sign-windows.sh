#!/usr/bin/env bash
# GoReleaser post-build hook: Authenticode-sign a Windows binary with
# osslsigncode (a Linux-native C tool, no .NET / kernel32 dependency), so it
# runs cleanly on the ubuntu release runner.
#
# WHY NOT the dotnet `sign` tool: the unpinned `sign --prerelease` install now
# resolves to the stable 1.x line, which aborts on a non-Windows runner with
# `System.DllNotFoundException: Unable to load shared library 'kernel32.dll'`
# (Kernel32.SetDllDirectoryW in AppInitializer; see dotnet/sign#711). That
# broke the 0.4.0 release.
#
# CREDENTIALS: osslsigncode signs with a code-signing certificate as a
# PKCS#12/PFX bundle (+ password). It CANNOT use Azure Trusted Signing's cloud
# HSM (Trusted Signing never exposes an exportable key). So this path is keyed
# off a cert blob, NOT the TRUSTED_SIGNING_* / AZURE_* variables.
#
# Required env (all must be set to actually sign):
#   WINDOWS_CERT_P12       base64-encoded PKCS#12 (.pfx) code-signing bundle
#   WINDOWS_CERT_PASSWORD  password for the PKCS#12 bundle
# Optional:
#   WINDOWS_CERT_TSA       RFC3161 timestamp authority URL
#                          (default: http://timestamp.digicert.com)
#
# Invoked by .goreleaser.yaml as: scripts/sign-windows.sh <path-to-da.exe>
set -euo pipefail

binary="${1:?usage: sign-windows.sh <binary>}"

# The gate: no cert material -> nothing to sign with yet. Exit clean so the
# release proceeds with an unsigned Windows binary (today's behavior).
if [[ -z "${WINDOWS_CERT_P12:-}" ]]; then
  echo "sign-windows: WINDOWS_CERT_P12 unset -> skipping signing of ${binary}"
  exit 0
fi

: "${WINDOWS_CERT_PASSWORD:?set WINDOWS_CERT_PASSWORD (PKCS#12 password)}"
tsa="${WINDOWS_CERT_TSA:-http://timestamp.digicert.com}"

command -v osslsigncode >/dev/null 2>&1 || {
  echo "sign-windows: osslsigncode is not on PATH; install with" >&2
  echo "  sudo apt-get update && sudo apt-get install -y osslsigncode" >&2
  exit 1
}

# Decode the PKCS#12 to a temp file scrubbed on exit (never written to logs).
pfx="$(mktemp)"
trap 'rm -f "${pfx}"' EXIT
printf '%s' "${WINDOWS_CERT_P12}" | base64 -d >"${pfx}"

echo "sign-windows: Authenticode-signing ${binary} via osslsigncode (TSA ${tsa})"
osslsigncode sign \
  -pkcs12 "${pfx}" \
  -pass "${WINDOWS_CERT_PASSWORD}" \
  -h sha256 \
  -n "dot-agents" \
  -i "https://github.com/AGOrcha/dot-agents" \
  -ts "${tsa}" \
  -in "${binary}" \
  -out "${binary}.signed"
mv "${binary}.signed" "${binary}"
