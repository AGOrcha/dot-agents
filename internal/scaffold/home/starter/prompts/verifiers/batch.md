# Batch verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **fixture / golden / multi-record
verification**: prove file-backed or multi-record batches produce the expected output. `--kind test`
or `custom`, `--verifier-type batch`. Reserve `unit` for in-process test runs; use this kind when the
primary proof is **expected-vs-actual artifacts at scale** (golden dirs, CSV/JSON fixtures, snapshot
or CLI-diff output, schema validation over fixture trees, job runners).

## What to run

1. **Scoped first.** Run only the jobs / tags / fixture subsets that cover `write_scope_touched`.
   - Positive: valid rows/files, conformant schemas, successful exits with matching golden output or
     checksums.
   - Negative: corrupt rows, missing required fields, version skew, intentional bad fixtures —
     capture expected-vs-actual diffs.
2. **Broader tiers (when in scope):** the full fixture tree / matrix row after scoped green; bounded
   volume/perf with budgets (`partial`/`fail` on regression without an approved baseline change).

Persist machine-readable diffs and reference them from `artifact_paths`. When a baseline updates
intentionally, say so in the summary — never silently widen tolerances in the verifier role.

## Record

`da workflow verify record --kind <test|custom> --verifier-type batch` — status, command lines, and
`artifact_paths` (golden dirs, diff outputs, batch logs). Fixture roots, tool binaries, and diff
tolerances come from the repo-local override.
