# Eval Checklist: Release Cut

Pass/fail gate. A release is high-stakes — every item must pass before reporting
the cut as done. If any item fails, the release is NOT done.

## Preflight (before pushing the tag)

1. [ ] `release-docs-refresh` was run first / docs already reconciled; only the
       VERSION (and CHANGELOG) bump is pending.
2. [ ] The `lint-workflows` pin-check (test.yml) is GREEN on the release ref —
       every signing tool pinned (`*_VERSION` + `*_SHA256`), no `--prerelease`/`@latest`
       signing install, no dotnet `sign`.
3. [ ] `VERSION` matches the intended `<x.y.z>` and the CLI `--version` will match it.

## Run outcome

4. [ ] An `auto-release.yml` run was triggered for this version (push or `workflow_dispatch`).
5. [ ] The run is GREEN, and specifically the **"Verify Cosign signature on
       checksums.txt"** step passed (not merely a created GitHub release).
6. [ ] The GitHub release `v<version>` exists with the expected artifacts.

## Recovery hygiene (if a re-drive happened)

7. [ ] No stale `v<version>` tag was left behind from a failed attempt
       (`git ls-remote --tags origin v<version>` resolves to the final cut, not an orphan).
8. [ ] No partial/duplicate GitHub release remains.
9. [ ] Any infra-class failure was classified (`references/known-failures.md`) and a
       re-drive was attempted **at most once** before escalating.

## Report

10. [ ] Reported: release URL, the Cosign verify-step status, and the failure
        classification if any step went red.
