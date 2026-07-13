# Workflow: Release Cut

The flow is: **preflight pin-check → cut → monitor → (on infra failure) clean stale
tag + re-drive → classify**. Each step gates the next; do not skip the preflight.

Throughout, the project-specific values — the **release workflow file**, the
**signing/timestamp toolchain**, the **CI pin-check job**, and the **done-signal
step** — are resolved from the project overlay (`da config relevance` + the project's
release docs / CI config). The procedure below is generic; substitute the project's
concrete names where placeholders appear.

## 0. Predecessor check

`release-cut` runs **after** the `release-docs-refresh` skill. Confirm docs/scope/spec
were already reconciled and the only pending change is the VERSION (and CHANGELOG)
bump. If docs still drift from the code, stop and run `release-docs-refresh` first —
do not paper over a contract gap inside a release cut.

## 1. Preflight: pin-check the signing toolchain

The recurring failure mode this guards against: a signing tool resolves to a fresh
version whose native layer crashes on the runner, burning multiple release runs.
**Before pushing the tag**, confirm the project's release workflow still pins every
build/sign/timestamp tool to an exact version **plus** a checksum, and has not
reintroduced any known-broken signing tool:

- **If the project's CI enforces this** (a pin-check / lint-workflows step that fails
  when a signing tool is installed unpinned, via a `--prerelease`/`@latest` install,
  or reintroduces a known-broken tool): confirm that step is green on the release ref
  before cutting.
- **If you cannot rely on CI**, eyeball the release workflow: every signing/timestamp
  tool install must carry both a pinned version and a checksum, and the checksum must
  be verified. See the project step-map reference for the exact steps to inspect.
- A pin bump is its own reviewable PR (version + checksum bumped together), never a
  silent mid-cut surprise. If the pin needs to move, stop and land that PR first.

Do not proceed to the cut if the pin-check is red.

## 2. Cut: push the version bump / tag

The release workflow triggers on the project's release event — typically a push to
the default branch that touches `VERSION`. Many release pipelines create and push the
annotated `v<version>` tag **inside** the workflow (e.g. a GoReleaser step), so you do
NOT pre-create the tag — confirm the project's trigger contract before assuming.

- Confirm `VERSION` (and `CHANGELOG`) carry the intended `<x.y.z>` and that the
  built artifact's reported version will match (release workflows usually have a hard
  "verify version matches" gate).
- Merge the version-bump PR (or push the bump). The version-touching push starts the
  run automatically.
- If you are RE-driving an already-bumped version (the bump landed but the run
  failed), do not re-bump — use the re-drive path in step 4.

## 3. Monitor the release workflow

```
gh run list --workflow=<release-workflow> --limit 5
gh run watch <run-id> --exit-status      # or: gh run view <run-id> --log-failed
```

A well-formed release workflow has an idempotency guard (a "release already exists"
check) that no-ops the run if the release already shipped, which is what makes a
re-drive safe. The real done-signal is the project's **signature-verification step**
passing (e.g. a `cosign verify-blob` of the published checksums) — not merely a
created GitHub release. Resolve which step is the done-signal from the project's step
map.

## 4. On an infra-class failure: clean the stale tag + re-drive

Decide first: **infra-class** (toolchain regression, signing/timestamp flake,
transient CDN/network) vs. **real regression** (test/build failure, version
mismatch). For real regressions, fix the code — do NOT re-drive. Use the project's
known-failure classifier to map the signature.

For an infra-class failure, the recovery is mechanical (the lesson's rule 3):

1. **Delete the stale tag** left by the half-finished cut. A re-run that hits an
   existing tag aborts at the tag-create step (`git tag` exit 128,
   `tag already exists`):
   ```
   git push <active-line> :refs/tags/v<version>     # delete remote tag
   git tag -d v<version>                              # delete local tag (if present)
   ```
   The release object itself is guarded by the "release exists" check; if a partial
   GitHub release was created, delete it too: `gh release delete v<version> --yes`.
2. **Re-drive** via the project's re-drive path — most release workflows declare
   `workflow_dispatch` for exactly this (no version bump needed):
   ```
   gh workflow run <release-workflow> --ref <default-branch>
   ```
3. Re-monitor (step 3). Re-drive **once**; if it fails again on the same infra
   signature, escalate — a second identical failure means the pin/toolchain itself
   needs a fix PR, not another re-drive.

## 5. Classify the run outcome

Whatever the result, record the class so the next cut routes instantly instead of
re-diagnosing:

- **Green** → run the `eval/checklist.md` gate, then report the release URL + the
  signature-verification step status.
- **Infra-class, recovered by re-drive** → note the signature (e.g. "timestamp-authority
  flake, re-drove once, green") so the pattern is captured.
- **Toolchain regression** (a known-broken-tool signature from the project's
  classifier) → this is a *pin* fault: open a pin-bump PR per the project's
  known-failures reference; do not keep re-driving.
- **Real regression** → hand back to the code owner; release stays uncut.
