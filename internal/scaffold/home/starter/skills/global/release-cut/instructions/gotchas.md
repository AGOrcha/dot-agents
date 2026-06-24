# Gotchas: Release Cut

Common failure points, stated as generic failure CLASSES. The project's concrete
signatures (the exact tools, error strings, and the known-broken signing tool to
avoid) live in the project's known-failures reference — map each class below to the
project's signature before recovering.

## Stale tag on retry (git exit 128)

- A release workflow that pushes the annotated `v<version>` tag early, then signs and
  publishes, leaves the tag behind when a *later* step fails. Naively re-triggering
  the run then aborts at the tag-create step with **exit 128**
  (`fatal: tag 'v<version>' already exists`) — the early side effect blocks the
  obvious "just run it again" recovery.
- Fix: before any re-drive, delete the stale tag on the remote **and** locally:
  `git push <active-line> :refs/tags/v<version>` then `git tag -d v<version>`. Delete
  a partial GitHub release with `gh release delete` if one was created. THEN re-drive.
  Never hand-patch the half-shipped state.

## A signing tool that cannot run on the release runner (do NOT reintroduce)

- A signing tool can be a *native* tool that simply cannot run on the project's
  release runner (e.g. a platform-PE-interop tool failing with a missing-native-lib
  error on a Linux runner). The failure reads like an environment problem but is
  really a tool that does not belong in this pipeline.
- The fix is to replace it with a runner-native signing tool. Do NOT reintroduce the
  known-broken tool — if the project's CI pin-check fails because the broken tool was
  re-added, treat that failure as "you re-added the regression," not a flaky lint. The
  project's known-failures reference names the specific broken tool and its
  replacement.

## Unpinned-toolchain version regression (timestamp / ASN.1 path)

- An unpinned signing/timestamp library can resolve to a fresh version whose code path
  throws an internal error (e.g. an ASN.1/encoding cast failure in the timestamp
  path). Like the runner-incompatibility case, the project did not change — the
  toolchain moved underneath it.
- This is a **toolchain fault**, not a project regression: do not re-drive blindly.
  Pin (or re-pin) the offending tool to a known-good version + checksum in its own PR,
  then cut.

## Signing / timestamp transient flake

- Keyless signing mints an OIDC token and talks to external transparency/CA services;
  timestamp authorities and SAST scanners also hit external services. Any of these can
  throw a transient network/CDN/cert-enrollment or timestamp error that is genuinely
  retry-able.
- A single transient signing/timestamp error with no version change is the one clean
  case for the **clean-tag + re-drive once** recovery. If the *same* signature recurs
  on the re-drive, it is no longer a flake — escalate to a pin review.

## Don't re-drive a real regression

- The re-drive path is for **infra-class** failures only. A test/build failure, a
  "verify version matches" mismatch, or a smoke-test failure is a real regression —
  re-driving just burns runs. Classify first (the project's known-failures reference),
  fix the code, then cut.

## Gating tools are pinned for a reason

- Release workflows pin their action versions and verify signing-tool installs with a
  version + checksum. The CI pin-check typically targets the curl/install signing-tool
  steps, not the SHA-pinned `uses:` actions — keep it that way. Bumping a pin is a
  deliberate, reviewable PR (version + checksum together), never a mid-cut edit.
