# CLI-runner verifier — dot-agents repo overlay

Repo-local committed layer. Composes **after** `verifiers/verifier.base.md` (the contract) and
`verifiers/cli-runner.md` (the kind: build fresh, smoke floor, scoped invocations). This file adds
**only** the dot-agents command matrix.

## Commands

1. **Build fresh (required, always):** `go build -o ./bin/da ./cmd/da`
   A build failure is a terminal `--status fail` — the tree does not produce a working binary.
2. **Smoke floor (required):** `bash scripts/verify.sh`
   The shared CLI smoke harness (uses `./bin/da`; exercises `--version`/`--help`, `status`, `doctor`,
   `explain`, `workflow`, dry-runs, expected-failure cases). Any failure fails the pass — a change
   must not regress a sibling command.
3. **Task-scoped invocations (required when a touched path maps to a command):** a file under
   `commands/<area>/…` maps to the `da <area> …` command. Run the changed subcommand end-to-end
   against `./bin/da` (positive: exit 0 + a stable substring / `--json` field; negative: non-zero
   exit + clear error, mirroring `scripts/verify.sh`'s `expect_success=false` cases).

`--kind custom`, `--verifier-type cli-runner`. If the build or floor fails, you may skip the scoped
invocations but record `--status fail`.
