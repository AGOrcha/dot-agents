# Backlog drain queue (from triage 2026-06-28)

Source: full triage map in session transcript. This is the actionable queue for the
backlog-drain heartbeat. Skip flaky-archive + owner-gated/secret-blocked items.

## ✅ CODEX BLOCK RESOLVED (2026-07-03 morning) — it was a WEEKLY USAGE CAP, not a balance.
##   Owner has TWO codex accounts (`codex-auth list` / `codex-auth switch <query>`): Plus (personal,
##   was 100% weekly-capped) + Business (0% weekly). Owner re-authed personal → gating resumed. All the
##   "out of credits" ticks above were the Plus weekly cap. GATE FLUSH (serial, one gate/window — NEVER
##   parallel, that split a small window):
##     #279 agent-runner  → SOUND → MERGED (f3f5ec51). #285 verifier-go → NOT-SOUND (sandbox-escape:
##       empty workdir runs on host) → fixed → SOUND → MERGED (853ffa85). ⇒ harness-driver UNBLOCKED.
##     #287 gencore → 3 rounds (nil-Profile panic, KG-error-swallow, auth-disclosure) → SOUND → MERGED
##       (3102791b); #283 + #284 CLOSED (superseded). 
##     #277 admin-cli → 2 rounds (prune archive-match + delete-order bugs; now audit-verify must
##       fail-closed on torn/forged tail) → fix-worker ae3707e8 in flight (round 3).
##     #288 harness-driver (NEW, r4 orchestration, Harness.Run→EvalRun, 100% cov) → base-drift compile
##       fail (built pre-#287, used removed gogen.Register) → worker a7e713cce rebasing onto master +
##       switching to gencore.Register(reg,querier,gogen.Profile).
##   LESSON: merge an API-CHANGING PR (#287 reshaped the gen packages) BEFORE dispatching workers that
##   build on that API — else they build on stale base + break on rebase. (cf. parallel-worker-branch-drift)
##   DEFERRED cleanup: (a) workflow-state sync to master — `da workflow advance --commit-state` committed
##   to the checked-out feat/verifier-go branch (34-path canonical-store sync, preserved on branch
##   wf-state-sync-2026-07-03; local feat/verifier-go DIVERGED from origin, do NOT push it). Advance the
##   merged tasks (agent-runner done; verifier-go/generator-go/harness-driver pending) + sync to master
##   from a master worktree. (b) #286 topology still owner-pending.

## ⚠️ NEW: codex TLS error (2026-07-03 ~14:25Z) — `invalid peer certificate: UnknownIssuer` on
##   wss://chatgpt.com/backend-api/codex/responses. Probes now retry ~90s on TLS before the credit error
##   (defeats the cheap-fast-fail-probe assumption; explains the ambiguous slow probes — they were TLS
##   retries, NOT credit windows). PRIMARY blocker is still "out of credits", but IF a top-up lands and
##   codex still fails, this cert issue (proxy/MITM/cert-store? UnknownIssuer) may need fixing too. Watch
##   whether it persists. Reduce probe cadence (probing is no longer cheap; always dry many ticks running).
##
## 🔁 SERIALIZE GATES — one full gate per window, NEVER parallel (2026-07-03 morning).
##   codex refills in tiny ~1-review windows. I parallel-launched #285+#279 on one small window →
##   they SPLIT it and BOTH died "out of credits" mid-review, zero verdicts. Wasted the window.
##   RULE: launch exactly ONE full background gate per tick (the gate IS the probe — no separate probe,
##   which consumes a sliver a small window can't spare). Single gate fits a ~1-review window → completes;
##   dry window → fast-fails. Serial order: #285 → merge → #279 → merge → #287 (closes #283/#284) → #277.
##   ~1 PR merged per window; ~4 windows to flush. A SUBSTANTIAL owner top-up would flush all at once.
##   Currently DRY (window exhausted by the parallel mistake). All 4 PRs still GREEN + staged.
##
## ⛔ CODEX DEGRADED TO UNUSABLE (2026-07-03, later) — can't complete even ONE review.
##   Gate now INITIALIZES a session (gpt-5.5, reasoning high) but dies "out of credits" MID-review before
##   producing a verdict. Worse than earlier this arc (which managed 2 gates). The credit trickle is
##   sub-review-sized → NO gate can complete. HARD BLOCKER: only a substantial owner top-up unblocks it.
##   Probing consumes only the useless trickle (can't finish a review anyway), so background probe-gates
##   are harmless — but SLOW the cadence (confirmed-same result each tick). Owner escalated (sharper push).
##   4 PRs remain staged GREEN + fix-complete (#285/#279/#287/#277) → will merge instantly on real top-up.
##   FLUSH-ON-RETURN order: #285 → #279 → #287 → #277; then close #283/#284 (superseded by #287),
##   resolve #286 topology (owner), ping orchestrator to build harness-driver.
##
## 🔵 GATE STRATEGY (2026-07-03) — codex windows are TINY (~1 review) + intermittent, then "out of credits".
##   Confirmed: a session ran a real ~8-min review then went dry; balance message is literally "out of
##   credits" (a BALANCE issue, not a rate cap) — top-ups keep being too small. LESSON: do NOT foreground-
##   run a gate with a short Bash timeout (I wasted a window by SIGTERM'ing an 8-min review mid-flight).
##   NEXT-TICK PROBE = launch the PRIORITY gate (#285) DIRECTLY in background (run_in_background, 600000ms):
##   if a window is open it produces a verdict; if dry it errors "out of credits" fast. Either way no waste.
##   Flush order when a window opens: #285 → #279 → #287 → #277 (one per window; merge SOUND before next).
##   All 4 PRs GREEN + fix-complete + staged. Owner ask (substantial codex top-up) already sent; not re-pinging.
##
## 🟠 GATE ROUND (2026-07-02) — codex intermittent (~2 gates/top-up) then DRY; ALL FIXES NOW PUSHED.
##   Codex processed #285 + #279 (both NOT-SOUND, real findings), then went dry (#277 blocked). Tiny
##   balance; needs a BIGGER top-up to flush. All fixes built w/o codex + pushed — queue is now fully
##   staged: everything fix-complete + green-pending-CI, frozen ONLY on codex credits. Re-gate order
##   when credits return: #285 → #279 → #287 → #277 (crit-path #285+#279 first → unblocks harness-driver).
##   #285 verifier-go → was NOT-SOUND ("timeout enforced via context" untested). FIXED+PUSHED
##     (worker ac73a20b): TestVerify_TimeoutExpires (real 3s subproc + 1s timeout asserts cancellation
##     fired) + portable helperSleepCmd (cross-OS) + strengthened cancel test; 25/25 pass. CI re-running.
##   #279 agent-runner → was NOT-SOUND (OQ1 auth-not-disclosed + Claude adapter gap). FIXED+PUSHED
##     (worker a57928927, commit 52194f96): OQ1 owner-ruling recorded in doc.go + PR body disclosure;
##     Claude json-telemetry rationale documented + TestClaudeRunner_FullArgv added; 100% cov. CI re-running.
##   #287 gencore (supersedes #283/#284) → SonarCloud gate OK (dedup WORKED); only new-issue was 1×
##     go:S3776 (test-fn complexity 16>15). FIXED+PUSHED (main-loop, c82d9de7): extracted a t.Helper().
##     CI re-running. On merge, CLOSE #283 + #284.
##   #277 admin-cli: green; gate BLOCKED (codex dry); re-gate when credits return.
##
## 🔴 CURRENT STATE (2026-07-02) — CODEX OUT OF CREDITS AGAIN; gate queue frozen.
##   Confirmed via real gate on #277: "Your workspace is out of credits." Bottleneck = codex (the
##   cross-brain gate). Building NEW work now just grows WIP upstream of a frozen bottleneck — does
##   NOT increase merge throughput. So: keep already-built PRs green + ready; do NOT dispatch fresh
##   build workers until credits return. Owner action = top up codex workspace credits.
##   GATE-READY when credits return (green, ungated): #277 admin-cli, #279 agent-runner.
##   #285 verifier-go: was RED on S1186 (empty no-op cancel @ verifier.go:161) — FIXED + pushed
##     (000d81f8), CI re-running; joins gate-ready set once green.
##   #283 generator-ts / #284 generator-py: RED on new_duplicated_lines_density 18.6/18.7% (≤3%).
##     Root: cross-file duplication vs the MERGED go generator (~15 near-identical scaffolding funcs).
##     OWNER RULED (2026-07-02): "the duplication is a sign of bad design and impl" → FIX PROPERLY
##     (extract shared core; do NOT paper over with a sonar exclusion). DISPATCHED opus worker
##     a70d0a4287680c6ea → branch refactor/r4-gen-shared-core: extract internal/eval/gen/gencore
##     (engine once, parameterized by a LanguageProfile) + refactor golang(merged)/ts/py onto it as
##     thin adapters. ONE coherent PR off master, SUPERSEDES #283+#284. Build now (no codex needed),
##     gate when credits return (1 clean gate replaces 2 broken PRs → reduces eventual gate load).
##   COPILOT (#279): invocation already CORRECT — `copilot -p <prompt>` (binary `copilot`, flag `-p`,
##     explicitly NOT `gh copilot suggest`). Harness label "gh-copilot" is consistent w/ product-name
##     convention (cf. "claude-code"/"codex") + test-asserted — left as-is. #279 stays green/ready.
##   #286 (t08-frontend-skeleton, r2): GREEN (Vite+React+TS SPA in web/dashboard/, all web gates pass).
##     WRITE_SCOPE DEVIATION — worker added repo-ROOT pnpm-workspace.yaml + pnpm-lock.yaml (outside
##     t08 scope ['web/dashboard/']). RULING (2026-07-02): web/dashboard/ content ACCEPTED (in-scope,
##     green); the 2 root files REJECTED from #286. Root cause = PLAN-COHERENCE GAP: t16-coverage-gate-
##     and-ci's CI uses `pnpm --filter dashboard` (needs a root pnpm-workspace) but NO task's write_scope
##     owns that root file (t16 scope = dashboard.yml + coverage_test.go + web/dashboard/.github/... only),
##     AND it conflicts w/ repo convention (docs/web = self-contained npm, package-lock.json in-subdir,
##     no root JS workspace). OWNER TOPOLOGY DECISION NEEDED (low-urgency, codex-frozen):
##       (A, recommended, convention-preserving) web/dashboard/ self-contained — lockfile in web/dashboard/,
##          NO root workspace; amend t16 CI to `cd web/dashboard && pnpm test|build` (not `--filter`).
##       (B) deliberate repo-wide pnpm monorepo — adopt root pnpm-workspace, assign root file to t16's
##          write_scope, keep `pnpm --filter`. This is a NEW repo tooling convention → owner's call.
##     #286 HELD (not merge-ready) until resolved; resolve at gate-flush touchpoint (same as codex top-up).
##     Not re-dispatching a restructure worker until the direction is set (avoid wasted/wrong-direction work).
##   session-handoff-journal chain (heartbeat's stated priority) = ALREADY COMPLETE (see below).
##
## ⚠️ STALE-TRIAGE CORRECTIONS (2026-06-30, from status-reconciliation pass — PR #242)
## - #207 (graph-backend t5) IS MERGED — NOT owner-gated. Lines 44/49/81-87 below are obsolete;
##   t1/t2/t4/t5 all shipped. The bug-2 ruling is moot (it merged).
## - docs-starlight dm5-prod (#94) + dm6-minting (#93) ARE MERGED — NOT secret-blocked (line 143).
##   Whole docs-starlight plan is 8/8 shipped. (Separately: confirm live CF Access PROD secrets applied.)
## - 37 shipped-but-stale task statuses synced to completed via PR #242 (see PR body for the full list).
## - Genuinely-open net-new work = the R-series build-out (r2=16, r3=6, r4=16, r5=7) + scattered tails.

## ✅ CODEX RESTORED — gating resumed (2026-06-29). Order: p5 #211 → p3b #212 → p6 → p7/p8 → p9.
## (Also done this stretch: macOS signing fix — QUILL_SIGN_P12 rebuilt full-chain, #203 verify-gate
## merged, #214/#215 fsguard allowlist fix, v0.4.2 re-release in flight.)

## ✅ SESSION-HANDOFF-JOURNAL LAYER COMPLETE (10/10, all tasks status=completed) 2026-06-29.
##   p0-p9 merged: #205,#206,#208,#210,#212,#209,#211,#216,#218,#217,#220. Plus 3 concurrency
##   fixes landed: #219 (kg fwd-slash seam + agentslock acquire delete-pending race),
##   #221 (fsops.RemoveAll Windows release retry). OPEN FOLLOW-UP (owner priority call):
##   .agents/active/fold-back/journal-lock-release-race.md — TestEmitConcurrentNoTornLines
##   low-rate lock-RELEASE lifecycle flake (integrity intact; careful fix, not patched on a tick).
##   NEXT: plan archive (blocked — archive doesn't persist, see DEFERRED) + impl-results writeup [DONE].
## ✅ MASTER FULLY GREEN (2026-06-29). #222 cleared all 16 sonar-new-issues (gated SOUND — both
##   prod refactors config.Load/runWorkflowTaskUpdate proven behavior-preserving); master post-merge
##   run = success on all 3 Test jobs + Coverage gate. LESSON: the Coverage gate (sonar-new-issues)
##   runs SEPARATELY on master post-merge — watch it, not just per-OS Test jobs.
##
## == DOCS/DEMO WAVE 2 COMPLETE (2026-06-30) — architect-grade site + deck ==
## 4 concept docs MERGED, each accuracy-gated through MULTIPLE rounds (the gate caught real
## overclaims in EVERY one — the verification thesis proving itself): concepts/verification-and-scoring
## (the aleph/no-partial-credit thesis), concepts/config-model, concepts/workflow-artifact-model,
## concepts/platform-projection. #231 published them + OUTCOME_SCORING_RUBRIC + VERIFIER_REVIEWER_TEMPLATES
## (26 public pages). #232 fixed the EMPTY SIDEBAR (real "sparse site" cause — autogenerate doesn't see the
## custom loader; now explicit from public-pages.mjs → 31 nav links). Lychee gate flaw fixed (#231: site's
## own origin excluded — was blocking every new-page PR). #233 = the ASDLC demo deck (Marp → site page +
## delivered .pptx; config-v2 worked example through the verification audit-trail; honest defense-in-depth
## caught-defects story incl #147 ESCAPED). OPEN: pptx slide-18/11 overflow polish (owner call); deck
## content is SOUND + site renders full.
##
## == DEMO PREP COMPLETE (2026-06-29 eve) — master fully green ==
## Owner-directed docs/demo wave, all merged: #223 onboarding skill + Getting Started page;
## #224 architect diagram deck (6 mermaid + 2 stubs filled + snapshots; gate caught+fixed 5
## accuracy errors); #225 link fixes + in-site rewriter (shared public-pages.mjs) + lychee CI
## gate; #226 e2e fix (filled stubs -> graph routes). Each codex-gated. master green incl new
## lychee + UI e2e. LESSON: docs gate must run the Playwright e2e too (npm build alone missed
## the stub-heading regression #226 fixed).
## AWAITING OWNER DISPOSITION (green + correct, do not merge without ruling):
##  - #204 antigravity: root-caused (Lstat guard, also fixed latent all-platform Win hook bug),
##    gate-SOUND, sourcing=projection (owner's rule already holds). Merge as platform vs hold as probe?
##  - #207 graph t5: CI green, mechanism correct, headline predicate = honest ApproximationNotice.
##    v1.5=NARROW recorded (.agents/active/fold-back/v1.5-dsl-field-to-field-from-t5.md, §12.4 blocker).
##    Merge w/ approximation now vs hold for the v1.5 increment? v1.5 impl is post-demo.
##
## == AUTONOMOUS DRIVE PAUSED: backlog drained to owner-gated / net-new only ==
## Remaining open PRs all owner-gated/proto: #207 t5 (OWNER bug-2 ruling), #204 L4 antigravity (OWNER
##   F1/F2+merge), #181/#184/#185 protos. Remaining backlog = CREATE-PLAN / IDEATION (net-new initiation,
##   better with owner eyes). Plus the captured lock-release-race fold-back (owner priority call).
## Nothing autonomously actionable without an owner ruling or net-new initiation. Loop on relaxed heartbeat.
## IN FLIGHT: p9-e2e-integration-test (LAST task) — #220, codex-SOUND. windows+ubuntu GREEN
##   after #219 (acquire-race) + #221 (fsops release retry) both merged. macOS hit a SEPARATE
##   pre-existing LOW-RATE flake: TestEmitConcurrentNoTornLines lock-RELEASE race (rmdir ENOTEMPTY
##   on POSIX / sharing-violation on Windows) — integrity INTACT (no torn lines), only lock-dir
##   cleanup races (reclaim-vs-release lifecycle bug, NOT caused by p9). Re-ran the macOS job to
##   confirm p9's own change is clean; merge p9 on green. Race captured for a CAREFUL fix (not
##   patched on a tick — mutual-exclusion/liveness risk): .agents/active/fold-back/journal-lock-release-race.md
##   FIXES LANDED THIS BRANCH-STRETCH: #219, #221 (both gated SOUND).
## LESSON RE-CONFIRMED: check ALL CI jobs (esp windows-latest) before calling a PR merge-ready;
##   master had been red on windows since p3b/p5 (two Windows-only bugs) — caught only via p7's CI.

## IN FLIGHT (do not re-dispatch)
- (none — see PAUSE-BLOCKED below)

## PAUSE-BLOCKED (codex credits out → little clean autonomous work remains)
- pr-event-source + unified-event-contract: NOT needs-plan — ALREADY SHIPPED+MERGED via
  layered-pr-fanout (lpf-event-foundation + lpf-pr-producer); code in internal/events/ cites the
  spec decision IDs. Triage false-positive. (Optional: annotate the 2 design.md as implemented +
  link to layered-pr-fanout; route deferred §5 work — da service proxy server, coach/poller
  migration, event.sast.* — into r3-background-worker-service per the spec's graduation note.)
- scoped-knowledge-graphs / org-config-resolution: openQ=1 each → need IDEATION first (codex) → blocked.
- All remaining CREATE-PLAN candidates must be SHIPPED-verified before planning (triage list unreliable).
=> Net: with codex out, gating + ideation + the clean create-plan items are all blocked. Loop holds for credits.

## MERGED (session-handoff-journal): p0 #205, p1 #206, p2 #208, p3a #210, p4 #209 (5/10)
## REMAINING: p3b (running), p5 (gating) → then p6-cmd-surface → p7-hooks/p8-handoff-skill → p9-e2e

## CODE-SOUND, OWNER-GATED (do not auto-merge)
- graph-backend-adapter-contract/t5 (#207): all 4 gate findings resolved (refresh-rebuild, CheckCompat
  full ref-path + collision-proof signature, lockfile state-machine). ONLY open item = bug-2 (escalated):
  the compliance view's "changed since evidence collected" predicate is NOT expressible in v1 DSL, so a
  LOUDLY-marked source-stale ApproximationNotice placeholder ships. OWNER DECISION: (a) merge #207 now
  with the marked placeholder [RECOMMENDED — 3 real fixes + lockfile + collision-proof compat are sound
  + valuable; approximation is explicit not silent; defer/extend-DSL as a follow-up; unblocks t6/t9], or
  (b) hold #207 until you rule defer-view-until-v1.5-DSL vs extend-DSL-now.

## MERGED THIS SESSION (loop)
- session-handoff-journal/p0 → #205 (AcquireFileLock export, codex SOUND after idempotency fix)
- session-handoff-journal/p1 → #206 (journal core: envelope/identity/appender, codex SOUND)

## DOGFOOD FINDING (delegation lifecycle)
For a SEQUENTIAL single-scope chain (p0..p9 all touch internal/journal), `da workflow fanout`
is friction: (a) each completed task's delegation record lingers active and the scope-guard
then REJECTS the next same-scope fanout; (b) `delegation closeout` requires a merge-back.md
artifact that a PR-based flow doesn't produce, so it can't release them. WORKAROUND adopted:
dispatch sequential-chain tasks DIRECTLY (Agent + worktree) + `da workflow advance`, skip fanout.
Reserve fanout for PARALLEL disjoint-scope work where conflict-detection earns its keep.

## DONE THIS SESSION
- brew/quarantine root-cause → v0.4.1 macOS sig broken (PR #203 verify-gate; owner re-sign needed)
- session-handoff-journal PLAN created+active (→ READY-WORK; p0 now dispatched)
- app-type-profiles IDEATION → 3 forks resolved, spec plan-ready; OR-1/OR-2 escalated (see OWNER-DISPOSITION). Plan creation queued behind owner inputs.

## CREATE-PLAN queue (specs ratified/stable, no plan, not shipped) — one per fire
1. scoped-knowledge-graphs (foundational, openQ=1)
2. org-config-resolution (extends shipped config layers, openQ=1)
3. pr-event-source + unified-event-contract (small, openQ=0 — pair them)
4. (verify-not-covered first) da-project-specifics-source, cli-runner-verifier, project-audit-plan-sync-expansion

## IDEATION queue (open forks block planning) — one per fire, ideation-cycle skill
1. ideation-system-composition (ratify=29, openQ=1 — resolve 1 Q → graduates to plan; HIGH leverage)
2. lens-evidence-policy (openQ=3, per-lens evidence-form)
3. skill-tiering-contract (openQ=2, tier boundaries/promotion)
4. work-tracking-storage-abstraction (openQ=2, backend seam vs KG-as-SOT)
5. planning-agent-pipeline-and-interactive (only idea.md — needs full ideation)

## READY-WORK queue (live plans, concrete next tasks) — gate+merge per loop rules
- session-handoff-journal: p0 ✅merged → p1 in-flight → p2-command-schemas, p3a/p3b-emit, p4-snapshot, p5-recover, p6-cmd-surface, p7-hooks, p8-handoff-skill, p9-e2e (sequential deps)
- graph-backend-adapter-contract: t5-cross-adapter-reads-from, t6-bridge-decommission, t9-sdk-materializeview-gate
- agent-ops-hardening: p4-fanout-gate-enforcement, p5-routing-doc-cross-harness-evidence
- coverage-gate-per-file: cg6b-ratchet-loop (in_progress), cg-verify-close
- kg-ideate-skill: t1-kg-brief-molecule
- first-class-resource-docs: rd-resource-trio-guides, rd-score-guide, rd-workflow-catalog-gaps
- local-gate-ci-parity: SYNC stale task-state first (t1 shipped #178), then t2..t7

## DEFERRED / NEEDS-FIX (do not auto-run)
- ARCHIVE 17 completed plans — BLOCKED: `da workflow plan archive` moves don't persist
  (plans re-project / not git-committed). Needs investigation before re-attempting. Includes:
  the 5 already status=completed (r1-outcome-scoring, stage-profile-*, verifier-precondition-policy,
  workflow-client-commands, workflow-commit-command) + reconcile-then-archive (coverage-95-staged,
  extends-oci-relax) + closeout-stub tails (production-code-helper-extraction, platform-driven-diagnostics,
  shared-target-projection-wiring, r1-5-hook-enforcement-telemetry, root-command-decomposition).

## OWNER-DISPOSITION (do not act without ruling)
- L4 antigravity probe (PR #204, draft): owner must (1) confirm antigravity research assumptions (paths INFERRED; it reportedly reads canonical .agents/ directly — SOT collision), (2) rule on the descriptor schema F1/F2 + the NEW capability D (zero-projection read-path), (3) decide whether to merge the antigravity harness as-is or after confirmation. Then the L4 descriptor-schema impl plan can be written.
- app-type-profiles OR-1: which seed profiles ship in dotagents-builtin + naming convention (shape-first vs language-qualified; non-code seeds in v1?). Then CREATE-PLAN app-type-profiles.
- app-type-profiles OR-2: may a public-source profile's CI run a PRIVATE behavior-corpus? (conservative default = refuse). Then CREATE-PLAN app-type-profiles.
- pr10-branch-split (pr6 TS port PARKED — likely close-as-superseded)
- sdd-kg-schema-v4-corrections (no spec backing — confirm intent)
- release-patch-train (standing release stub)
- dm6-da-sso-autowire + docs-starlight dm5-prod/dm6-minting (secret-blocked)
- payout-upgrade-refresh-input (consumed #153/#155 — archive/close)

## 2026-07-04 wave status (heartbeat converged)
- da-recipe-scripts: COMPLETE p1-p5 (#316/#323/#327/#330) + state-sync #336 merged.
- session-handoff-journal: COMPLETE p0-p9 (all merged, sonar-clean #222). Heartbeat "chain in flight" text is STALE.
- kg-ideate ideation: DELIVERED #337 (merged). Finding: the 3 "design-blocked" specs are stale/shipped, NOT design-blocked.
- 5 owner rulings CAPTURED (2026-07-04): FIPS opt-in (Go 1.26 default fips140=off); registry BYO/no-default (future public = v2+); graphstore daemon deferred WITH trigger-criteria; planner --fold-back ADOPTED (opt-in); SLICES.yaml Q6 kept OPEN.
- Reconciliation worker a1651ca4 IN FLIGHT → docs PR (3 specs to shipped-reality + record rulings). Gate light (docs-only) on report.
- #332 release/0.5.0: OWNER-HELD (docs predecessor #334 merged; clear to cut on owner go).
### Greenlit follow-ons (queue, not urgent):
- [ ] planner-evidence: implement opt-in `--fold-back` flag (scope-escape auto-tracks dangling item; reduces command chaining).
- [ ] graphstore: instrument daemon-trigger metrics (SQLITE_BUSY/retry rate, p99 lock-acquire latency, concurrent-writer count, WAL checkpoint stall).
