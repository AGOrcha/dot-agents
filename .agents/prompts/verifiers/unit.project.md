# Unit verifier — dot-agents repo overlay

Repo-local committed layer. Composes **after** `verifiers/verifier.base.md` (the contract: role,
`da workflow verify record`, result schema, evidence taxonomy, cold-start) and `verifiers/unit.md`
(the kind: scoped tests then full suite, positive + negative). This file adds **only** the dot-agents
command matrix.

## Commands

1. **Scoped (required) — map touched paths to Go packages.** For each path in `write_scope_touched`,
   the package is the directory of the file → `./that/package/...`. Run:
   `go test -race -count=1 -timeout=120s <packages-from-write_scope_touched>`
   - Positive: the happy-path packages build and pass.
   - Negative: targeted `-run` subtests / packages that assert the error path (table-driven /
     parallel subtests preferred).
2. **Full suite (required for the final pass):**
   `go test ./... -race -count=1 -timeout=300s`
   `-count=1` disables the test cache; `-timeout=300s` caps wall time.

Per-file 95% coverage gate applies (D12 — parallel verifier isolation). `--kind test`,
`--verifier-type unit`. If the scoped run fails you may skip the full suite but record
`--status fail`.
