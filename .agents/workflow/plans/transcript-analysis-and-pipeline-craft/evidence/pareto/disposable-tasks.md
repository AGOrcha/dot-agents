# Disposable-task substrate — live-wave contrasts (C0-C6)

Resolves fold-back `disposable-task-substrate` (red-team `reviews/red-team-premortem-2026-07-12.md`
RT-10 / pre-mortem F2: an undefined substrate was the plan's most dangerous failure mode — a
CI-clean frontier measured on unrepresentative tasks). Defines what qualifies as a disposable
task, the candidate pool, and the cell-matching procedure. Live runs remain user-gated (cost).

## Definition

A **disposable task** is a bounded implementation task that can be executed repeatedly through
the full-loop inner pipeline (executor → verifiers → lenses → gate) such that:

1. **Snapshot-identical re-runs (rubric step 4).** The task starts from a frozen `(repo SHA,
   bundle, profile)` triple; every repeat and every contrast arm launches from the same triple.
   Repo state is provided by a throwaway worktree at the pinned SHA.
2. **Write-scope-safe / zero canonical effect.** Work happens in an isolated worktree; nothing
   is merged, pushed, or recorded into canonical PLAN/TASKS state (runs use a scratch plan
   namespace). The delivery gate stops before push.
3. **Ground-truth referenced.** The task has an external correctness reference that does NOT
   depend on the swapped stage: a landed historical implementation (diffable) and/or a frozen
   verifier suite. This keeps the accuracy axis instrument-independent per RT-9's caveat.
4. **Cell-representative.** The task's measured profile (cache_regime, generated-output
   fraction, duration) falls inside the target historical cell's bounds — checked empirically
   in C0/wave-0, not assumed (see procedure below).

## Candidate pool (two tiers)

### Tier A — historical-task replays (frontier rows; representative)

Re-implement an ALREADY-LANDED, formerly-delegated task from its original bundle, in a worktree
pinned to the task's original base SHA. The landed diff + its verifier suite are the ground
truth; the output is discarded. Sources: `.agents/history/<plan>/` (archived TASKS.yaml +
delegate merge-back archives). Selection at wave prep resolves exact task ids from history;
candidate plans by target task class:

| target cell (task_class × cache/output profile) | candidate source plans (archived, verified-complete) |
|---|---|
| `impl-slice` / go-cli, medium-length, test-bearing | `global-flag-compliance` (e.g. `implement-command-fixes`), `error-message-compliance`, `command-surface-decomposition` |
| `impl-slice` / go-cli, short-mechanical | `coverage-95-staged` / `coverage-gate-per-file` allowlist-shrink slices, `binary-rename-da-sweep` |
| `docs` / short, low-output | `config-relevance-profiles` docs slices; docs tasks from `agentsrc-local-schema` |
| `review` (C6) | re-review of a landed PR diff from `adhoc-reviews` / any Tier-A replay's output |

Selection rules: task must have (a) an archived bundle or reconstructable contract, (b) its
base SHA still resolvable, (c) a verifier suite that passes on the landed implementation
(sanity: ground truth verifies green before any contrast run), (d) write_scope untouched by
later refactors severe enough to break replay (check `git log --oneline <scope paths>` since
base SHA; >20 commits touching the scope = disqualify).

### Tier B — synthetic calibration controls (NEVER frontier rows)

D12 hop-chain-style fresh-context trials and copilot-smoke-style trivial tasks, per **O13**:
calibration only (fixed-tax floor, scorer sanity, harness plumbing). They sit in the
short/cache-cold regime where H4 says fixed tax dominates — structurally unlike the workflow
loop — so they are hard-excluded from frontier cells and used to calibrate the axes.

**Trap-test transfer check (fold-back `lens-transfer-trap-test`, 2026-07-13).** When a
contrast arm ports a stage prompt (lens/rubric/skill text) to a SECONDARY-tier model, the
instructions — not the model — are the capability carrier, and "pasted" is not "used." Before
that arm's frontier rows count, one Tier-B planted-defect task verifies the transfer: a task
seeded with a known defect the stage prompt's procedure should catch (analogue of the
compound-discount trap, Part K.1). Pass = the ported stage flags the planted defect; fail =
bounded regeneration of the vague prompt section (not a rewrite), then re-test. Calibration
only per O13 — the trap row never enters a frontier cell; it gates instrument trust the same
way C0 gates repeat-variance.

## Cell-matching procedure (empirical, not assumed)

1. At wave prep, shortlist ≥2 Tier-A candidates per target cell from the table above.
2. **C0 doubles as the profiler:** its n≥5 baseline repeats measure each candidate's realized
   cache_regime, generated-output fraction, duration, and $ per run.
3. A candidate is confirmed for a cell iff its C0-measured profile lands inside the cell's
   frozen bins (`preregistration.md` §2: cache-hot ≥85% / warm 50-84% / cold <50%) and its
   generated-output fraction is within the historical cell's IQR.
4. Mismatched candidates are re-binned to the cell they actually measured into, or dropped —
   never force-fit. If no candidate lands in a preregistered target cell, that cell's contrasts
   are reported as unexecutable-as-designed (honest gap), not run on a substitute regime.

## Explicit non-goals

- No live production tasks (pre-existing queue debt stays out of the experiment).
- No task authored specifically to make a contrast "come out" (fence per elvissun LFD:
  candidates are selected by pre-stated criteria from history, before any contrast runs).
- Tier-B never feeds a frontier cell (O13).
