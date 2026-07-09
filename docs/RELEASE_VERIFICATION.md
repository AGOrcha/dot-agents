---
title: Release Verification
description: How to verify a dot-agents release with Cosign keyless signing before installing it.
sidebar:
  order: 7
---

# Release Verification

Every `dot-agents` release is signed with [Cosign](https://github.com/sigstore/cosign)
using keyless signing via [Sigstore](https://www.sigstore.dev/) and GitHub OIDC.
There are no long-lived keys: the signature is bound to the GitHub Actions
workflow that produced the release and the signing event is logged to the
public [Rekor](https://rekor.sigstore.dev/) transparency log.

This document explains how to verify a release before installing it.

## Before cutting a release (maintainers)

> This section is for maintainers **cutting** a release. If you are a user
> verifying a release you downloaded, skip to [Why verify?](#why-verify).

**REQUIRED pre-cut step — run `release-docs-refresh` BEFORE bumping `VERSION`
or finalizing the `CHANGELOG`.** This applies to **every** release cut, patch
and minor alike. It is not optional and it is not added by hand per cut: it is
wired in as a structural predecessor of the version bump (see below), so every
cut auto-includes it.

1. Run the `release-docs-refresh` skill (`.agents/skills/release-docs-refresh/`)
   at release-ready state. It reconciles the scope, spec, and user-facing docs
   (`README.md`, `docs/**`, `docs/web/**`) **and** `docs/PLATFORM_DIRS_DOCS.md`
   against the code, fixing genuinely stale docs in place.
2. Where the **code** breaks a documented contract or promise (not merely
   lagging docs), the skill classifies it and routes a tracked code-fix or
   proposal via the promise-gap analyst (and platform-dir drift via the
   platform-dirs change analyst) — **do not paper over the gap by editing the
   doc.**
3. Only after the docs pass completes: bump `VERSION`, finalize the `CHANGELOG`
   `## [X.Y.Z]` section, and merge. The `VERSION` change on merge to `master`
   fires `auto-release.yml`.

**How this is enforced structurally (so it can never be forgotten):**

- **Patch cuts:** the `release-patch-train` plan's `release-patch` task declares
  `depends_on: [release-docs-refresh]`, and the `release-docs-refresh` task is
  its mandatory predecessor. `release-patch` cannot run until the docs pass
  completes.
- **Minor cuts:** every plan-tail `release-minor` task MUST likewise declare a
  `release-docs-refresh` predecessor in its `depends_on`, per the
  release-gated-plans convention
  (`.agents/proposals/release-gated-plans-convention.md`). New feature-plan
  release tails inherit this from the convention's wiring recipe, so minor cuts
  get the docs pass too — not just the patch train.

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

Replace `VERSION` with the release tag you downloaded, e.g. `0.5.0`.

### 1. Download the release assets

From the [releases page](https://github.com/AGOrcha/dot-agents/releases)
or via `gh`, download:

- The binary archive for your platform, e.g.
  `dot-agents_VERSION_darwin_arm64.tar.gz`
- `checksums.txt`
- `checksums.txt.bundle`

```bash
VERSION=0.5.0
TAG="v${VERSION}"
# GoReleaser names archives with amd64/arm64; normalize uname -m to match.
ARCH=$(uname -m); case "$ARCH" in x86_64) ARCH=amd64;; aarch64) ARCH=arm64;; esac
gh release download "${TAG}" \
  --repo AGOrcha/dot-agents \
  --pattern 'checksums.txt*' \
  --pattern "dot-agents_${VERSION}_$(uname -s | tr A-Z a-z)_${ARCH}.tar.gz"
```

### 2. Verify the Cosign signature on `checksums.txt`

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp "^https://github.com/AGOrcha/dot-agents/.github/workflows/auto-release.yml@refs/heads/master$" \
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
dot-agents_0.5.0_darwin_arm64.tar.gz: OK
```

### 4. Install

Extract the archive and move `da` onto your `PATH`. Verifying after
download is sufficient — `brew install da` and the install
script do not currently invoke cosign automatically.

## Transparency log lookup

Every signing event is logged to the public Rekor transparency log.
You can confirm independently that a given `checksums.txt` signature
was recorded:

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp "^https://github.com/AGOrcha/dot-agents/.github/workflows/auto-release.yml@refs/heads/master$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --rekor-url "https://rekor.sigstore.dev" \
  checksums.txt
```

You can browse Rekor entries for this project at
<https://search.sigstore.dev/?logIndex=>.

## Scope and limitations

This signing approach (Cosign keyless on `checksums.txt`) provides
**supply-chain integrity** — proof that the artifacts came from this
project's official release workflow. It is separate from OS-level
code-signing trust, which now varies by platform:

- macOS: the darwin `da` binary is Apple Developer ID-signed and notarized
  (via quill) as of v0.4.0, so it clears Gatekeeper on first run — both via
  `brew install da` (the Homebrew cask no longer strips the quarantine
  attribute; that stopgap postflight hook was removed) and when the tarball is
  downloaded **manually** from the releases page.
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
