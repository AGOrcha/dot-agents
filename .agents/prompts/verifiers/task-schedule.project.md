# Task-schedule verifier — dot-agents repo overlay

Repo-local committed layer. Composes **after** `verifiers/verifier.base.md` (the contract) and
`verifiers/task-schedule.md` (the kind: deps resolve, acyclic, partition consistent). This file adds
**only** the dot-agents dependency format and scheduler command.

## Commands

1. **Dependency resolution:** every `depends_on` id resolves. Note the format — a dep string is
   **cross-plan** when it contains `/` (`<plan-id>/<task-id>`); a bare id is local to the same plan.
   A dep pointing at a non-existent task is a dangling dependency → `fail`.
2. **Acyclicity:** the `depends_on` graph has no cycles; report the first cycle's task ids.
3. **Eligibility cross-check:** `da workflow eligible --json` — the ready/blocked partition must be
   consistent with the graph and report no conflicts; cite the offending task id otherwise.

`--kind test`, `--verifier-type task-schedule`. If a required check fails, you may skip the later ones
but record `--status fail` naming the first offending task/dep.
