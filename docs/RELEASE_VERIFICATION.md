# Release Verification

Every `dot-agents` release is signed with [Cosign](https://github.com/sigstore/cosign)
using keyless signing via [Sigstore](https://www.sigstore.dev/) and GitHub OIDC.
There are no long-lived keys: the signature is bound to the GitHub Actions
workflow that produced the release and the signing event is logged to the
public [Rekor](https://rekor.sigstore.dev/) transparency log.

This document explains how to verify a release before installing it.

## Why verify?

Verifying the release lets you confirm that:

1. The `checksums.txt` file you downloaded was produced by this repository's
   official release workflow (not by an attacker who tampered with the GitHub
   release page or a mirror).
2. The binary you are about to run matches the checksum recorded by that
   trusted `checksums.txt`.

If either step fails, do not install the binary.

## Prerequisites

Install `cosign` (one-time):

```bash
# macOS
brew install cosign

# Linux (see https://docs.sigstore.dev/cosign/installation for other options)
curl -O -L "https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64"
sudo mv cosign-linux-amd64 /usr/local/bin/cosign
sudo chmod +x /usr/local/bin/cosign
```

You will also need `sha256sum` (preinstalled on Linux; on macOS use
`shasum -a 256` or `brew install coreutils`).

## Step-by-step verification

Replace `VERSION` with the release tag you downloaded, e.g. `0.3.3`.

### 1. Download the release assets

From the [releases page](https://github.com/NikashPrakash/dot-agents/releases)
or via `gh`, download:

- The binary archive for your platform, e.g.
  `dot-agents_VERSION_darwin_arm64.tar.gz`
- `checksums.txt`
- `checksums.txt.bundle`

```bash
VERSION=0.3.3
TAG="v${VERSION}"
gh release download "${TAG}" \
  --repo NikashPrakash/dot-agents \
  --pattern 'checksums.txt*' \
  --pattern "dot-agents_${VERSION}_$(uname -s | tr A-Z a-z)_$(uname -m).tar.gz"
```

### 2. Verify the Cosign signature on `checksums.txt`

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp "^https://github.com/NikashPrakash/dot-agents/.github/workflows/auto-release.yml@refs/heads/master$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

Expected output:

```
Verified OK
```

If you see `Verified OK`, the `checksums.txt` file was produced by the
official release workflow at the expected ref. If verification fails,
**stop** — the release artifacts may be tampered with.

#### What the flags mean

- `--certificate-identity-regexp` — the signing certificate must have been
  issued for an OIDC subject matching this regex. The subject is set by
  GitHub to the workflow file plus the ref it ran on; pinning to
  `auto-release.yml@refs/heads/master` ensures only releases built from
  `master` are accepted.
- `--certificate-oidc-issuer` — the OIDC issuer must be GitHub Actions.
  This prevents acceptance of certificates minted by a different OIDC
  provider (e.g. a malicious fork's CI).

### 3. Verify the binary checksum

Once `checksums.txt` is trusted, use it to verify the binary you downloaded:

```bash
# Linux: keeps just the line for the file you have on disk
sha256sum --ignore-missing -c checksums.txt

# macOS (with coreutils)
gsha256sum --ignore-missing -c checksums.txt

# macOS (without coreutils) — manual one-liner
shasum -a 256 dot-agents_${VERSION}_darwin_arm64.tar.gz
# compare the output against the corresponding line in checksums.txt
```

Expected output (Linux/coreutils form):

```
dot-agents_0.3.3_darwin_arm64.tar.gz: OK
```

### 4. Install

Extract the archive and move `da` onto your `PATH`. Verifying after
download is sufficient — `brew install dot-agents` and the install
script do not currently invoke cosign automatically.

## Transparency log lookup

Every signing event is logged to the public Rekor transparency log.
You can confirm independently that a given `checksums.txt` signature
was recorded:

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp "^https://github.com/NikashPrakash/dot-agents/.github/workflows/auto-release.yml@refs/heads/master$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --rekor-url "https://rekor.sigstore.dev" \
  checksums.txt
```

You can browse Rekor entries for this project at
<https://search.sigstore.dev/?logIndex=>.

## Scope and limitations

This signing approach (Cosign keyless on `checksums.txt`) provides
**supply-chain integrity** — proof that the artifacts came from this
project's official release workflow. It does **not** provide OS-level
code-signing trust:

- macOS Gatekeeper still treats the binary as unsigned. The Homebrew
  **cask** strips the quarantine attribute on install (an interim workaround) so
  `brew install --cask` works today, but a binary downloaded **manually** from
  the releases page still needs the right-click → Open workaround on first
  launch until Apple Developer ID notarization lands.
- Windows SmartScreen will warn about an unrecognized publisher.
- Linux package managers will not pick up the cosign signature
  automatically.

OS-native signing (Apple Developer ID + notarization, Windows EV cert,
Linux distro packaging) requires paid certificates and is a separate
decision tracked outside this verification recipe. If you need
seamless Gatekeeper / SmartScreen trust, open an issue.

## Troubleshooting

**`Error: no matching signatures`** — the wrong `checksums.txt.bundle`
file was used, or the certificate identity does not match. Double-check
that you downloaded the `.bundle` file from the same release as the
`checksums.txt`.

**`Error: fetching certificate from Fulcio`** — transient network
failure or Fulcio outage. Retry. Verification is fully offline once
you have the `checksums.txt.bundle` and `checksums.txt`.

**`sha256sum: WARNING: N lines are improperly formatted`** — this is
expected when using `--ignore-missing`; only the line(s) matching the
file(s) you downloaded are checked.
