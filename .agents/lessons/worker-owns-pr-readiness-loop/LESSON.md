# Worker owns the PR readiness loop end-to-end

## Pattern

When spawning a loop-worker for a delegated task, the worker is responsible for monitoring its OWN PR's CI + Sonar state and auto-fixing any review-blockers until the PR is truly review-ready. Parent context does NOT spend cycles on coverage-gate or cog-complexity follow-up commits — workers handle those in-place.

## Root cause

Parent (orchestrator) context is the scarce resource. Each round-trip "worker finishes → parent inspects PR → parent fixes coverage → parent pushes" burns ~2-3k tokens of parent context per PR per fix. With 3-5 PRs in flight per wave, that compounds to 10-20k tokens of context just for mechanical coverage/cog fixes that the worker could have done in the same session.

Wave 9 burned multiple parent-context cycles fixing #100 (deps.go 75%→100%), #101 (signal_hook_outcomes.go 93.85%→98.46%), and #102 (cog 36→under 15) — all preventable.

## Rule (bake into every loop-worker briefing prompt)

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
   - Merge PRs when stacked review-ready signals accumulate.
   - Author new tasks / fan out next wave.

## Cross-references

- `[[no-lazy-allowlist-tech-debt]]` — workers must NOT allowlist past coverage gates; the auto-fix path is "add tests" not "edit coverage-exceptions.txt"
- `[[bundle-scope-via-code-graph]]` — workers' auto-fix may need to extend write_scope mentally (a new `_test.go` for an in-scope file is fine; a wholly new file is fold-back territory)
- `[[parallel-worker-branch-drift]]` — workers using `git worktree add` for isolation makes the self-monitor safe across parallel spawns
- `[[loop-worker-vs-general-purpose]]` — only loop-worker has bounded-stage contract; general-purpose workers run the full readiness loop without a delegation bundle
