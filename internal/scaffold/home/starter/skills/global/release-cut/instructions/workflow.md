# Workflow: Release Cut

The flow is: **preflight pin-check → cut → monitor → (on infra failure) clean stale tag + re-drive → classify**. Each step gates the next; do not skip the preflight.

## 0. Predecessor check

`release-cut` runs **after** the `release-docs-refresh` skill. Confirm docs/scope/spec
were already reconciled and the only pending change is the VERSION (and CHANGELOG)
bump. If docs still drift from the code, stop and run `release-docs-refresh` first —
do not paper over a contract gap inside a release cut.

## 1. Preflight: pin-check the signing toolchain

The 0.4.0 cut burned multiple runs because a signing tool resolved to a fresh
version whose native layer crashed on Linux. **Before pushing the tag**, confirm
`auto-release.yml` still pins every build/sign/timestamp tool to an exact version
**plus** a `sha256` and does NOT reintroduce the dotnet `sign` tool:

- The repo CI already enforces this — the `lint-workflows` job in `.github/workflows/test.yml`
  runs a pin-check step that fails if `auto-release.yml` installs `jsign`/`quill`/`cosign`
  unpinned, uses a `--prerelease`/`@latest` signing-tool install, or reintroduces dotnet `sign`.
  Confirm that step is green on `master` (or the release branch) before cutting.
- If you cannot rely on CI, eyeball `auto-release.yml`: every signing/timestamp tool
  install must carry both a pinned `*_VERSION` and a `*_SHA256` env var, and the
  checksum must be verified (`sha256sum -c -`). See `references/auto-release-stepmap.md`
  for the exact steps to inspect.
- A pin bump is its own reviewable PR (`*_VERSION` + `*_SHA256` bumped together), never
  a silent mid-cut surprise. If the pin needs to move, stop and land that PR first.

Do not proceed to the cut if the pin-check is red.

## 2. Cut: push the version bump / tag

`auto-release.yml` triggers on a push to `master` that touches `VERSION`. The
GoReleaser step itself creates and pushes the annotated `v<version>` tag — you
do NOT pre-create the tag.

- Confirm `VERSION` (and `CHANGELOG`) carry the intended `<x.y.z>` and the CLI
  `--version` will match (the workflow's "Verify CLI version matches" step is a hard gate).
- Merge the version-bump PR to `master` (or push the bump). The push to `VERSION`
  starts the run automatically.
- If you are RE-driving an already-bumped version (the bump already landed but the
  run failed), do not re-bump — use the `workflow_dispatch` path in step 4.

## 3. Monitor auto-release.yml

```
gh run list --workflow=auto-release.yml --limit 5
gh run watch <run-id> --exit-status      # or: gh run view <run-id> --log-failed
```

The "Check if release exists" step makes the whole job a no-op if the release
already shipped, so a re-drive is safe. A successful run ends with the
**"Verify Cosign signature on checksums.txt"** step passing — that is the
real done-signal, not merely a created GitHub release.

## 4. On an infra-class failure: clean the stale tag + re-drive

Decide first: **infra-class** (toolchain regression, signing/timestamp flake,
transient CDN/network) vs. **real regression** (Go test/build failure, version
mismatch). For real regressions, fix the code — do NOT re-drive. Use
`references/known-failures.md` to classify.

For an infra-class failure, the recovery is mechanical (the lesson's rule 3):

1. **Delete the stale tag** left by the half-finished cut. A re-run that hits an
   existing tag aborts with `git tag` exit 128 (`tag already exists`):
   ```
   git push origin :refs/tags/v<version>     # delete remote tag
   git tag -d v<version>                      # delete local tag (if present)
   ```
   The release object itself is guarded by "Check if release exists"; if a partial
   GitHub release was created, delete it too: `gh release delete v<version> --yes`.
2. **Re-drive via workflow_dispatch** (no version bump needed — that is exactly
   why the workflow declares `workflow_dispatch`):
   ```
   gh workflow run auto-release.yml --ref master
   ```
3. Re-monitor (step 3). Re-drive **once**; if it fails again on the same infra
   signature, escalate — a second identical failure means the pin/toolchain itself
   needs a fix PR, not another re-drive.

## 5. Classify the run outcome

Whatever the result, record the class so the next cut routes instantly instead of
re-diagnosing:

- **Green** → run the `eval/checklist.md` gate, then report the release URL + the
  Cosign verify step status.
- **Infra-class, recovered by re-drive** → note the signature (e.g. "jsign tsmode
  timestamp flake, re-drove once, green") so the pattern is captured.
- **Toolchain regression (kernel32 / DLSequence)** → this is a *pin* fault: open a
  pin-bump PR per `references/known-failures.md`; do not keep re-driving.
- **Real regression** → hand back to the code owner; release stays uncut.
