# Apple macOS code signing & notarization (Developer ID) — for `da`

How the macOS release binaries (`da` for `darwin/amd64` + `darwin/arm64`) are
**Developer ID-signed and notarized** so Gatekeeper accepts them on download.
This is the macOS counterpart to the Windows path in
[`infra/azure/codesigning`](../../azure/codesigning/README.md): both sign on the
**same ubuntu release runner** with no macOS hardware, gated so releases keep
shipping until the credentials exist.

| Piece | What |
|---|---|
| Tool | [`anchore/quill`](https://github.com/anchore/quill) — signs/notarizes Mach-O binaries from Linux |
| Hook | `scripts/sign-macos.sh` — GoReleaser post-build hook (gated, darwin-only) |
| GoReleaser | `dot-agents-unix` build runs the hook per binary; non-darwin + un-credentialed runs no-op |
| CI | `.github/workflows/auto-release.yml` installs quill (gated on `MACOS_SIGNING_ENABLED`) and passes the `QUILL_*` secrets to GoReleaser |

## 1. One-time Apple setup

1. **Developer ID Application certificate** (Apple Developer portal → Certificates →
   *Developer ID Application*; Team ID **`24MV8888U4`**). Generate a CSR, download
   the cert, and export it **with its private key** from Keychain Access as a
   `.p12` with a password. quill needs the full chain — if the `.p12` lacks the
   Apple intermediates, attach them with `quill p12 attach-chain`.
2. **App Store Connect API key** (for notarization; App Store Connect → Users and
   Access → Integrations → Keys). Download the `.p8` once and note its **Key ID**
   and **Issuer ID**.

## 2. CI configuration (gated — dormant until set)

Set one **variable** (non-secret gate) and five **secrets** on `AGOrcha/dot-agents`:

| Name | Kind | Value |
|---|---|---|
| `MACOS_SIGNING_ENABLED` | variable | `true` (gates the quill install step) |
| `QUILL_SIGN_P12` | secret | the Developer ID `.p12` (base64) |
| `QUILL_SIGN_PASSWORD` | secret | the `.p12` password |
| `QUILL_NOTARY_KEY` | secret | the App Store Connect `.p8` (base64) |
| `QUILL_NOTARY_KEY_ID` | secret | the API key id |
| `QUILL_NOTARY_ISSUER` | secret | the API key issuer id |

`base64 -i cert.p12 | pbcopy` to produce the base64 values. Until `QUILL_SIGN_P12`
is set, `scripts/sign-macos.sh` exits clean and the darwin tarballs ship unsigned
exactly as today. If only the `QUILL_SIGN_*` pair is set (no `QUILL_NOTARY_*`),
binaries are **signed but not notarized** — useful to land signing first.

## 3. Follow-up once notarization is verified

`.goreleaser.yaml`'s Homebrew cask has an **interim** `post` hook that strips the
quarantine xattr because the binaries are not yet notarized. Once a release ships
notarized darwin binaries and `brew install` works without the workaround, **remove
that cask hook** (tracked alongside this setup).

> Note: quill does not staple the notarization ticket to a bare Mach-O binary
> (stapling targets `.app`/`.dmg`/`.pkg`); Gatekeeper verifies notarized bare
> binaries online on first run, which is fine for a CLI distributed via tarball.

## 4. Pitfall: the `.p12` MUST contain the full chain (incl. Apple Root CA)

quill derives the signature's *designated requirement* from the cert chain inside
`QUILL_SIGN_P12`. If the p12 holds only the leaf + the Developer ID intermediate
(no Apple Root CA), quill puts the intermediate at chain-index 0 and emits an
**unsatisfiable** DR — `certificate root[field.1.2.840.113635.100.6.2.6]` — because
that Developer-ID marker OID lives on the *intermediate*, not the anchor. `codesign
--verify --strict` then fails and macOS (Ventura+, hard on macOS 26 / Apple Silicon)
SIGKILLs the binary on first exec. This shipped in **v0.4.1**.

**Fix:** the p12 must carry the full chain — leaf + key + `Developer ID Certification
Authority` (intermediate, `DeveloperIDG2CA`) + `Apple Root CA` — so quill emits the
correct `certificate 1[...6.2.6]`. Build it from PEM components:

    cat DeveloperIDG2CA.pem AppleIncRootCertificate.pem > chain.pem
    openssl pkcs12 -export -out devid-fullchain.p12 \
      -inkey devid_signing.key -in devid_cert.pem -certfile chain.pem -passout pass:<pw>

Verify before shipping: sign a Mach-O with `quill sign --p12 devid-fullchain.p12`, then
`codesign -d -r- <bin>` must show `certificate 1[` (not `certificate root[`) and
`codesign --verify --strict <bin>` must pass. `verify-macos-release.yml`
(release:published → macos-latest) is the CI regression guard.
