# omp-platform findings — verify (cli-runner) stage (D14/R8)

Isolated CLI dogfood of the slice binary (`.agents/worktrees/d14/bin/da`) run under
omp's embedded shell. da↔omp / da-CLI friction hit while verifying the managed
`.gitignore` auto-fill wiring:

- **Signal/coordination paths are not discoverable from cwd.** The brief named
  `CONVENTIONS.md` and `active/coordination/verify-unit.md` as if cwd-relative,
  but they live under `.agents/worktrees/_state/{swarm-run/design,active/coordination}`
  (the `refs/agents/state` worktree). Had to `glob` for them. A da-native
  `da workflow coord path <stage>` (or an env var to STATE/COORD) would remove the
  manual hunt — the file-only coordination protocol has no CLI to resolve its own
  paths.

- **`resolve-prompt` returns unresolved base/kind layers as `exists:false` but
  still `matched:true`.** `da --json workflow resolve-prompt --kind verifier
  --slug cli-runner` lists `verifiers/verifier.base.md` and `verifiers/cli-runner.md`
  as `scope:unresolved, exists:false`, with only the repo-local
  `cli-runner.project.md` resolved. Fine here (the project overlay is
  self-contained), but a consumer that expects the base contract layer to exist
  gets no hard error — the CLI leaves "is a partial resolution acceptable?" to the
  caller. An `--require-complete` flag (nonzero exit when any layer is missing)
  would make gating scriptable.

- **`da refresh` platform enablement is driven by PATH probing, not the project
  manifest.** Even with a fully isolated `HOME`/`AGENTS_HOME`, `IsInstalled()`
  falls back to `exec.LookPath`, so cursor/claude/codex/copilot all detected as
  installed and `DetectAndEnableNewPlatforms` force-enabled them (overriding any
  `enabled:false` I set). There is no CLI to pin the enabled set for a hermetic
  run (no `da platform enable/disable`, and `.agentsrc.json` carries no platform
  toggle that feeds `config.json`'s `agents` map). A hermetic dogfood cannot get a
  "copilot-only" refresh without removing the other CLIs from PATH — worth a
  `da refresh --platforms copilot` / `AGENTS_PLATFORMS` override for reproducible
  per-platform verification.

- **`.agentsrc.lock` churns on every `da refresh` (timestamp-only).** Two
  identical no-op refreshes rewrite the committed lock's `refreshedAt` +
  per-unit `fetched_at`/`last_checked_at` (structural digest unchanged). The D14
  managed `.gitignore` is byte-stable, but the *committed contract* lock is not —
  every refresh dirties the git tree with a timestamp diff, which will surface as
  spurious "modified .agentsrc.lock" noise in any consuming repo that commits the
  lock and runs refresh routinely. Consider freezing volatile timestamps on a
  no-op re-resolve so the committed lock stays byte-stable when nothing changed.

- **Positive:** the D14 wiring itself dogfooded cleanly end-to-end via the CLI —
  `da refresh` generated copilot's dynamic `.github/hooks/*.json` fanout, the
  managed block ignored it VIA the block line (`git check-ignore -v` →
  `.gitignore:14:.github/hooks/*.json`), the committed `.agentsrc.json`/
  `.agentsrc.lock` stayed out of the block (tracked), the user's own ignore line
  survived outside the markers, and the block was byte-stable across 3 refreshes.
  No ad-hoc root rules needed (#381 `.github/hooks/*.json` rule retirement holds).
