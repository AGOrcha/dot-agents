# Gotchas: Release Cut

Common failure points. The first three are the failure modes that turned the 0.4.0
cut into a multi-attempt manual rescue.

## Stale tag on retry (git exit 128)

- The GoReleaser step pushes an annotated `v<version>` tag early, then signs and
  publishes. When a *later* step fails, the pushed tag survives. Naively
  re-triggering the run then aborts at `git tag -a` with **exit 128**
  (`fatal: tag 'v<version>' already exists`) — the early side effect blocks the
  obvious "just run it again" recovery.
- Fix: before any re-drive, delete the stale tag on the remote **and** locally:
  `git push origin :refs/tags/v<version>` then `git tag -d v<version>`. Delete a
  partial GitHub release with `gh release delete` if one was created. THEN
  `workflow_dispatch` re-drive. Never hand-patch the half-shipped state.

## dotnet `sign` kernel32 Linux regression (do NOT reintroduce)

- The original Windows-signing path used the dotnet `sign` global tool. On the
  ubuntu runner it crashed with a `kernel32.dll` `DllNotFoundException`
  (dotnet/sign#711) — a native PE-interop fault that reads like an environment
  problem but is really a tool that simply cannot run on Linux. It broke 0.4.0.
- The fix replaced it with **jsign** (a JVM tool, Linux-native, `--storetype
  TRUSTEDSIGNING`). Do NOT reintroduce a dotnet `sign` step. The `lint-workflows`
  pin-check in `test.yml` fails the build if `auto-release.yml` reintroduces it —
  treat that failure as "you re-added the kernel32 regression," not a flaky lint.

## DLSequence cast failure (timestamp path)

- An unpinned signing/timestamp library resolved to a fresh version whose
  timestamp path threw a `DLSequence` cast failure (an ASN.1/Bouncy-Castle
  internal mismatch). Like the kernel32 crash, the project did not change — the
  toolchain moved underneath it.
- This is a **toolchain fault**, not a project regression: do not re-drive blindly.
  Pin (or re-pin) the offending tool to a known-good `*_VERSION` + `*_SHA256` in
  its own PR, then cut. See `references/known-failures.md`.

## Cosign / timestamp transient flake

- Keyless Cosign signing mints an OIDC token and talks to Fulcio/Rekor; the
  Sonar scanner and the jsign Trusted Signing token acquisition also hit external
  services. Any of these can throw a transient network/CDN/CE ("certificate
  enrollment") or timestamp error that is genuinely retry-able.
- A single transient signing/timestamp error with no version change is the one
  clean case for the **clean-tag + re-drive once** recovery. If the *same*
  signature recurs on the re-drive, it is no longer a flake — escalate to a pin
  review.

## Don't re-drive a real regression

- `workflow_dispatch` re-drive is for **infra-class** failures only. A Go
  test/build failure, a `Verify CLI version matches` mismatch, or a smoke-test
  failure is a real regression — re-driving just burns runs. Classify first
  (`references/known-failures.md`), fix the code, then cut.

## Gating tools are pinned for a reason

- All `uses:` actions in `auto-release.yml` are pinned to commit SHAs and the
  signing-tool installs carry `*_VERSION` + `*_SHA256`. The CI pin-check targets
  the curl|install signing-tool steps, not the SHA-pinned `uses:` actions — keep
  it that way. Bumping a pin is a deliberate, reviewable PR (version + sha256
  together), never a mid-cut edit.
