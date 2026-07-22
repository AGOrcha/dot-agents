# Gotchas: Iteration Close

## Stop-Gate Sentinel

- **Skipping the sentinel-write step** — `da workflow hook-sentinel write iteration-close` is a required first action (after task identity is known), not optional advice. If it is skipped, the Stop/SubagentStop gate finds no sentinel and exits 0 silently: the whole closeout contract goes unenforced for that turn. Write it before any verify/checkpoint/advance/merge-back action.
- **Declaring artifacts you will not produce** — only pass `--expect` for artifacts this iteration actually owns. Listing a merge-back on a direct closeout, or a `review-decision.yaml` on a review-skipped non-code iteration, makes the gate hard-remediate on a missing file that was never contracted. Match the sentinel to the path you are actually on.
- **Changing the governed closeout sequence without updating the gate** — if you alter which workflow actions iteration-close performs (e.g. add/remove a checkpoint role, change the merge-back vs advance split), you MUST update the matching `iteration-close-gate` HOOK.yaml/gate.sh contract and its tests in the same change. A skill edit that drifts from the gate silently breaks enforcement. No instruction here permits hard remediation on transcript-only facts without verified trace input.

## Binary Resolution

- **Payout: missing dev binary** — `/tmp/dot-agents-dev` doesn't exist or is stale (binary from a prior session, not current `../dot-agents` HEAD). Always check `ls -la /tmp/dot-agents-dev` and compare mtime to `../dot-agents` last commit. Rebuild if uncertain.
- **dot-agents: `go run` is slow** — prefer `go run ./cmd/da workflow ...` only if `da` isn't in PATH. The installed binary is faster and avoids accidental compilation errors masking the verify step.
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

## Close the iteration (direct) / advance

- **Delegated worker closing/advancing the parent task** — If fanout created `.agents/active/delegation/<task-id>.yaml`, you are the worker: use **`workflow merge-back`**, never `close-task`/`advance`. The parent runs `workflow delegation closeout` after accepting your merge-back; accepted closeout already completes the delegated task. Closing/advancing from the worker breaks the orchestration model in `.agents/workflow/specs/workflow-parallel-orchestration/design.md`.
- **Closing a task with incomplete subtasks** — If a plan task has sub-checklist items still open in markdown, moving the YAML to `completed` (via `close-task` or `advance`) creates drift. Only close when the markdown plan and YAML are in sync.
- **Wrong plan-id or task-id** — Use `da workflow plan` to list plan IDs and `da workflow tasks <plan-id>` to list exact task IDs before running `close-task`/`advance`. Typos silently fail or create a new task.
- **`close-task` commits for you** — on the direct path `close-task` runs checkpoint → score → advance → commit in one step, so you do not separately `advance` then `commit`. Get your code commit on the branch first; `close-task` then commits the workflow state on top. Pass `--no-commit` only when a caller batches the commit elsewhere.

## Loop-State Log Entry

- **Wrong commit hash in iteration log** — When writing the `commit:` field in the loop-state iteration entry, always use `git log -1 --format="%h"` (short hash of HEAD after the iteration commit). Do not use a hash from a prior iteration, from `git log` output that predates the current commit, or from memory. Run `git log -1 --format="%h"` immediately after `git commit` and paste the result directly. If the iteration produced multiple commits, use the final one.

## Proposal Creation

- **`modify` action replaces the entire file** — When writing a `skill`/`modify` or `rule`/`modify` proposal, the `content:` field must contain the full updated file. Do not write just the new gotcha — read the current file first and include all existing content plus the new addition.
- **`workflow prefs set-shared` only works for valid preference keys** — Do not use it to queue gotchas or rule changes. Use the proposal/review loop instead: write the proposal artifact to `~/.agents/proposals/<id>.yaml` (or use `propose.sh`), then inspect/apply it with `da review`.
- **`da review` returns no proposals when the dir is empty or missing** — Run `mkdir -p ~/.agents/proposals` if the proposals directory hasn't been created yet.
