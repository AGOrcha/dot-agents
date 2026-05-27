# Worker owns the PR readiness loop end-to-end (bridging pattern — superseded for dot-agents-dev by `[[verifier-owns-ci-watch-shift-left]]`)

## Status

**This pattern is now the FALLBACK**, not the canonical flow for dot-agents-dev work.

The canonical flow for projects with a `pr-ci` verifier_profile registered (per `[[verifier-owns-ci-watch-shift-left]]`) is:
- **Impl loop-worker exits at `merge-back`** — no CI polling, no Sonar fix-up
- **`pr-ci` verifier (dispatched via `verifier_profiles` registry)** owns the PR-CI-watch loop and auto-fixes mechanical issues
- **Lens reviewers** (architecture-standards / acceptance-invariants / adversarial, landed via PR #122) audit pre-merge on demand
- **Parent** runs `da workflow delegation closeout --decision accept` which auto-advances task status per `commands/workflow/delegation.go:1608-1649` (no separate `workflow advance` needed — see `[[verify-task-status-vs-pr-history]]`)

This lesson stays as:
1. **Historical context** for sessions before the verifier_profile + staged dispatch + delegation closeout contract landed (codex #119 + PR #122 + PR #123)
2. **Fallback for consumers WITHOUT a `pr-ci` verifier_profile registered** — the impl-owns-loop pattern is still correct when no verifier exists to take the loop

## When this pattern still applies (fallback mode)

If the project has NOT registered a `pr-ci` (or equivalent) entry in `.agentsrc.json`'s `verifier_profiles` + `app_type_verifier_map`, OR if a quick one-off PR is being shipped without the staged-dispatch chain, the impl loop-worker should own the readiness loop end-to-end. The original guidance below applies.

## Pattern (fallback)

When spawning a loop-worker for a delegated task WITHOUT a verifier profile to take over, the worker is responsible for monitoring its OWN PR's CI + Sonar state and auto-fixing any review-blockers until the PR is truly review-ready. Parent context does NOT spend cycles on coverage-gate or cog-complexity follow-up commits.

## Root cause (why we shifted left)

Parent (orchestrator) context is the scarce resource. Each round-trip "worker finishes → parent inspects PR → parent fixes coverage → parent pushes" burns ~2-3k tokens of parent context per PR per fix. With 3-5 PRs in flight per wave, that compounds to 10-20k tokens of context just for mechanical coverage/cog fixes that the worker could have done in the same session.

Wave 9 burned multiple parent-context cycles fixing #100 (deps.go 75%→100%), #101 (signal_hook_outcomes.go 93.85%→98.46%), and #102 (cog 36→under 15) — all preventable.

That cost continued to repay per impl session because each worker re-learned the SonarCloud API, the `gh pr checks` schema, the coverage-gate.sh exit codes. The successor pattern (`[[verifier-owns-ci-watch-shift-left]]`) amortizes that across PRs via a single specialized profile.

## Rule (fallback mode — applies only without a verifier profile)

1. **Worker runs a self-monitor after initial PR push.** Poll `gh pr checks <n>` + SonarCloud quality-gate API at `/api/qualitygates/project_status?projectKey=<project>&pullRequest=<n>` until terminal state. 60-90s interval.

2. **Worker auto-fixes these in-place** (no parent intervention):
   - CI test failures (rerun once via `gh run rerun --failed`; else debug + push)
   - Per-file coverage gate <95% — add targeted tests; use `go tool cover -func`
   - SonarQube `new_coverage` <95% — same fix
   - Cognitive-complexity criticals (S3776) — extract helpers; mirror the proven factoring patterns from `commands/review.go` and `internal/platform/hooks_test.go`
   - Duplicated-lines-density >3% — extract shared helper or table
   - Duplicated literals (S1192) — extract a status/error const block

3. **Worker returns to parent ONLY when:**
   - PR is fully review-ready: all checks pass, Sonar gate OK, new_coverage ≥95%, 0 open issues, 0 security hotspots, dup density 0%; OR
   - Worker is hard-blocked and must fold back (per `[[no-lazy-allowlist-tech-debt]]` + `[[validate-bundle-against-head]]`) — fold-back artifact written, no commits.

4. **Worker reports the final PR URL + the readiness summary line:**
   `PR #<n> · ✅ READY · N/N checks · gate OK · new_cov X% · dup Y% · hotspots Z% · 0 issues`

5. **Parent's role shifts to terminal-only orchestration:**
   - Spawn workers with the auto-fix mandate.
   - Receive only the terminal READY or FOLD-BACK report.
   - Run `da workflow delegation closeout --decision accept` (NOT `workflow advance`) per `[[verify-task-status-vs-pr-history]]`.
   - Author new tasks / fan out next wave.

## Migration path (for dot-agents-dev to the canonical flow)

1. **Land `pr-ci` verifier_profile** per `[[verifier-owns-ci-watch-shift-left]]` (deferred until codex PR-B + config-v2 phases p1+p4 land — see `.agents/proposals/codex-019e6245-examination-and-sequenced-plan.md` §10).
2. **Update orchestrator session-start skill** to spawn impl + verifier per app_type instead of one all-in-one loop-worker.
3. **Trim loop-worker AGENT.md** to defer the CI loop to the verifier profile when present.

## Cross-references

- `[[verifier-owns-ci-watch-shift-left]]` — the canonical successor pattern: `pr-ci` is a **generic starter default** (GitHub gh CLI + SonarCloud — the common case); projects on other platforms (Bitbucket, GitLab, CircleCI, CodeQL, Snyk, etc.) override with their platform-specific commands while keeping the same classification/auto-fix template. Lands via codex PR-B + config-v2 + default `pr-ci` verifier_profile.
- `[[verify-task-status-vs-pr-history]]` — `delegation closeout --decision accept` auto-advances; never run `workflow advance` after delegated work
- `[[no-lazy-allowlist-tech-debt]]` — workers (impl or verifier) must NOT allowlist past coverage gates; auto-fix path is "add tests" not "edit coverage-exceptions.txt"
- `[[bundle-scope-via-code-graph]]` — workers' auto-fix may need to extend write_scope mentally (a new `_test.go` for an in-scope file is fine; a wholly new file is fold-back territory)
- `[[parallel-worker-branch-drift]]` — workers using `git worktree add` for isolation makes the self-monitor safe across parallel spawns
- `[[loop-worker-vs-general-purpose]]` — only loop-worker has bounded-stage contract; general-purpose workers run the full readiness loop without a delegation bundle
- `[[const-extraction-triggers-cpd-on-tables]]` — fixing S1192 dup literals via const extraction can trigger Sonar CPD when the literal sits inside a tabular layout; data-driven refactor (loop over typed slice) is required
