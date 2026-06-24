# Agent-ops hardening — retrospective + design (spec)

**Spec id:** `agent-ops-hardening`
**Status:** design artifact (spec tier) — doubles as the retrospective record. Plan: `workflow/plans/agent-ops-hardening/`.
**Source:** a 4-stream reflection (2026-06-23) on the config-v2/0.4.0 run + the recent history + the Claude and Codex **session transcripts**.

## 1. The reflection (4 streams)

- **This run (PRs #88–#118):** 7 recurring patterns; cross-cutting root cause = *"trust the code, not the prose/queue/local-branch"* (gocognit≠Sonar, stale spec/comments, stale branch for `eligible`, spec-derived write_scopes).
- **Recent history (#82–#118, 52 lessons):** ~57% of PRs were pure overhead (fixups, state-reconciliation, docs-corrections, CI/release fragility). Throughline: **the lessons corpus is mature but lessons are documentation, not enforcement** — the same recognized pattern recurs because nothing mechanically gates it (the write_scope/co-commit wall recurred t11→t12→t13→t13a *twice* after a lesson said "promote on 2nd occurrence").
- **Claude sessions (11 transcripts, 307MB):** quantified taxes — TCC `~/Documents` lock **190 hits/7 sessions**; sonar pre-push flake (dist/.scannerwork) **1562 hits** → `--no-verify` **461 refs**; S3776 rework **707 hits**; stale-queue "already done" re-discovery **483 hits** (the *real* delegation waste — workers do NOT refuse scope); manual CI poll loops (`gh pr checks` 309 + sleep/until 283); orchestrator tools re-fetched via ToolSearch (SendMessage ×10…); coordination:code ≈ **5:1** in big sessions.
- **Codex sessions (159 transcripts):** **1,222 `GOCACHE=/tmp` workarounds** (sandbox can't write the default Go cache) — the single biggest mechanical tax; same `~/Documents` TCC lock → worktree-escape + handoff-replay *reconstruction* (the p4g/p4h "reconstructed" cost); rate-limit caps with no failover killed a pipeline; **strengths:** clean cross-tool resume of rate-limited Claude sessions, impeccable read-only adversarial review (caught `sync.go:116` lock-writer + `docsaccess/client.go:137` http-token-leak), native spawn/wait/close delegation; **stealth leak:** worktree handoffs authored `Author: Claude Code session`.

## 2. Diagnosis

Two buckets explain nearly all the churn:
- **(A) Environment taxes** — a few one-time/config fixes (TCC `~/Documents`; Codex `GOCACHE`/sandbox; sonar scanner walking `dist/`/`.scannerwork`) account for thousands of friction events and the worst loops (`--no-verify`, worktree-reconstruction).
- **(B) Enforcement gap** — the right lessons exist; what's missing is *operationalization* into skills + pre-action gates so ground truth (code/master/Sonar) is re-derived mechanically at the moment of action.

## 3. Decisions (the outlet — what to build)

**P0 — environment (highest ROI, mostly one-time):**
1. Pre-set `GOCACHE`/`GOTMPDIR`/`TMPDIR` to a writable dir in the Codex sandbox profile (+ a `go` wrapper / sandbox permission for `da`/`~/.agents` writes). Kills the 1,222-event tax.
2. Add `dist/` + `.scannerwork` to the sonar-scanner exclusions so the pre-push gate stops flaking. Removes the root reason for `--no-verify`.
3. Move the repo out of `~/Documents` (only protected folders are Documents/Desktop/Downloads/iCloud) with a migration script that preserves Claude transcripts/memory (`~/.claude/projects/<encoded-path>/` + the `~/.claude.json` project key). Removes the TCC tax for BOTH tools; no FDA babysitting (brew-cdhash grants are upgrade-fragile; detached tmux server is its own responsible process).

**P1 — skills (operationalize the lessons):**
4. `safe-push` — clean build artifacts (dist/.scannerwork) + serialize pushes + run the gate; removes the reason to `--no-verify`.
5. `delegation-brief-hardening` — pre-fanout gate: derive write_scope from the code graph, walk all `*_test.go` callers of changed symbols, HEAD-validate the bundle premise, forecast coverage-delta for test moves. Retires the largest fold-back cluster (write_scope-misses-tests, stale-bundle, stop-and-report).
6. `release-cut-monitor-retry` — preflight pin-check the signing toolchain → cut → monitor `auto-release.yml` → on infra failure clean the stale tag + `workflow_dispatch` re-drive → classify known sign/timestamp failures (kernel32, DLSequence cast). + a CI check that fails if a signing/build tool installs unpinned.
7. `reconcile-eligible` orientation step in `orchestrator-session-start` (`git fetch origin master` + mark merged-to-master tasks done before fanout) + **pre-load the orchestrator toolset** (SendMessage/TaskStop/Monitor/PushNotification/sonarqube-gate) so they aren't ToolSearch-refetched.
8. Small helper skills: `pr-gate-wait` (wrap the 283 poll loops), `locate-and-tail-claude-session` (cross-tool resume), `sonar-issue-fetch` (MCP path, not curl/DNS).

**P2 — lessons + brief templates:**
9. New lessons: `pin-release-toolchain-and-make-releases-retryable`; `hermetic-home-for-state-resolving-tests` (the flaky-test root, generalizes the keychain lesson); `gates-must-be-locally-reproducible` (if a gate isn't locally checkable, fix that, don't CI-round-trip).
10. Brief-template hardening (delegation bundle defaults): keep functions under Sonar cognitive-complexity 15 (extract helpers; gocognit≠Sonar); write_scope MUST include the tests a change breaks; front-load read-only boundary for plan/review tasks; "a skipped/tagged cross-platform test is UNVERIFIED until its CI shard is green"; sanitize FS paths for Windows (no `:` etc.).
11. Scrub the `Author: Claude Code session` stealth leak in any committed worktree-era handoffs.

**P3 — routing:**
12. Document Claude-vs-Codex routing: Codex → adversarial/second-opinion review, cross-tool resume, bounded staged delegation; Claude → heavy iterative Go build-test loops (until the env taxes are fixed, after which either).

## 4. Done criteria
The P0 taxes are gone at the source (CI green for the env/sonar PRs; the move-script preserves history); the P1 skills exist + pass `skill-architect` eval; the P2 lessons are indexed and the brief template is updated; a follow-up session shows a materially lower overhead-PR ratio than the 57% baseline.

## 5. Sequencing / notes
P0 is independent and highest-ROI — do first. P1 skills should each go through `/skill-architect` (deliberate design), not be hand-spun in bulk. The session-handoff journal (spec #88, post-0.4.0) is the structured-state fix for the cold-start re-orientation tax and complements `reconcile-eligible`.
