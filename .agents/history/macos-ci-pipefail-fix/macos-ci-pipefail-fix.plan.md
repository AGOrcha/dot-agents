# macOS CI Pipefail Fix Plan

## Goal

Fix the branch's failing macOS GitHub Actions job by removing a fragile smoke-test pattern in `.github/workflows/test.yml`.

## Findings

- PR `#10` fails only on `macos-latest`.
- The failing step is `Test workflow health`.
- The command itself succeeds and prints healthy output.
- The step exits with code `141` because the workflow runs under `bash -e -o pipefail` and uses `./bin/dot-agents workflow health | grep ...`, which can terminate the producer with `SIGPIPE` once `grep` matches.

## Completed Plan

- [x] Inspect the failing workflow run and isolate the exact macOS-only failure mode.
- [x] Replace fragile `command | grep` smoke assertions with capture-and-assert patterns that preserve output and avoid `SIGPIPE`.
- [x] Re-run the relevant local reproduction under `bash -e -o pipefail`.
- [x] Archive implementation results under `.agents/history/`.
