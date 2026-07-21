# Workflow Performance Optimization — design + methodology

**Status:** draft (awaiting approval)
**Prompted by:** the `commands/workflow` Windows CI timeout (measured 437.963s
non-race on a real windows-latest-class box; the CI `-timeout` was bumped
300s→1200s as a scaffold). The bump is a band-aid; this plan makes the codebase
"faster across the board" — test-suite AND runtime — **deliberately, not
aimlessly**: every change is a hypothesis with a predicted, measured, falsifiable
win.

## 0. Measured baseline (evidence, not vibes)

| Metric | Value | Source |
|---|---|---|
| `commands/workflow` test wall-clock, Mac, non-race | **44.6s** | measured this session |
| same, Mac, `-race` | **82.3s** (1.85× race overhead) | measured |
| same, real Windows box, non-race | **437.963s (~10× Mac)** | measured on `pap-home` |
| `-race` on Windows | pushed >300s → CI timeout | CI #450 |

**The dominant amplifier is Windows git-subprocess spawn cost (~10× Mac), not
algorithmic complexity.** So the highest-leverage move is cutting the *number* of
git process spawns (tests and runtime), and every headline proof must be measured
**on Windows**, where the cost lives — a Mac-only proof would understate or miss
the win.

### Root causes (from the read-only perf audit, CONFIRMED via file:line)

**Test-time** (biggest wall-clock cost = repeated real-git fixture bootstraps, run serially):
- `internal/testutil.InitGitRepo` = **5 git subprocesses per fixture** (`init`, `config`×2, `add`, `commit`) — `internal/testutil/testutil.go:201-247`.
- `closeTaskTestRepo` used by **16 tests**, no `t.Parallel()` — `commands/workflow/close_task_test.go:22-50`.
- `setupArchivePlan` reused **18 times** — `commands/workflow/plan_task_test.go:28-57`.
- `state_ref_test.go` = the widest git-plumbing matrix (`git show`/`cat-file`/`update-ref`/`commit-tree` per test).
- Heavy git suites are **serial** (no `t.Parallel()` in close_task / commit_cmd / plan_task / state_ref).
- Cheap template already exists: `fakeGit` + `iterationCloseCommit(WithIncludes)` seams; the go-git commit impl (`gogitImpl`) is already in-process.

**Runtime** (process-per-op git plumbing):
- `collectWorkflowGitSummary` = **5 subprocesses** per `status`/`orient`/`checkpoint` — `commands/workflow/state.go:428-446,968-989`; `runWorkflowCheckpoint` then calls `gitModifiedFiles` which **re-probes `isGitRepo`** (duplicate spawn).
- `readPlanTaskRecordsFromStateRef` = **O(tasks) `git show`**; `writePlanStateRefCAS`/`buildStateRefCommit` = **O(files) subprocesses** (`cat-file -e`, `hash-object`, `update-index`, `write-tree`, `commit-tree`) per CAS attempt — `commands/workflow/plan_task.go:803-1038,899-979`.
- Reconcile walks all plans through that same per-blob path — `state_ref_reconcile.go:59-142`.

**Infra gap:** the perf harness (`scripts/perf/run-benchmarks.sh`, `docs/PERF_BUDGET.md`) covers `internal/config`/`internal/platform`/`commands/internal/lifecycle` — **not `commands/workflow`**. No workflow benchmarks, no pprof, no perf gate. We cannot currently *measure* the exact hotspot we're optimizing.

## 1. Methodology / guidelines (the "not aimless" contract)

1. **Measure-first — no optimization without a baseline.** Phase 0 builds the
   workflow benchmark + profiling harness and captures baselines BEFORE any
   optimization lands. A change with no before/after number is rejected in review.
2. **Hypothesis → prediction → proof.** Each optimization is stated as a
   hypothesis with (a) the mechanism, (b) a *predicted* quantified win + threshold,
   (c) the exact proof measurement, (d) a **kill criterion**.
3. **Windows-in-the-loop.** Headline metrics are measured on Windows (the user's
   box `pap-home` and/or a CI perf job), because the 10× amplification is
   Windows-specific. Mac numbers are a fast inner-loop signal, never the proof.
4. **Behavior-preserving.** Optimizations MUST NOT change behavior: full package
   tests stay green, coverage ≥ gate, 0 new Sonar, no public-contract change. A
   refactor that needs a test rewrite to pass is a red flag, not a win.
5. **Falsification gate (drop what doesn't pay).** If the measured win misses its
   threshold, **revert** the change — do not keep complexity that didn't earn its
   place. (Repo lesson: `prototype-experiment-fidelity-gate` / scientific-method
   spine — a passing test can prove the wrong thing; gate on the real metric.)
6. **Regression gate (wins must persist).** New benchmarks join the perf harness
   with budgets; a CI timing/bench guard catches silent erosion. The 1200s CI
   `-timeout` bump is a **scaffold** — a named exit criterion is bringing
   `commands/workflow` Windows `-race` back under a sane cap and reverting it.
7. **One lever per change, isolated proof.** No bundling; each hypothesis lands as
   its own reviewable change with its own number so attribution is clean.

## 2. Hypotheses (each: claim · evidence · prediction · proof · kill)

- **H1 — Shared committed-repo template fixtures + seam migration cut test git-spawns.** *Claim:* build one committed repo template per suite (or reuse `fakeGit`/seams for orchestration-only tests), reserving real-git bootstraps for 1–2 end-to-end smokes per command. *Evidence:* `InitGitRepo`=5 spawns × 16 (`closeTaskTestRepo`) + 18 (`setupArchivePlan`) + state_ref matrix. *Prediction:* ≥50% cut in `commands/workflow` git-subprocess count → ≥40% Windows wall-clock reduction. *Proof:* subprocess count (instrumented seam) + Windows `go test` wall-clock before/after. *Kill:* <20% Windows wall-clock win.
- **H2 — Collapse duplicate/served git-summary probes.** *Claim:* dedupe `isGitRepo` across `collectWorkflowGitSummary`→`gitModifiedFiles`, and reuse one summary read on the checkpoint path. *Evidence:* `state.go:100-145,968-989`. *Prediction:* −1..2 subprocess per `status`/`checkpoint`; measurable ms win on Windows. *Proof:* new `status`/`checkpoint` benchmark, before/after. *Kill:* no measurable win.
- **H3 — Batch/in-process the state-ref plumbing.** *Claim:* replace O(tasks) `git show` with a single `git cat-file --batch` (or go-git tree walk), and build the ref commit in-process (go-git) instead of `hash-object`/`update-index`/`write-tree`/`commit-tree` per file. *Evidence:* `plan_task.go:803-1038,899-979`. *Prediction:* state-ref read/write goes O(tasks) spawns → O(1); ≥60% latency cut on a 20-task plan (Windows). *Proof:* new state-ref read+write benchmark parametrized by task count, before/after on Windows. *Kill:* <30% win at 20 tasks, or any behavior/CAS-safety change.
- **H4 — Parallelize the git-heavy suites once fixtures are cheap/isolated.** *Claim:* `t.Parallel()` on the (now cheap, isolated) suites. *Evidence:* no `t.Parallel()` in the 4 heavy files; pure-fixture suites already parallelize. *Prediction:* wall-clock ↓ by ~min(cores, suite count) factor on the parallelizable remainder. *Proof:* Windows wall-clock before/after. *Kill:* flakiness or <15% win. *Depends on H1* (parallel real-git bootstraps would worsen spawn storms).
- **H5 — Same treatment for the other git/fs-heavy packages.** *Claim:* apply H1/H3 to `commands/score_run`, `internal/config/materialize`, `commands/internal/lifecycle`, and batch/WAL-tune `internal/graphstore` (5k-node SQLite seeds, per-row fsync). *Evidence:* scout cross-package section. *Prediction:* per-package Windows wall-clock ↓. *Proof:* per-package before/after. *Kill:* per-package <20%.

## 3. Phased task breakdown

- **Phase 0 — Instrumentation foundation (measure-first; blocks all optimization).**
  Add `commands/workflow` benchmarks (`status`/`orient`/`checkpoint`/`advance`,
  state-ref read+write parametrized by task count, commit/mirror path); add a
  git-subprocess **counter seam** so tests+benches assert spawn counts; add pprof
  guidance; extend `scripts/perf/run-benchmarks.sh` + `docs/PERF_BUDGET.md` to
  cover workflow. **Capture + record baselines (Mac + Windows).** No optimization
  merges before this lands.
- **Phase 1 — Test-speed (H1, H4).** Shared template fixtures, seam migration of
  orchestration tests, then parallelization. Biggest wall-clock win; enables the
  timeout-bump revert.
- **Phase 2 — Runtime cheap wins (H2).** Dedupe git-summary probes; one-read
  checkpoint path.
- **Phase 3 — Runtime deep (H3).** Batch/in-process state-ref read+write+commit;
  reduce CAS re-reads. Highest runtime impact, highest effort/risk (CAS safety) —
  gated hard on behavior-preserving proof.
- **Phase 4 — Cross-package (H5).** score_run, config/materialize, lifecycle,
  graphstore.
- **Phase 5 — Regression gate + scaffold removal.** Perf budgets + CI bench/timing
  guard for workflow; revert the 1200s `-timeout` to a sane cap once Windows
  `-race` fits; document the perf contract.

## 4. Success criteria / exit

- `commands/workflow` Windows `-race` back under a sane per-package cap (target
  ≤600s, ideally far less) **with the 1200s scaffold reverted**.
- Every merged optimization has a recorded before/after proof (Windows headline).
- A workflow perf budget + CI guard exists so the win persists.
- Zero behavior/coverage/Sonar regressions across all phases.

## 5. Non-goals

- No feature/behavior changes; no public CLI/contract changes.
- Not chasing micro-optimizations without a measured hotspot (falsification gate).
- The `release-docs-refresh` cut is **paused** until this plan's Phase 1
  (test-speed) + Phase 5 (scaffold removal) make the suite fast again.

---

## 6. Repo-wide expansion — file-I/O + other production hot-spots

Scope broadens beyond `commands/workflow` to the file-I/O and CPU/alloc hot-spots
that run on nearly every `da` invocation or KG/dashboard op. Same methodology
(§1). Grounded by two read-only audits (CONFIRMED via file:line).

### 6.1 New baseline evidence (hot-spots, not yet benched)

**File-I/O (production):**
- `internal/config/agentsrc.go:1171-1196,1490-1708` — `LoadAgentsRC` = full-file ReadFile + JSON decode, called by add/refresh/remove/lint/sync/install; `GenerateAgentsRC` re-walks scoped dirs + stats markers. No cache. [CONFIRMED]
- `internal/platform/resources.go:101-192` — repeated `os.Stat`/`os.ReadDir` across project+global scopes on every projection/status/refresh. [CONFIRMED]
- `internal/platform/hooks.go:530-1244` — managed-hook read/compare/backup/rewrite loop per file. [CONFIRMED]
- `internal/dashboard/store/store.go:119-239` — snapshot ReadDir + parse every sidecar/iter-log on cache miss. [CONFIRMED]
- **Correctness-gate EXCLUSIONS (NOT perf targets):** `internal/config/local_source.go` CAS-ignore verify + `internal/agentslock/lockfile.go` RMW — PERF_BUDGET classifies both as correctness gates; per the `no-external-semantics-approximation-for-security-gate` lesson we do NOT shortcut them.

**CPU/alloc (unbenched surfaces):**
- `internal/graphstore/impact.go:25-100` + `sqlite.go:424-951` — impact BFS frontier growth, full-set cap/sort/delete, per-row scan + `decodeExtra` JSON unmarshal (5k-node scale bottleneck). [CONFIRMED]
- `internal/dashboard/store/store.go:146-579` — every request rebuilds `sessions()` (group/sort/project DTOs). [CONFIRMED]
- `internal/adapters/builtin/crg/postprocess.go:91-252` — flows/communities/risk traversals, append/sort/map churn. [CONFIRMED]
- `commands/workflow/hook_outcome.go:150-408` — whole-sidecar JSON validate+rewrite per append; `iter_log.go:325-723` — double YAML decode on v1 + token scans. [CONFIRMED]
- `internal/platform/pipeline_projection.go` + omp/cc emitters — builder + `fmt.Fprintf` + `MarshalIndent` churn. [INFERENCE]
- **Quick wins [CONFIRMED]:** `internal/graphstore/crg.go:504-511` + `internal/config/paths.go:156-160` compile `regexp.MustCompile` INSIDE the function (recompiled per call) — hoist to package scope.

**Bench-coverage gap:** the harness covers config/platform-projection/lifecycle only. **Zero** coverage for graphstore, dashboard/store, workflow runtime, adapters, pipeline emit.

### 6.2 New hypotheses

- **H6 — Command-scoped I/O caching/memoization.** *Claim:* memoize `LoadAgentsRC` parsed manifest per command; memo scoped resource listings; reuse the known render-hash in `hooks.go` to skip re-reads; finer dashboard snapshot invalidation. *Prediction:* eliminate N−1 redundant reads/parses per command → measurable ms + alloc cut. *Proof:* new agentsrc/resources/hook/dashboard benchmarks before/after. *Kill:* no measurable win, or any correctness change. CAS-ignore + agentslock stay untouched.
- **H7 — KG/graphstore query hot paths.** *Claim:* pre-size BFS frontier/result slices, short-circuit empty `extra` decode, batch/specialize row scans + edge-batch args. *Prediction:* ≥30% ns/op + alloc cut on impact-radius/search/stats at 5k nodes. *Proof:* new graphstore benchmarks at 5k before/after. *Kill:* <15% or behavior change.
- **H8 — Dashboard projection memoization.** *Claim:* memoize `sessions()` by root fingerprint; pre-size; avoid per-request re-group/sort. *Prediction:* repeated dashboard reads → O(1) on unchanged roots. *Proof:* new dashboard-store benchmarks (ListRuns/GetRun/ListIterations/Health) before/after. *Kill:* <20% on the warm path.
- **H9 — Zero-risk quick wins.** *Claim:* hoist per-call `regexp.MustCompile` to package scope; single-pass versioned decode (iter-log v1); record-granular hook-outcome validation. *Prediction:* removes per-call compile + double-decode; small but certain. *Proof:* micro-benchmarks. *Kill:* n/a (unambiguous correctness-preserving cleanups; still measured).

### 6.3 New phases (added to the DAG; all gated on the expanded Phase 0)

- **Phase 0 (EXPANDED)** — instrumentation adds benchmarks for the new surfaces (graphstore impact/query, dashboard/store, workflow iter-log + hook-outcome, CRG postprocess, pipeline emit, agentsrc load/generate, platform resources/hooks) + extends `scripts/perf` + `PERF_BUDGET.md`. Still gates all optimization.
- **Phase 6 (H6)** — command-scoped I/O caching/memoization (agentsrc, resources, hooks, dashboard snapshot).
- **Phase 7 (H7)** — KG/graphstore query hot paths (+ CRG postprocess).
- **Phase 8 (H8)** — dashboard projection memoization.
- **Phase 9 (H9)** — zero-risk quick wins (regex hoist, single-pass decode, incremental validation).
- **Phase 5 (BROADENED)** — regression gate + perf budgets now cover every new benchmark, not just workflow.

Scope note: the plan slug stays `workflow-perf-optimization` (historical), but its scope is now **repo-wide performance**. Phases 6-9 run parallel to 1-4 after Phase 0, each independently proven; the correctness-gate exclusions (§6.1) are firm non-goals.
