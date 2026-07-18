# Lesson: the new_violations gate counts godre/S3776/S1192 in TEST code (local precheck misses them)

**Date:** 2026-07-18
**Surfaced by:** repeated CI round-trips (#419, #424, #434, #435) on the same gated-pattern class in delegated test code.

## Pattern

The CI `Enforce zero new Sonar issues` step (`scripts/sonar-new-issues-gate.sh`) counts **ALL** new violations — including the `godre` analyzer and TEST files — even though:
- the main SonarCloud **quality gate** profile does NOT gate test-file S1192/S100, and
- the local `analyze_code_snippet` MCP precheck does NOT run the `godre` analyzer,

so test code that reads "clean" locally still **blocks the merge**. The recurring offenders in Go test code:

- **`godre:S8193`** — an init-if whose variable is used ONLY in the condition: `if head := stateRefHead(repo); head == "" { t.Fatal("...") }` → inline it: `if stateRefHead(repo) == "" {`. (If the var is ALSO used in the body, e.g. `t.Fatalf("got %s", head)`, it is NOT flagged — leave it.)
- **`go:S3776`** — a test function with cognitive complexity >15 (table loop + `t.Run` closure + many nested `if err != nil`). Extract the per-case body into a flat helper `assertX(t, ...)`.
- **`go:S1192`** — a string literal (≥10 chars) duplicated ≥3 times, in prod OR test. Extract a `const`. (Literals <10 chars, e.g. `"PLAN.yaml"`, are under threshold.)

`go:S100` (underscore test names, `TestX_Y`) is NOT gated — matches the repo-wide convention; leave it.

## Rule

- Put a **"gated Sonar patterns"** paragraph in every worker/delegation brief that writes Go tests: avoid S8193 (inline single-use init-if vars), keep test funcs' cognitive complexity ≤15 (S3776), extract consts for ≥3× ≥10-char literals (S1192).
- Do NOT rely on the local snippet precheck for `godre`/test-file issues — it can't see them. Assume the CI new-issues gate is stricter.
- Also: the per-file **95% coverage floor** applies to NEW non-allowlisted files (cover error legs, ideally via seams so they're OS-portable), and the merged multi-OS coverage job is where a NEW file's floor is enforced.
