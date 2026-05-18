# ADR-0008: PR #10 duplication-density scope — accept waiver, defer cleanup to follow-up plans

**Status:** accepted
**Date:** 2026-05-04
**Owners:** dot-agents
**Related:** [ADR-0001](0001-adopt-architecture-decision-records.md) (ADR conventions); [ADR-0006](0006-da-rename-strategy.md) (precedent for one-shot scope decisions on large in-flight branches); [`sonarqube-pr10` plan findings.md](../../.agents/workflow/plans/sonarqube-pr10/findings.md) (sq1 audit; per-cluster numbers); [`sonarqube-pr10` plan](../../.agents/workflow/plans/sonarqube-pr10/) (operationalizes the other two failing conditions, sq2 reliability + sq3 hotspots)

## Context

PR #10 (`feature/PA-cursor-projectsync-phase1-extract-293f` →
`feature/workflow-auto-operator`) is the projectsync Phase 1 extraction
PR: 100 commits, **895 changed files**, +91,494 / −17,033. SonarCloud's
incremental analysis on the PR head reports the quality gate as
**ERROR** with three failing conditions:

| Condition                          | Threshold | Actual | Status |
|------------------------------------|-----------|--------|--------|
| `new_reliability_rating`           | ≤ 1 (A)   | 4 (D)  | ERROR  |
| `new_duplicated_lines_density`     | ≤ 3 %     | 4.7 %  | ERROR  |
| `new_security_hotspots_reviewed`   | ≥ 100 %   | 0.0 %  | ERROR  |

Reliability and hotspots are addressed in-PR by sq2 and sq3. This ADR
is scoped to the third failing condition only — duplication density —
because the other two have clear in-PR fix paths whereas duplication
does not.

The sq1 audit (findings.md §2) captured the duplication shape:

- **Total:** 40 duplicated files, **2,501 dup lines**, 145 blocks,
  PR new-code density **4.7 %** vs the 3 % threshold (gap ≈ **1.7 pp**).
- Roughly the top ~40 % of dup lines must be removed to drop under 3 %.
- Per-file density is concentrated in `*_test.go` fixtures: the top
  contributors include `internal/platform/resource_plan_test.go`
  (26.8 %, 239 dup lines), `commands/workflow/state_plan_test.go`
  (18.3 %, 184 dup lines), `commands/refresh_test.go` (32.1 %,
  114 dup lines), `commands/skills/promote_test.go` (28.3 %,
  108 dup lines), and so on.
- sq1 grouped the duplication into three clusters:
  - **Cluster D — test-fixture / table-test repetition.** The dominant
    cluster (~18+ `*_test.go` files): `setupTempProject(t)`,
    `writeFixtureRC(t, ...)`, parallel table-test scaffolding. Removing
    means extracting an `internal/testutil` helper package, touching
    dozens of files.
  - **Cluster E — production-code constant/literal duplication,
    partially de-duped.** Cross-module duplication in
    `internal/graphstore/{sqlite,postgres,mcp_server}.go`,
    `internal/platform/resource_plan.go`, several top-level
    `commands/*.go`, and the `commands/{agents,skills}` list/promote
    helpers. The branch already mass-fixed several sub-clusters
    (`commands/workflow`, `commands/kg`+`internal/graphstore`,
    `commands/agents`+`commands/import.go`, `internal/platform`); what
    remains is genuinely cross-module and needs design review before
    extraction.
  - **Cluster F — single-file outliers.**
    `commands/agents/list.go` (52.3 %) and `commands/skills/list.go`
    (51.7 %) are ~90 % identical and dominate per-file density. A
    single shared-renderer extraction would address both files.

The duplication is **branch-wide debt that predates and is orthogonal
to projectsync Phase 1.** It accumulated across the 324-commit history
that landed on this branch; very little of it is *introduced* by the
extraction work itself. Treating it as in-PR scope inflates an already
895-file diff and slows the merge of the actual extraction.

Three options were on the table for closing out the duplication
condition before merging PR #10:

- **(A) Fix duplication in this PR.** Land the testutil extraction and
  any production-helper extractions inside PR #10 itself. Drives
  density under 3 % and clears the gate condition outright.
- **(B) Accept duplication failure as a known waiver, with follow-up
  plans tracking cleanup. Merge PR #10 anyway after reviewer agrees.**
  PR #10 ships with the SonarCloud `new_duplicated_lines_density`
  condition still ERROR; the waiver is visible in SonarCloud's PR
  comment and acknowledged by the reviewer at merge time. Cleanup
  ships as separate, scoped plans.
- **(C) Block PR #10 merge until a duplication-cleanup PR ships
  first.** The cleanup lands on the base branch (or as a prerequisite
  PR), then PR #10's incremental diff no longer counts the
  pre-existing duplication as "new code."

## Decision

**Adopt option (B) — accept the duplication-density failure as a known
waiver with follow-up plans tracking cleanup; merge PR #10 anyway
after the reviewer affirms the waiver.**

Specifics:

- **The other two failing conditions are still in-PR.** sq2 brings
  `new_reliability_rating` to A (1); sq3 brings
  `new_security_hotspots_reviewed` to 100 %. PR #10 must clear those
  two before merge. The waiver applies **only** to
  `new_duplicated_lines_density`.
- **Reviewer affirmation is required before merge.** The PR description
  must call out the duplication waiver and link this ADR. The reviewer
  approves with explicit acknowledgement of the failed condition; that
  acknowledgement is the auditable record.
- **Follow-up plan IDs are reserved here so the debt is tracked, not
  forgotten:**
  - **`go-test-fixture-extraction`** — Cluster D. Extract an
    `internal/testutil` helper package
    (`setupTempProject`, `writeFixtureRC`, parallel table-test
    scaffolding) across the ~18+ `*_test.go` files identified in
    findings.md §2. Rough scope: ~2,500 lines, 18+ files. This is the
    bulk of the 4.7 % density — clearing Cluster D alone is expected
    to bring density under threshold on subsequent PRs.
  - **`production-code-helper-extraction`** — Cluster E. Cross-module
    list/render/promote duplication left over after the in-progress
    mass-fix. Needs design review before extraction (interfaces,
    package boundaries) and is therefore unsafe to do mechanically
    inside PR #10.
- **Cherry exception — Cluster F.** sq2 may include
  `commands/{agents,skills}/list.go` shared-renderer extraction as a
  scope-bump *if it is cheap* — the two files are ~90 % identical and
  one extraction commit closes both. This is a one-file-pair fix, not
  a mass-dedup, so it does not violate the option-(B) anti-scope. If
  sq2 surfaces any non-trivial complication (interface design, import
  cycle, behavioral edge), Cluster F drops back to
  `production-code-helper-extraction` instead.

This ADR is **decision-only**. It does not authorize any mass-deduping
inside PR #10 (that is option (A), explicitly rejected), nor does it
schedule the follow-up plans (those are tracked as planned but not
yet created in `.agents/workflow/plans/`).

## Consequences

**Easier:**

- PR #10 unblocks. The 895-file extraction PR is not held hostage by
  branch-wide duplication debt that predates it.
- Cleanup gets *scoped* plans (`go-test-fixture-extraction`,
  `production-code-helper-extraction`) instead of being smuggled into
  an already-large PR where review attention is diluted.
- The reviewer reviews the projectsync extraction on its own merits,
  not the extraction plus a 40-file testutil refactor.
- Follow-up plans can take the design-review time Cluster E genuinely
  needs (cross-module helper extraction is not mechanical).

**Harder:**

- PR #10 ships with a visibly-failing SonarCloud condition. Anyone
  reading the PR thread sees an ERROR gate and may assume the merge
  was sloppy. Mitigation: PR description links this ADR; the waiver is
  documented, not hidden.
- The follow-up plans must actually be created and worked. Without
  that, "deferred to follow-up plan" becomes "deferred forever," and
  duplication accumulates further. Mitigation: plan IDs are reserved
  in this ADR; the next planning cycle picks them up.
- The next PR off this branch tip inherits the same duplication if
  Cluster D / E aren't cleaned up first — meaning the same waiver
  argument has to be repeated. Mitigation: prioritize
  `go-test-fixture-extraction` immediately after PR #10 merges.

**New risks:**

- **Waiver fatigue.** If subsequent PRs also waive
  `new_duplicated_lines_density`, the gate condition becomes ignored
  by convention. Mitigation: each waiver requires a fresh ADR (or an
  explicit reference to this one with current numbers); waivers are
  not granted by precedent.
- **Cherry creep on Cluster F.** sq2 may try to extract the
  agents/skills list renderer and find it less mechanical than
  expected. Mitigation: anti-scope above — if Cluster F is non-trivial,
  it folds back to `production-code-helper-extraction` rather than
  expanding sq2.
- **Threshold drift.** SonarCloud may recalculate "new code" baselines
  between now and merge; the 4.7 % / 1.7 pp gap could move. The
  decision still holds (the *qualitative* shape — branch-wide debt,
  not extraction-introduced — is stable) but the verification step
  must re-pull live numbers, not trust the audit snapshot.

**Locked-in commitments:**

- The follow-up plan IDs (`go-test-fixture-extraction`,
  `production-code-helper-extraction`) are the canonical next homes
  for this debt. Renaming or splitting them later requires
  cross-references back to this ADR so the debt trail is intact.
- The decision is scoped to PR #10 specifically. A future PR with
  different duplication characteristics needs its own decision; this
  ADR is not a generic license to waive duplication-density failures.

## Alternatives considered

- **(A) Fix duplication in this PR.** Rejected. Cluster D alone
  requires `internal/testutil` extraction across ~18 files of test
  scaffolding; Cluster E requires cross-module design review. Both
  inside an already 895-file PR would inflate the diff well past
  reviewable size and tangle two unrelated concerns (projectsync
  extraction + duplication cleanup) in one merge. The sq1 audit notes
  option (A) is "only realistic if testutil extraction is tightly
  scoped (~40 files)" — and even that scope is independent of the
  extraction work the PR is actually about. The Cluster F cherry is
  the only mechanical sub-fix worth landing here; everything else is
  orthogonal.

- **(C) Block PR #10 merge until follow-up duplication-cleanup ships.**
  Rejected. Slowing projectsync Phase 1 by the time required to design
  and ship the testutil extraction (and possibly Cluster E too)
  costs more than the visibility cost of a documented waiver. The
  duplication is not a correctness or security risk — it is
  maintainability debt — and there is no scenario where merging the
  extraction first makes the cleanup harder. The sequencing argument
  for (C) (cleanup-first, then PR #10's incremental diff drops below
  the threshold) is technically sound but trades real shipped value
  for cosmetic gate-status.

- **Permanent waiver / threshold relaxation.** Not seriously
  considered: lowering the project-wide
  `new_duplicated_lines_density` threshold to mask this PR's failure
  would erode the gate's value for every future PR. Bounded ADR-level
  waiver is the correct shape.

## References

- [`sonarqube-pr10` plan findings.md](../../.agents/workflow/plans/sonarqube-pr10/findings.md)
  §2 — sq1 audit; per-cluster duplication numbers (40 files, 2,501
  dup lines, 4.7 % density, 1.7 pp gap), per-file top-10 contributors,
  cluster D/E/F classification, and the three-option recommendation
  this ADR adopts.
- The same findings.md §1 (reliability) and §3 (security hotspots) —
  the other two failing conditions, addressed in-PR by sq2 and sq3
  respectively. This ADR does not waive them.
- [ADR-0001](0001-adopt-architecture-decision-records.md) — Nygard
  format and ADR conventions.
- SonarCloud project `NikashPrakash_dot-agents` (org `npk-aorcha`),
  PR 10 — live quality-gate snapshot used as the source of truth for
  the numbers above. Counts drift on every push; verification step
  before merge must re-pull.
- [`gh pr view 10`](https://github.com/NikashPrakash/dot-agents/pull/10)
  — branch shape (100 commits, 895 changed files), referenced in
  context.
