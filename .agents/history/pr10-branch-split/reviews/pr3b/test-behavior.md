# Test + Behavior Review — pr3b/workflow (PR#16)

_Lens 2 of 3 (tests + code), general-purpose reviewer, read-only._

## Verdict
**approve with nits**

## Critical findings
None.

## Major findings

1. **Draft-plans behavior change (`aaa21dc`) — correct, safe, well-pinned.**
   Purely additive. `collectEligibleTasksForPlan` (plan_task.go:1925) still
   guards `plan.Status != "active"` → returns `nil` for drafts, so a draft can
   never become actionable. The change only adds a hint surface
   (`collectDraftPlanIDs`, `renderDraftPlansHint`, `eligibleOutput.DraftPlans`,
   `state.go` warning). Pinned by `TestRunWorkflowEligible_DraftPlansSurfaceHint`,
   `TestRunWorkflowNext_DraftPlansSurfaceHint`, and E2E
   `lifecycleStepPlanCreate`. Old silent-skip provably gone.

2. **E2E lifecycle test (`736cf12`) is production-realistic.** Drives the real
   command surface (cobra harness + `runWorkflow*`), real temp
   HOME/AGENTS_HOME/repo, asserts real artifact bytes (bundle/checkpoint YAML,
   merge-back frontmatter, closeout archive glob) and the dependency-unblock
   transition. Not seam-driven; would catch real regressions.

3. **graphstore backend parity is genuinely tested.** sqlite and postgres both
   run the same `storetest.Run{NodeRoundTrip,KGNoteRoundTrip,KGNoteSearch}` via
   a shared `OpenStore` closure → behavioral equivalence by construction.
   Postgres testcontainers gating clean: `lazyPostgresDSN` memoises
   `pgSkipReason`, `t.Skipf`s when Docker absent, honours `TEST_PG_URL`, reaps
   containers via explicit `Terminate` (TestMain→`pgTerminateAll`) + Ryuk
   fallback. No leak.

4. **Cleanup commit `73ff88f` is behavior-preserving.** `buildEdgeAdjacency`
   over `edgeRowIterator` is a faithful extraction; both backends'
   `GetImpactRadius` stay covered. sqlite now surfaces `rows.Err()` where it
   previously did not — strict improvement. `covprofile_test.go` hits every
   malformed-line skip branch + valid/uncovered/missing-file/header-only; ≥95%
   gate met. `TestVenvExeCandidates_CoversLayouts` post-`anyCandidate` keeps
   identical bin+Scripts+.exe assertions.

5. **Migrations idempotency covered.** `TestEnsureSchema_IdempotentOnExistingDB`
   re-opens the DB, proves re-init doesn't error and data survives; plus
   table/index/column/unique-constraint suite.

## Minor findings / nits

- **CRG skip guard is structurally weak (root cause of the known local
  failure).** `DiscoverCRGBin` only does `os.Stat`/`exec.LookPath`, never
  probes the launcher (no `--version`). A broken/partial CRG on PATH is
  *discovered*, so `TestCRGBridgeFreshBuildRealCRG` would not skip — it would
  run `BuildReport` and fail. Out of PR scope per brief (CI installs working
  CRG), but recommend a cheap liveness probe so a degraded PATH entry skips
  rather than fails.
- Large slice history is mostly seam/error-path coverage padding, but the
  seams target real error branches, not impossible states. Acceptable.

## Notes
Behavior risk is confined to the one intentional change, which is correctly
additive and triple-pinned. E2E + parity harnesses give real regression value.
Only follow-up worth tracking: the CRG liveness-probe gap.
