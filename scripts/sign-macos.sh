#!/usr/bin/env bash
# GoReleaser post-build hook: Developer ID-sign (+ notarize) a macOS binary with
# quill (https://github.com/anchore/quill). quill signs Mach-O binaries with an
# Apple Developer ID from ANY platform, so it runs on the ubuntu release runner —
# the macOS counterpart to scripts/sign-windows.sh (which uses dotnet `sign`).
#
# GATED — a no-op unless QUILL_SIGN_P12 is set. This keeps releases shipping
# (unsigned darwin binaries, today's behavior) until the Developer ID
# Application certificate + secrets are configured. It activates with no code
# change once the secrets exist.
#
# Invoked by .goreleaser.yaml as: scripts/sign-macos.sh <path-to-da> <goos>
# The dot-agents-unix build also produces linux binaries, so the goos guard
# skips everything that is not a darwin Mach-O.
#
# Signing inputs (env, read by quill):
#   QUILL_SIGN_P12        Developer ID Application cert as a .p12 (path or base64)
#   QUILL_SIGN_PASSWORD   the .p12 password
# Notarization inputs (env, optional — notarize only when all three are set):
#   QUILL_NOTARY_KEY      App Store Connect API key .p8 (path or base64)
#   QUILL_NOTARY_KEY_ID   the key id
#   QUILL_NOTARY_ISSUER   the issuer id
set -euo pipefail

binary="${1:?usage: sign-macos.sh <binary> <goos>}"
goos="${2:-}"

# This hook fires for every dot-agents-unix binary (linux + darwin). Only darwin
# Mach-O binaries are signable with quill; skip the rest cleanly.
if [[ "${goos}" != "darwin" ]]; then
  echo "sign-macos: ${binary} is goos=${goos:-unknown} (not darwin) -> skip"
  exit 0
fi

# The gate: no cert -> nothing to sign against yet. Exit clean so the release
# proceeds with an unsigned darwin binary (today's behavior).
if [[ -z "${QUILL_SIGN_P12:-}" ]]; then
  echo "sign-macos: QUILL_SIGN_P12 unset -> skipping signing of ${binary}"
  exit 0
fi

# quill signs Mach-O from Linux; the workflow installs it. This guard keeps local
# runs honest.
command -v quill >/dev/null 2>&1 || {
  echo "sign-macos: 'quill' is not on PATH; install from https://github.com/anchore/quill" >&2
  echo "  curl -sSfL https://raw.githubusercontent.com/anchore/quill/main/install.sh | sh -s -- -b /usr/local/bin" >&2
  exit 1
}

# Notarize only when the full App Store Connect API key is provided; otherwise
# sign only. Sign-only still leaves the binary Developer ID-signed (Gatekeeper
# will still warn on un-notarized downloads) — this lets signing land before
# notarization is fully wired.
if [[ -n "${QUILL_NOTARY_KEY:-}" && -n "${QUILL_NOTARY_KEY_ID:-}" && -n "${QUILL_NOTARY_ISSUER:-}" ]]; then
  echo "sign-macos: signing + notarizing ${binary} via quill"
  quill sign-and-notarize "${binary}"
else
  echo "sign-macos: QUILL_NOTARY_* not fully set -> signing only (no notarization) for ${binary}"
  quill sign "${binary}"
fi
