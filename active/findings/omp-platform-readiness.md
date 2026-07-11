# omp-platform findings — readiness stage (D14 managed-gitignore-autofill)

da↔omp friction observed during the readiness gate. Terse: command + what happened.

- **Coordination is manual git-ref plumbing, not a da primitive.** The whole swarm hangs off a
  parallel `refs/agents/state` lineage + hand-written `COORD/*.md` signal files with a line-1 verdict
  token. da has no `da swarm signal`/`da workflow signal` seam to emit/await these; every stage
  re-implements grep-line-1 by hand. omp would want a first-class signal/gate CLI.
- **`da kg impact <file> --json` works under omp/embedded-shell (0.4.2), good.** Both target files
  resolved (`graph_state: ready`). But output truncates impacted nodes to 50 and `impacted_files`
  is the generic package closure (`cmd/da/main.go`, `commands/add.go`, test scratch under
  `.agents/history/*` + `.agents/sandbox/*`) — no `--scope`/`--exclude-nonsource` flag to drop
  non-production fan-out, so a human must eyeball relevance. Minor.
- **Building `da` from source is mandatory per-slice (0.4.2 installed is stale) but the readiness
  gate can't build yet** — the slice worktree is what carries `bin/da`, and readiness only *creates*
  it. So readiness is forced to use the stale installed `da` for `kg impact`. Worked here, but it's
  an ordering wart: KG readback happens on the stale binary, impl/verify on the fresh one.
- **`.venv` must be hand-symlinked into each new worktree** (`ln -s "$(pwd)/.venv" <wt>/.venv`)
  because `git worktree add` doesn't carry gitignored/untracked roots. da has no
  `da worktree add`/refresh wrapper that provisions the venv, so the CRG-backed `kg` tooling would
  silently break in a fresh worktree without this manual step.
