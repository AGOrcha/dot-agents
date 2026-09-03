---
id: loud-atomic-error-handling
lead: dot-agents-swallowed-errors-loud-atomic
decision: MERGE-into-full-loop-orchestration-runtime (+ close lead as delivered)
produced_by: kg-ideate Phase 1 (kg-brief) — no spec/plan scaffold warranted
date: 2026-07-22
---

# Recommendation: swallowed-errors loud + atomic is DELIVERED — route one residual, close the lead

## TARGET decision

**MERGE-into-`full-loop-orchestration-runtime`** for a single residual gap, **and close the
ideation-inbox lead as DELIVERED.**

This lead's concern is **not** new work. It was fully specced, sliced, and shipped by the
archived plan **`swallowed-errors-loud-atomic`** (`.agents/history/swallowed-errors-loud-atomic/`,
`status: archived`, all 16 tasks `completed`), born from the same #350 fold-back and the same
312-site audit that IS this lead's body. Per the `single-source-of-truth-across-specs-and-plans`
lesson and the shared Contract, a NEW competing spec+plan for an already-owned-and-delivered
concern is prohibited — so no `design.md` / `plan-draft.md` is written.

The Phase-1 brief found exactly **one** audit finding classified as remediation-worthy
(**should-be-ATOMIC**) that was **never assigned to any of the 11 slices** and **still swallows in
current code**: the `internal/journal/` durable-snapshot + recovery package. It is routed below as
a single tracked task into the active plan that owns the loop-orchestration runtime and its
PreCompact recovery, `full-loop-orchestration-runtime`.

## Phase 1 — kg-brief (grounded)

- **KG:** `da kg query --intent repo_context "swallowed errors loud atomic snapshot journal"` →
  *No results found.* KG is thin on this topic (expected; kg-brief fallback to corpus + lessons +
  history applies).
- **History (the decisive grounding):** `.agents/history/swallowed-errors-loud-atomic/` is the
  archived delivery of this exact concern — `success_criteria`: "Each of the 11 remediation slices
  lands as a stacked PR … top-3 blast-radius risks fixed with survival/rollback tests." **16/16
  tasks `completed`** (se0, se1, se2, se3, se4, se5, se5-add-errors, se6, se7, se8, se9-p2-loud-batch,
  se9-periph, se9-commands, se9-platform-safe, se9-platform-shared, se9-import, se10). I did **not**
  trust the `completed`/`archived` status (`stale-plan-status-vs-reality` lesson) — I verified the
  fixes against current code (see evidence below).
- **Lessons scanned (`.agents/lessons/index.md`), directly on-point:**
  - `error-audit-whole-command-sequence` — *born from this very work* (se5/#356 missed `git add -A`
    `.Run()` + push.go's void `stageAndCommit`; fixed in follow-up se5-add-errors/#367). Confirms the
    audit's completeness discipline and that the sync slice is fully closed.
  - `leverage-cross-platform-fs-helpers` — route OS-divergent fs behavior through `internal/fsops`
    (mutations) + `internal/testutil.MakeDir/FileUnreadable` (forced access errors); never branch on
    `runtime.GOOS` nor use the NTFS-ignored `0o000` trick. This is the mandated test pattern for the
    residual fix.
  - `hermetic-home-for-state-resolving-tests` — any test resolving `$HOME`/state-dir must
    `t.Setenv` an isolated `t.TempDir()`; applies to snapshot-collector tests (they resolve repo
    state dirs).
  - `single-source-of-truth-across-specs-and-plans` — the constraint forcing MERGE, not a new spec.
- **Existing-plan overlap check:** no *active* plan owns "swallowed errors." The owning plan is
  archived/complete. `full-loop-orchestration-runtime` (active) owns the loop runtime + PreCompact
  snapshot/recovery — the natural home for the `internal/journal/` residual (`grep -c internal/journal`
  in the archived TASKS.yaml = **0**; the package was audited but never sliced).
- **Cross-ref — `error-message-compliance` / `docs/ERROR_MESSAGE_CONTRACT.md`:** a **distinct,
  complementary** concern. That contract governs the *shape* of a human-facing failure (primary
  message, `ErrorWithHints`, `UsageError`, finite-domain enumeration). This concern governs *whether
  a real I/O error surfaces at all* (loud vs swallowed) and *whether the enclosing op stays atomic*.
  No remediation overlap; the residual fix's surfaced errors should follow the message contract when
  they reach a CLI boundary.

## Evidence: delivery verified in current code (`fix/ci-sonar-and-link`)

| Slice / top-risk | Site | Current-code proof |
|---|---|---|
| se0 (base helper) | `internal/fsops/allow_missing.go` | `ReadFileAllowMissing`/`ReadDirAllowMissing`/`StatAllowMissing` present with the `IsNotExist→(zero,false,nil)` / real-err→`(zero,false,err)` contract |
| se3 / #350 trigger | `internal/config/agentsrc.go` `GenerateAgentsRC` | now collects `errs` from all detectors and `errors.Join(errs...) != nil → return nil, joined` (fail-or-full); `isDirEntry` returns `(bool,error)`; `warnOnCorruptGitConfig` emits the events warning for corrupt `.git/config` |
| se1 / top-risk #2 | `internal/config/paths.go` + `commands/root.go` | `PreflightUserHome()` (hard-fails `"cannot resolve your home directory … set $HOME or $AGENTS_HOME"` unless `AGENTS_HOME` set), wired in `root.PersistentPreRunE` |
| se2 / top-risk #1 | `internal/platform/hooks.go` `resolveHookSpecInScopes` | uses `fsops.StatAllowMissing`; a real Stat error `return nil, err` (aborts the search) instead of being read as "candidate absent" — docstring codifies the loud+atomic contract feeding `emitPreferredHookFile` |
| se6 / top-risk #3 | `internal/graphstore/sqlite.go`, `crg.go` | `encodeExtra` now returns error and is checked at all 4 `StoreFileNodesEdges` sites; `crg.go` `ReadNodes`/`ReadEdges` discriminate `os.IsNotExist` (return `nil,nil`) vs real Stat error (`return nil, fmt.Errorf("stat CRG db: %w", err)`) |
| se5 (+follow-up) | `commands/sync/push.go`, `commit.go` | `stageAndCommit` now returns error; only "nothing to commit" is non-fatal — confirmed by `error-audit-whole-command-sequence` lesson / #367 |

## STILL-OPEN swallow-sites (cited to current code) — the residual

Package **`internal/journal/`** — audited (fold-back body §"internal/kg/lockfile + journal + …")
as **should-be-ATOMIC / should-be-LOUD**, but present in **no** slice write-scope and unchanged:

1. **`internal/journal/snapshot.go` — durable PreCompact snapshot collectors (should-be-ATOMIC):**
   - `capturePlans` (`snapshot.go:244-249`): `entries, err := os.ReadDir(base); if err != nil { return []PlanState{} }` — a permission/I/O error is indistinguishable from "no plans."
   - `captureDelegations` (`snapshot.go:422-427`): same `os.ReadDir` → `return []DelegationState{}`.
   - `capturePendingMergeBacks` (`snapshot.go:454-459`): same `os.ReadDir` → `return []string{}`.
   - `readYAML` (`snapshot.go:329-335`): `data, err := os.ReadFile(path); if err != nil { return false }` then `yaml.Unmarshal(...) == nil` — **missing, corrupt, AND permission-denied all collapse to
     `false`**; a `PLAN.yaml`/delegation contract that exists but is corrupt (concurrent-write
     collision) silently vanishes from the durable snapshot. `buildSnapshot`→`writeSnapshot` has no
     error return by design, so a real error silently persists a **zero-plans / zero-delegations**
     snapshot with no signal anywhere.
2. **`internal/journal/recover.go` — crash-recovery reads (should-be-LOUD):**
   - `RecoveryView`/`loadSnapshotForRecovery` (`recover.go:288-291`): any `snapErr` →
     `snap = SnapshotState{}`; a **corrupt** `snapshot.json` reads as "no snapshot yet." (The raw
     error is preserved in `recoveryNotes`/`Notes`, which softens but does not add the
     `IsNotExist` discrimination.)
   - `eventTransition` (`recover.go:461-468`): `json.Unmarshal` of `Input`/`Observed` on a
     *registered* transition → `return transition{}, false`, silently dropped and **uncounted**
     (unlike the outer-envelope failure at `:352`, which increments `skipped`) — can silently lose a
     real task-status transition during recovery.

## Drafted task to MERGE into `full-loop-orchestration-runtime`

```yaml
- id: se11-journal-snapshot-loud-atomic
  title: 'internal/journal snapshot + recovery: loud + atomic (residual of swallowed-errors-loud-atomic)'
  status: pending
  depends_on: []          # internal/fsops helpers already merged (se0)
  blocks: []
  owner: dot-agents
  write_scope:
    - internal/journal/snapshot.go
    - internal/journal/snapshot_test.go
    - internal/journal/recover.go
    - internal/journal/recover_test.go
  verification_required: true
  notes: |-
    Sole residual of the archived swallowed-errors-loud-atomic plan: internal/journal
    was audited (should-be-ATOMIC/LOUD) but in NO slice write-scope. Snapshot side
    (ATOMIC): capturePlans/captureDelegations/capturePendingMergeBacks os.ReadDir and
    readYAML os.ReadFile+yaml.Unmarshal must distinguish os.IsNotExist (legit-absent,
    keep current empty-slice/false) from a real error, and thread that real error up so
    buildSnapshot/writeSnapshot never persists a zero-plans/zero-delegations snapshot on
    a permission/I/O/corrupt fault (give the collectors + buildSnapshot an error channel,
    or at minimum a durable degraded-mode marker so a partial snapshot is never mistaken
    for a settled one). Recovery side (LOUD): loadSnapshotForRecovery must discriminate
    IsNotExist vs corrupt snapshot.json (corrupt is not "no snapshot yet"); eventTransition
    must COUNT a dropped malformed registered-transition payload (mirror the :352 skipped++)
    rather than silently returning false. Route surfaced CLI-boundary errors through the
    ERROR_MESSAGE_CONTRACT helpers. Use internal/fsops.{ReadFileAllowMissing,ReadDirAllowMissing}
    + internal/testutil.MakeDir/FileUnreadable (per leverage-cross-platform-fs-helpers;
    NEVER runtime.GOOS branch / 0o000). Tests hermetic per hermetic-home-for-state-resolving-tests.
    No AI-attribution trailers/footers.
  acceptance: |-
    Three-way table per collector (absent → unchanged empty result; permission-denied →
    real error surfaced/threaded, NOT silent-empty; present+valid → unchanged). ATOMIC
    proof: with a repo whose plans dir is unreadable, assert buildSnapshot does NOT emit a
    settled snapshot claiming zero plans (error threaded or degraded-marker set). LOUD proof:
    a corrupt snapshot.json is reported distinctly from "no snapshot yet"; a malformed
    payload on a registered transition increments the skipped/uncounted-loss accounting.
```

## Done-criteria — concrete, testable loud+atomic contract (governs the residual and codifies the delivered class)

A file-reading detector/collector or a write/mutation path satisfies the contract iff:

1. **ENOENT is the ONLY silent-degrade branch.** Every `os.ReadFile`/`ReadDir`/`Stat`/`Lstat`/
   `Open` and every `json`/`yaml.Unmarshal` on a file that may legitimately be absent routes
   through `internal/fsops.{ReadFileAllowMissing,ReadDirAllowMissing,StatAllowMissing}` (or an
   explicit `os.IsNotExist(err)` / `errors.Is` check). `os.IsNotExist` → the documented empty/zero/
   `false` degrade; **any other error (permission, I/O, corrupt parse) is returned or threaded to a
   caller that can act on it**, never conflated with absence. *Test:* three-way table (absent →
   unchanged; permission-denied → error surfaced; present+valid → unchanged), reusing
   `internal/testutil.MakeDir/FileUnreadable` (no `runtime.GOOS` branch, no `0o000`).
2. **Loud:** the real-error branch produces a signal a caller/user can observe — a returned/wrapped
   error, or a structured `internal/events` warning where the spec mandates a zero-error fallback
   (e.g. `DeriveRepoIDFromGit` §5.3). No swallow-and-degrade with zero signal.
3. **Atomic (fail-or-full):** if a real error occurs mid-operation, the enclosing op must **not
   persist a degraded/partial artifact as if complete** and must **not perform a destructive step
   gated on the incomplete result**. *Test — the mandatory side-effect assertion:* on a real error,
   the observable side effect is **absent** (file untouched / transaction rolled back / existing
   links or entries still present / no settled-but-empty snapshot written) — not merely "an error
   was returned."
4. **No regression of legitimate absence:** the absent-path case is asserted unchanged in the same
   table, proving the discrimination did not turn a benign missing file into a hard failure.

## 3-line summary

- **Brief found:** this lead's concern was fully delivered by the archived `swallowed-errors-loud-atomic`
  plan (16/16 tasks; se0 fsops helpers, se1 home preflight, se2 platform prune-guard, se3 agentsrc
  fail-or-full, se6 graphstore atomicity, se5 sync all verified present in current code); KG thin,
  grounding from history + lessons + audit body.
- **Decision:** do NOT create a competing spec/plan (single-source lesson + Contract); MERGE the one
  uncovered residual — the never-sliced `internal/journal/` snapshot+recovery swallow family — into
  active `full-loop-orchestration-runtime` as task `se11-journal-snapshot-loud-atomic`, and close the
  ideation-inbox lead as delivered.
- **Key open question:** should the snapshot collectors gain a real error channel threaded through
  `buildSnapshot`/`writeSnapshot` (true fail-or-full), or is a durable *degraded-mode marker* on the
  snapshot sufficient given the PreCompact writer's intentionally never-fails design? — the one
  design call the `se11` task must resolve.
