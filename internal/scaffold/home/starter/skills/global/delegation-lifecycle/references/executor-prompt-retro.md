# Executor-prompt retro (worker-side hardening)

This is the **executor (worker) counterpart** to the orchestrator-side pre-fanout
gate and brief-template defaults (the orchestrator agent + `instructions/workflow.md`
§ 0 / § "Brief-template defaults"). That work hardened the side that *authors*
the bundle. This doc hardens the side that *executes* it — the `loop-worker`
agent, the global `loop-worker.md` profile, and `instructions/bundle-to-execution.md`.

**Why a worker-side gate at all, if the orchestrator already gates the bundle?**
Defects shipped this run even when a bundle existed, because the worker is the
last agent to touch the diff before push. A worker that trusts a bundle blindly
inherits every gap the orchestrator missed (stale write_scope, a not-yet-broken
test, a Windows path) and a green PR check that is racing or mis-bucketed. The
worker MUST re-derive the gate locally, not assume the orchestrator got it right.
These rules are deliberately *symmetric* with — never a copy of — the
orchestrator gate: the orchestrator decides what to put in the bundle; the worker
verifies the diff against the same invariants before merge-back.

Each rule links the owning lesson rather than restating it. Lessons live in the
project's `.agents/lessons/<name>/LESSON.md` (`[[name]]` is the index anchor).

## Defect → prevention map

These are the recurring worker-shipped defects that verify / review / CI caught
this run, and the prevention now encoded in the executor prompts.

| # | Defect a worker shipped | Prevention (where applied) | Owning lesson |
|---|--------------------------|----------------------------|---------------|
| 1 | `write_scope` omitted the tests the change breaks → coverage dropped / cross-package asserting tests went red after merge | Worker re-derives the coverage-delta locally before editing: walk `*_test.go` callers (Go) or manifest/file-tree/embedded-content tests (non-Go scaffold, e.g. `copy_test.go`); if a broken asserter sits outside scope, escalate, don't silently expand | `[[bundle-scope-via-code-graph]]`, `[[tests-for-each-slice]]`, `[[validate-bundle-against-head]]` |
| 2 | Cross-platform not validated before push: `strings.Replace(p,"~",HOME)` mishandled the Windows 8.3 short path (`RUNNER~1`); symlink tests not GOOS-guarded; hardcoded separators | Worker reasons about GOOS: never hand-join or string-munge paths — route through the project's fs helpers; mirror the package's existing Windows-skip convention for OS-divergent tests | `[[leverage-cross-platform-fs-helpers]]`, `[[match-ci-test-flags-locally]]` |
| 3 | Per-file coverage below gate at push time | Worker checks per-file coverage locally and closes the gap by **adding tests**, never by allowlisting new code | `[[no-lazy-allowlist-tech-debt]]`, `[[tests-for-each-slice]]` |
| 4 | Shell scripts not Sonar-clean: S7679 (positional `$1`→`local`), S1192 (repeated literal→`readonly` const), S7688 (`[`→`[[`) | Worker runs the project's shell-lint/Sonar gate locally on any `.sh` it touches and fixes these three before push; const-extraction for S1192 must avoid re-triggering CPD on tabular data | `[[gates-must-be-locally-reproducible]]`, `[[const-extraction-triggers-cpd-on-tables]]` |
| 5 | Go S3776 cognitive-complexity over the gate (nesting-weighted, ≠ `gocognit`) | Worker keeps new/changed functions ≤ 15 **on Sonar's S3776**, extracting nested bodies into helpers; pass the Sonar gate, not the local linter | `[[sonarcloud-gate-mechanics]]`, `[[gates-must-be-locally-reproducible]]` |
| 6 | Trusted a green PR check: the new-issues signal has a stale-read race and is bundled into the "Coverage gate" check, so green-on-the-PR ≠ clean | Worker runs the project's quality gate **locally** to a terminal state and does not treat a single green forge check as proof; auto-fix mechanical findings, escalate non-mechanical | `[[sonar-rating-gate-misses-new-issues]]`, `[[worker-owns-pr-readiness-loop]]` |
| 7 | PRs green individually but red once combined on master | Worker validates the diff against **fresh `origin/master`** (the active line), not a stale local base; rebase/merge fresh before declaring ready | `[[stale-local-master-ref]]`, `[[stale-local-checkout-mass-drift]]`, `[[parallel-worker-branch-drift]]` |
| 8 | `gh pr create` targeted the wrong remote (fork default, not the active line) | Worker always names the active-line repo explicitly on `gh pr create`/`gh pr list`; never relies on the inferred default in a multi-remote (fork) clone | `[[stale-local-master-ref]]` |

## What was APPLIED vs RECOMMENDED

**APPLIED** (high-confidence, concise imperative rules added to the worker prompts):

- `instructions/bundle-to-execution.md` — new "Worker self-gate (before push)"
  section: re-derive the coverage-delta (defect 1), GOOS/fs-helper reasoning
  (defect 2), local-gate-not-green-check (defect 6), fresh-`origin/master`
  validation (defect 7), explicit-remote `gh pr create` (defect 8), shell-Sonar
  (defect 4) and S3776 (defect 5) on touched files. Also strengthened the
  existing allowlist line (defect 3).
- `agents/global/loop-worker/AGENT.md` — added a one-line "Self-gate before
  closeout" pointer in Full-Slice Execution so the rule is reachable from the
  agent contract without duplicating the checklist.
- `profiles/loop-worker.md` — added two cross-platform / fresh-base discipline
  bullets, kept generic for cross-repo reuse.

**RECOMMENDED (needs review)** — judgment calls or larger refactors, deliberately
NOT applied here:

- *Make the worker self-gate a typed verifier stage.* Defects 4–6 are exactly the
  mechanical surface a `pr-ci` / SAST verifier_profile owns
  (`[[verifier-owns-ci-watch-shift-left]]`). Where a project registers such a
  profile, the worker should exit at merge-back and the verifier owns the loop —
  so the worker-self-gate text is explicitly scoped to **fallback mode (no
  verifier registered)**. Promoting the self-gate into a reusable verifier prompt
  is a bigger change that should land via the verifier surface, not the worker
  prompt.
- *A `--no-verify`-is-forbidden line in the worker prompt.* PR #134's brief-template
  default already encodes "never `--no-verify`" at bundle-authoring time
  (`[[gates-must-be-locally-reproducible]]`). Re-stating it in the worker prompt
  risks drift with the canonical statement; recommend leaving it bundle-side and
  only linking it.
- *Per-language gate-command inventory in the prompt.* The exact local commands
  (which scanner, which coverage tool, which shell linter) are project-specific
  and belong in the project overlay / `.agentsrc.json`, not the starter prompt
  (see Genericity below).

## Relationship to the orchestrator gate (no contradiction)

- The coverage-delta rule is **authored once** on the orchestrator side
  (`workflow.md` § 0d). The worker rule here is a **re-derivation / verification**
  of that same invariant against the final diff, and says so — it does not
  re-author the rule.
- The brief-template defaults (cog-complexity ≤ 15, write_scope-includes-broken-tests,
  read-only-for-plan/review, skipped-cross-platform-test-is-unverified, Windows FS
  sanitization, never `--no-verify`) are what the orchestrator *puts in the bundle*.
  The worker rules are what the worker *does with* them at execution + push time.
  They are two ends of the same contract, not duplicates.

## Genericity (route via `da config relevance`, not the starter)

These items are dot-agents-development-specifics that should NOT be hardcoded into
the generic starter prompt — they belong in the project overlay / per-app-type
relevance config so the genericity thread can route them:

- The concrete quality-gate command (SonarCloud free-tier local-scanner mechanics,
  `.scannerwork`/`dist` hermetic exclusions) — `[[sonarcloud-gate-mechanics]]`,
  `[[gates-must-be-locally-reproducible]]`.
- The active-line **remote name** (this project pushes PRs to `AGOrcha`, not the
  stale `NikashPrakash` fork): the *rule* "name the active-line remote explicitly"
  is generic; the *value* is project-specific overlay config.
- The Go-vs-non-Go coverage-delta split is generic by `app_type`, but the concrete
  manifest test to walk for non-Go scaffold work (`internal/scaffold/.../copy_test.go`)
  is a dot-agents specific the per-app-type `verify.core` relevance should name.
- The shell-lint / S3776 toolchain bindings (which linter, which Sonar profile)
  are per-app-type `verify` relevance, not starter prose.
