# Gotchas: Iteration Close

## Binary Resolution

- **Payout: missing dev binary** — `/tmp/dot-agents-dev` doesn't exist or is stale (binary from a prior session, not current `../dot-agents` HEAD). Always check `ls -la /tmp/dot-agents-dev` and compare mtime to `../dot-agents` last commit. Rebuild if uncertain.
- **dot-agents: `go run` is slow** — prefer `go run ./cmd/dot-agents workflow ...` only if `da` isn't in PATH. The installed binary is faster and avoids accidental compilation errors masking the verify step.
- **Wrong working directory** — `workflow verify record` and `workflow checkpoint` read `.agentsrc.json` for project context. Must run from the repo root, not a subdirectory.
- **`make build-prod` is not an every-iteration step** — Use it after a major section or feature is stable. Running it in the middle of rapid iteration adds noise and can hide whether you are actually testing source vs binary behavior.
- **Fresh build, stale PATH target** — `make build-prod` updates `./bin/da`, but your shell may still resolve `da` to another location. Check `command -v da` after the build before assuming the new binary is active.

## Verify Record

- **Running verify record when tests failed** — Still run it with `--status fail`. Don't skip it. The log must capture failure states too; skipping produces a misleading "all clean" history.
- **Generic summary** — `"go test passed"` is noise. The summary should name the packages tested and test count: `"go test ./internal/platform/...: 4 new tests, 58 total pass"`. This is what makes the log useful for audit.
- **Partial tiers** — If acceptance or integration tests weren't run, use `--status partial`, not `pass`. Overstating coverage is the primary way checkpoint history becomes untrustworthy.

## Checkpoint Message

- **Backward-looking message** — `"Added phase-6 tests"` is less useful than `"Phase 6 status/explain registry coverage complete — sharedTargetRegistryPlanLines delegates to DryRunSharedTargetPlanLines"`. The checkpoint becomes the `workflow status` "Next action" text; make it orient future sessions.
- **Stale `workflow status` after writing checkpoint** — If `workflow status` still shows the old stale text immediately after `workflow checkpoint`, the checkpoint may have written to a different path. Check `da workflow log` to confirm the new entry appears.
- **`workflow status` next action shows literal plan Status header** — The "Next action" field in `workflow status` extracts the first `Status:` line from the active plan file, not a semantic next action. Treat it as a freshness indicator, not task direction. Use `workflow orient` + `workflow tasks` for actual task selection.

## Advance Task

- **Delegated worker advancing the parent task** — If fanout created `.agents/active/delegation/<task-id>.yaml`, you are the worker: use **`workflow merge-back`**, not `workflow advance`. The parent runs `advance` and `workflow delegation closeout` after accepting your merge-back. Advancing from the worker breaks the orchestration model in `LOOP_ORCHESTRATION_SPEC.md`.
- **Advancing a task with incomplete subtasks** — If a plan task has sub-checklist items still open in markdown, advancing YAML to `completed` creates drift. Only advance when the markdown plan and YAML are in sync.
- **Wrong plan-id or task-id** — Use `da workflow plan` to list plan IDs and `da workflow tasks <plan-id>` to list exact task IDs before running `advance`. Typos silently fail or create a new task.
- **Advancing before committing** — `workflow advance` should run after the commit is on the branch, not before. If tests pass but the commit fails, the YAML would show completed while code is uncommitted.

## Loop-State Log Entry

- **Wrong commit hash in iteration log** — When writing the `commit:` field in the loop-state iteration entry, always use `git log -1 --format="%h"` (short hash of HEAD after the iteration commit). Do not use a hash from a prior iteration, from `git log` output that predates the current commit, or from memory. Run `git log -1 --format="%h"` immediately after `git commit` and paste the result directly. If the iteration produced multiple commits, use the final one.

## Proposal Creation

- **`modify` action replaces the entire file** — When writing a `skill`/`modify` or `rule`/`modify` proposal, the `content:` field must contain the full updated file. Do not write just the new gotcha — read the current file first and include all existing content plus the new addition.
- **`workflow prefs set-shared` only works for valid preference keys** — Do not use it to queue gotchas or rule changes. Use the proposal/review loop instead: write the proposal artifact to `~/.agents/proposals/<id>.yaml` (or use `propose.sh`), then inspect/apply it with `da review`.
- **`da review` returns no proposals when the dir is empty or missing** — Run `mkdir -p ~/.agents/proposals` if the proposals directory hasn't been created yet.
