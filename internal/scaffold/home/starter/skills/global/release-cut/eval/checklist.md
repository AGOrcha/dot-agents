# Eval Checklist: Release Cut

Pass/fail gate. A release is high-stakes — every item must pass before reporting
the cut as done. If any item fails, the release is NOT done.

## Preflight (before pushing the tag)

1. [ ] `release-docs-refresh` was run first / docs already reconciled; only the
       VERSION (and CHANGELOG) bump is pending.
2. [ ] The project's signing-toolchain pin-check (the CI lint-workflows / pin-guard
       step) is GREEN on the release ref — every signing tool pinned (version +
       checksum), no `--prerelease`/`@latest` signing install, no known-broken signing
       tool reintroduced.
3. [ ] `VERSION` matches the intended `<x.y.z>` and the built artifact's reported
       version will match it (the workflow's "verify version matches" gate).

## Run outcome

4. [ ] A release-workflow run was triggered for this version (push or re-drive).
5. [ ] The run is GREEN, and specifically the project's **signature-verification
       step** (e.g. a `cosign verify-blob` of the published checksums) passed — not
       merely a created GitHub release.
6. [ ] The GitHub release `v<version>` exists with the expected artifacts.

## Recovery hygiene (if a re-drive happened)

7. [ ] No stale `v<version>` tag was left behind from a failed attempt
       (`git ls-remote --tags <active-line> v<version>` resolves to the final cut, not
       an orphan).
8. [ ] No partial/duplicate GitHub release remains.
9. [ ] Any infra-class failure was classified (`references/known-failures.md`) and a
       re-drive was attempted **at most once** before escalating.

## Report

10. [ ] Reported: release URL, the signature-verification step status, and the failure
        classification if any step went red.
