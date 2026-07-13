# Lesson: Pin the release toolchain and make releases retryable

## Pattern

A release pipeline fails for one of two avoidable reasons:

1. **An unpinned/`--prerelease` build or signing tool resolves to a fresh
   version that breaks.** The recent run installed the signing tool without a
   pinned version and got a build whose native layer crashed under Linux (a
   `kernel32`/PE-interop fault in `dotnet sign`, plus a `DLSequence` cast
   failure in the timestamp path). Nothing in the project changed — the
   toolchain moved underneath it.
2. **A re-run is poisoned by leftover state from the failed first attempt.**
   The tag already exists from the half-finished cut, so re-triggering the
   release job aborts on "tag exists" instead of cleanly re-driving the build.

Both turn a one-shot infra hiccup into a multi-attempt manual rescue.

## Root cause

- **Unpinned toolchains are non-reproducible.** `--prerelease` and bare
  "latest" installs mean the bytes that sign/build your artifact are chosen at
  job time, not by you. A regression in *their* release becomes a failure in
  *yours*, with a misleading stack (a native `kernel32` crash reads like an env
  problem, not a version problem).
- **Releases are treated as one-shot, not idempotent.** Tag creation, artifact
  build, signing, and publish are separate steps; when a late step fails, the
  early side effects (the pushed tag) survive and block the obvious "just run
  it again" recovery.

## Rule

1. **Pin every build/signing/timestamp tool to an exact version** in the release
   workflow. Never `--prerelease` and never an unpinned "latest" for anything on
   the signing/publish path. Bump the pin deliberately, in its own PR, so a
   toolchain regression is a reviewable change and not a silent surprise mid-cut.
2. **Add a CI guard that fails the job if a release tool resolves to an
   unpinned version** (the "fail-if-toolchain-installs-unpinned" check). Catch
   the drift before it signs anything.
3. **Make the release idempotent / re-drivable.** Recovery from an infra
   failure must be: delete the stale tag, then re-trigger via
   `workflow_dispatch` — not a manual surgery. Do not let a pre-existing tag
   abort the re-run; the re-drive should reconcile or replace stale state.
4. **Classify the known failure signatures** so a human (or the monitor) routes
   instantly instead of re-diagnosing: the `kernel32` native crash and the
   `DLSequence` cast failure are *toolchain* faults (re-pin / re-drive), not
   project regressions.

## How to apply

- At cut time, preflight-check that the installed signing/build tool matches the
  pin **before** building anything.
- On an infra-class failure (not a real test/build regression): clean the stale
  tag, then `workflow_dispatch` re-drive — do not hand-patch.
- Keep the pin and the CI unpinned-guard together so they can't drift apart.
- The multi-step "preflight pin-check → cut → monitor → classify → clean-tag +
  re-drive" judgment is exactly the `release-cut-monitor-retry` flow; this
  lesson is the *why* behind it.

## Related

- [[gates-must-be-locally-reproducible]] — same family: a release/CI step that
  can't be reproduced or cleanly re-run is a tax; fix the source.
- [[sonarcloud-gate-mechanics]] — a retryable infra flake ("Task finished
  abnormally") is the analogue on the analysis gate; re-drive, don't bypass.
