# PR #10 SonarCloud Findings — sq1 Audit

**Audit run:** 2026-05-04
**PR:** [NikashPrakash/dot-agents#10](https://github.com/NikashPrakash/dot-agents/pull/10) (`feature/PA-cursor-projectsync-phase1-extract-293f` → `feature/workflow-auto-operator`)
**SonarCloud project:** `NikashPrakash_dot-agents` (org `npk-aorcha`)
**PR shape (gh pr view 10):** OPEN, mergeStateStatus UNSTABLE, 100 commits, 895 changed files, +91,494 / -17,033.
**Source of truth:** SonarCloud incremental analysis via MCP (`get_project_quality_gate_status`, `search_sonar_issues_in_projects`, `search_security_hotspots`, `search_duplicated_files`). Counts and gate values below are the live PR analysis, not local lint replay.

## Quality Gate (live)

Status: **ERROR**

| Condition                          | Threshold | Actual | Status |
|------------------------------------|-----------|--------|--------|
| `new_reliability_rating`           | <= 1 (A)  | 4 (D)  | ERROR  |
| `new_security_rating`              | <= 1 (A)  | 1 (A)  | OK     |
| `new_maintainability_rating`       | <= 1 (A)  | 1 (A)  | OK     |
| `new_duplicated_lines_density`     | <= 3 %    | 4.7 %  | ERROR  |
| `new_security_hotspots_reviewed`   | >= 100 %  | 0.0 %  | ERROR  |

> Drift note vs `sonarqube-pr10.plan.md`: that plan's "Findings" recorded `new_security_hotspots_reviewed` at 2.8 % after one prior review. The live value is **0.0 %** with **0 hotspots in REVIEWED status** — the prior review either was never persisted on this PR, was filed against a stale branch tip, or was reset when SonarCloud re-baselined after a force push / new commit. Treat 0.0 % as the canonical starting point for sq3.

---

## 1. `new_reliability_rating` — D (4) -> must be A (1)

- **Current:** 4 (D) | **Target:** 1 (A) | **Gap:** D -> A. SonarCloud derives the rating from the worst reliability issue on new code; reaching A requires every reliability issue on new code to be resolved or transitioned to a non-OPEN status.
- **Total open reliability issues on PR new code:** **47**

### Top contributing files (top 10, ranked by issue count)

| # | File | Issues | Dominant rule(s) | Severity mix |
|---|------|--------|------------------|--------------|
| 1 | `src/lib/commands/add.sh` | 25+ (sample cap) | `shelldre:S7688` (`[[` vs `[`) | MAJOR |
| 2 | `ports/typescript/src/platforms/codex.ts` | 6 | `typescript:S7781` (`replaceAll`) | MINOR |
| 3 | `ports/typescript/src/commands/workflow.ts` | 3 | `typescript:S7781` | MINOR |
| 4 | `ports/typescript/tests/agentsrc.test.ts` | 2 | `typescript:S2871` (sort compare) | CRITICAL |
| 5 | `ports/typescript/tests/commands.test.ts` | 1 | `typescript:S2871` | CRITICAL |
| 6 | `ports/typescript/src/core/config.ts` | 1 | `typescript:S2871` | CRITICAL |
| 7 | `ports/typescript/src/core/hooks.ts` | 1 | `typescript:S2871` | CRITICAL |
| 8 | `ports/typescript/src/core/mcp.ts` | 1 | `typescript:S2871` | CRITICAL |
| 9 | `ports/typescript/src/commands/skills.ts` | 1 | `typescript:S7781` | MINOR |
| 10 | `ports/typescript/src/commands/agents.ts` | 1 | `typescript:S7781` | MINOR |
| (also) | `src/lib/utils/json.sh`, `scripts/verify.sh` | 2 | `shelldre:S7688` | MAJOR |

### Cluster classification

| Cluster | Files | Classification | Rationale |
|---------|-------|----------------|-----------|
| **A. Bash `[[` vs `[` (`shelldre:S7688`)** | `src/lib/commands/add.sh`, `src/lib/utils/json.sh`, `scripts/verify.sh` | **fixable in this PR** | Mechanical sed substitution; ~30 sites; identical pattern to commits cb3c197 / f3ff375 already shipped. No behavioral risk. |
| **B. TS `String#replace` -> `replaceAll` (`typescript:S7781`)** | `ports/typescript/src/{commands/workflow,commands/skills,commands/agents,platforms/codex}.ts` | **fixable in this PR** | Mechanical 11-line edit. Coordinate with `typescript-port` plan owner so port-plan churn doesn't revert the fix. |
| **C. TS sort without compare (`typescript:S2871`)** | `ports/typescript/{src/core/{config,hooks,mcp},tests/{agentsrc,commands}}.ts` | **fixable in this PR** | CRITICAL severity drives the D rating. 6 sites; want `(a, b) => a.localeCompare(b)` or numeric compare. Coordinate with `typescript-port`. |

No false-positive / waiver candidates identified.

### Recommended sq2 scope

sq2 (reliability fixes) lands all three clusters. Suggested commit split: (1) Cluster A — shell `[[`-tests across the three files (one commit); (2) Cluster B + C — TS port reliability fixes (one commit, coordinated with `typescript-port` plan owner). Hard test: `new_reliability_rating == 1` on PR #10's analysis page after both pushes. Anti-scope: no `// NOSONAR` / `# noqa` suppressions; if a true false-positive surfaces, fold-back with the rule key + line and let sq1 re-classify.

---

## 2. `new_duplicated_lines_density` — 4.7 % -> must be <= 3 %

- **Current:** 4.7 % | **Target:** <= 3 % | **Gap:** ~1.7 pp. Total new duplicated lines = **2,501** across **40** files (live `search_duplicated_files`, page-all). Roughly the top ~40 % of dup lines must be removed to break under 3 %.

### Top contributing files (top 10, by duplicated lines)

| # | File | Dup lines | Blocks | Density |
|---|------|-----------|--------|---------|
| 1 | `internal/platform/resource_plan_test.go` | 239 | 15 | 26.8 % |
| 2 | `commands/workflow/state_plan_test.go` | 184 | 11 | 18.3 % |
| 3 | `commands/agents/agents_test.go` | 119 | 7 | 15.7 % |
| 4 | `internal/graphstore/sqlite.go` | 116 | 4 | 14.3 % |
| 5 | `commands/refresh_test.go` | 114 | 4 | 32.1 % |
| 6 | `commands/workflow/testutil_test.go` | 110 | 6 | 14.1 % |
| 7 | `commands/skills/promote_test.go` | 108 | 6 | 28.3 % |
| 8 | `internal/graphstore/postgres.go` | 94 | 2 | 10.4 % |
| 9 | `commands/kg/kg_test.go` | 91 | 11 | 3.8 % |
| 10 | `internal/graphstore/mcp_server.go` | 87 | 4 | 12.6 % |

(Branch-wide totals: 40 duplicated files, 2,501 dup lines, 145 blocks, PR new-code density 4.7 %.)

### Cluster classification

| Cluster | Files | Classification | Rationale |
|---------|-------|----------------|-----------|
| **D. Test-fixture / table-test repetition** | All `*_test.go` rows (resource_plan, state_plan, agents, refresh, testutil, skills/promote, kg, postgres, sqlite, mcp_server, mcp_settings, foldback, drift_sweep, iter_log, plan_task, rules, workflow_integration, import) | **branch-wide debt** | Dominant cluster: `setupTempProject(t)` / `writeFixtureRC(t, ...)` / parallel table-test scaffolding repeated across packages. Fix means extracting `internal/testutil` helpers — touches dozens of files, inflates PR diff. Defer to follow-up plan: **`go-test-fixture-extraction`**. |
| **E. Production-code constant/literal duplication, partially de-duped** | `internal/graphstore/{sqlite,postgres,mcp_server}.go`, `internal/platform/resource_plan.go`, `commands/{import_plugins,settings,mcp,skills,rules,explain,status,ux}.go`, `commands/agents/list.go`, `commands/skills/{list,promote}.go`, `commands/agents/promote.go` | **mostly branch-wide debt** | Source plan already mass-fixed `commands/workflow`, `commands/kg`+`internal/graphstore`, `commands/agents`+`commands/import.go`, `internal/platform`. Remaining is genuinely cross-module (e.g. `agents/list.go` and `skills/list.go` at 51-52 % share a near-identical renderer); needs design review before extraction. Defer or fold cluster F as a one-liner. |
| **F. Single-file outliers worth fixing here** | `commands/skills/list.go` (51.7 %), `commands/agents/list.go` (52.3 %) | **fixable in this PR** *(if cheap)* | These two files are 90 % identical and dominate per-file density. Extract one shared helper into `internal/agents/listrender.go` (or similar). Recommend sq4's ADR-0008 explicitly note this option. |

No false-positive candidates — duplication is real.

### Recommended sq4 (ADR-0008) scope

ADR records one of three options:

- **(A) Fix duplication in this PR.** Inflates 895-file diff; only realistic if testutil extraction is tightly scoped (~40 files). Higher merge risk.
- **(B) Accept duplication failure as a known waiver with a follow-up plan tracking cleanup; merge anyway** if reviewer agrees. **Recommended path:** debt is real but predates and is orthogonal to projectsync Phase 1. ADR records: follow-up plan id + rough scope (40 files, ~2,500 dup lines, primarily `*_test.go`).
- **(C) Block PR #10 merge until follow-up duplication-cleanup ships.** Conservative; slows merge significantly.

ADR must include the per-cluster numbers above. Anti-scope: sq4 does not start mass-deduping; it is the *decision* — work (option A) or follow-up plan (B/C) is its own plan. If (B)/(C), cluster F's `commands/{agents,skills}/list.go` shared-renderer extraction is the cheapest in-PR cherry; sq2 can pick it up as a 6th sub-task.

### Decision (sq4) — recorded 2026-05-04 in [ADR-0008](../../../../docs/adr/0008-pr10-duplication-scope.md)

**Chosen path: option (B) — accept duplication failure as a known waiver with follow-up plans tracking cleanup; merge PR #10 anyway after reviewer affirms the waiver.**

- Reliability (§1) and security hotspots (§3) are still in-PR via sq2 and sq3. The waiver applies *only* to `new_duplicated_lines_density`.
- Follow-up plan IDs reserved for the deferred work:
  - **`go-test-fixture-extraction`** — Cluster D test-fixture debt (~2,500 lines across 18+ `*_test.go` files; the bulk of the 4.7 % density).
  - **`production-code-helper-extraction`** — Cluster E cross-module list/render/promote duplication; needs design review before extraction.
- Cherry exception: sq2 *may* include Cluster F (`commands/{agents,skills}/list.go` shared-renderer extraction) as a one-commit scope-bump if cheap; if any non-trivial complication surfaces it folds back into `production-code-helper-extraction` instead.
- Anti-scope: this decision does **not** authorize mass-deduping inside PR #10. ADR-0008 is decision-only; the follow-up plans are tracked but not yet created.
- Reviewer must affirm the waiver at merge time (PR description links ADR-0008).

---

## 3. `new_security_hotspots_reviewed` — 0.0 % -> must be 100 %

- **Current:** 0.0 % | **Target:** 100 % | **Gap:** 35 hotspots in TO_REVIEW, 0 REVIEWED. Every hotspot must be transitioned to REVIEWED with a disposition (SAFE / FIXED / ACKNOWLEDGED).

### Hotspot inventory by file (all 35)

| # | File | Hotspots | Rule | Vuln. probability |
|---|------|----------|------|-------------------|
| 1 | `commands/sync/commit.go` | 5 | `go:S4036` (PATH lookup) | LOW |
| 2 | `commands/sync/helpers.go` | 4 | `go:S4036` | LOW |
| 3 | `commands/sync/init.go` | 4 | `go:S4036` | LOW |
| 4 | `commands/sync/push.go` | 4 | `go:S4036` | LOW |
| 5 | `commands/sync/log.go` | 1 | `go:S4036` | LOW |
| 6 | `commands/sync/pull.go` | 1 | `go:S4036` | LOW |
| 7 | `commands/workflow/state.go` | 2 | `go:S4036` | LOW |
| 8 | `commands/workflow/plan_task.go` | 2 | `go:S4036` | LOW |
| 9 | `commands/workflow/delegation.go` | 1 | `go:S4036` | LOW |
| 10 | `commands/workflow/iter_log.go` | 1 | `go:S4036` | LOW |
| 11 | `commands/kg/sync_code_warm_link.go` | 1 | `go:S4036` | LOW |
| 12 | `internal/graphstore/crg.go` | 1 | `go:S4036` | LOW |
| 13 | `internal/platform/cursor.go` | 1 | `go:S4036` | LOW |
| 14 | `ports/typescript/src/commands/workflow.ts` | 1 | `typescript:S5852` (regex DoS) | MEDIUM |
| 15 | `ports/typescript/src/commands/agents.ts` | 1 | `typescript:S5852` | MEDIUM |
| 16 | `ports/typescript/src/commands/skills.ts` | 1 | `typescript:S5852` | MEDIUM |
| 17 | `ports/typescript/tests/commands.test.ts` | 3 | `typescript:S5443` (publicly writable dir) | LOW |
| 18 | `ports/typescript/tests/kg.test.ts` | 1 | `typescript:S5443` | LOW |

Counts: **27 × `go:S4036` (PATH)**, **3 × `typescript:S5852` (regex DoS)**, **4 × `typescript:S5443` (publicly writable dir, in tests)**, **1 × `typescript:S5852` already in workflow.ts above** — total **35**.

### Cluster classification

| Cluster | Files / hotspots | Classification | Rationale |
|---------|------------------|----------------|-----------|
| **G. Go `exec.LookPath` PATH hotspots (`go:S4036`)** | 27 hotspots across `commands/sync/*.go`, `commands/workflow/*.go`, `commands/kg/*.go`, `internal/graphstore/crg.go`, `internal/platform/cursor.go` | **needs review in this PR** (likely REVIEWED-SAFE) | `exec.LookPath("git")` / `LookPath("crg")` etc. dot-agents runs on dev workstations (not privileged); looked-up tool name is hard-coded. Disposition: REVIEWED-SAFE with one-line per-hotspot rationale ("hard-coded tool name; user-shell context; no escalation surface"). sq3 must inspect each line; if any uses a config-derived variable name, escalate to FIXED via allowlist refactor. |
| **H. TS regex backtracking (`typescript:S5852`)** | 3 hotspots in `ports/typescript/src/commands/{workflow,agents,skills}.ts` | **needs review in this PR** (likely FIXED) | Per-regex inspection. If user-controllable input, refactor to non-backtracking form. If bounded by repo file-system patterns, mark SAFE. Coordinate with `typescript-port`. |
| **I. TS publicly-writable-dir hotspots in tests (`typescript:S5443`)** | 4 hotspots in `ports/typescript/tests/{commands,kg}.test.ts` | **needs review in this PR** (REVIEWED-SAFE) | Tests using `os.tmpdir()` / `/tmp/...` for fixture scaffolding. Bounded to test process with random subdirs. Per-hotspot rationale required, disposition uniform. |

No false-positive candidates that warrant FALSE_POSITIVE status.

### Recommended sq3 scope

Reviews **all 35** hotspots. Practical batching:

1. **Batch 1 — Cluster G (Go LookPath, 27 hotspots).** Inspect each call site, confirm hard-coded tool name, `change_security_hotspot_status` to REVIEWED-SAFE with one-line rationale per hotspot. ~30 min.
2. **Batch 2 — Cluster I (TS test publicly-writable-dir, 4 hotspots).** REVIEWED-SAFE with test-isolation rationale. ~5 min.
3. **Batch 3 — Cluster H (TS regex DoS, 3 hotspots).** Per-regex judgment: SAFE if bounded, FIXED with refactor commit if unbounded. ~15 min.

Hard test: `new_security_hotspots_reviewed == 100 %` after sweep. Anti-scope: no mass-mark; each `change_security_hotspot_status` carries a specific `comment` field — that comment is the audit trail.

---

## Recommended split summary: in-PR vs follow-up plans

| Task | Cluster(s) | Outcome | Effort |
|------|------------|---------|--------|
| **sq2** | A (shell `[[`), B (TS `replaceAll`), C (TS sort compare) | `new_reliability_rating` -> A (1) | small, mechanical |
| **sq3** | G (Go LookPath), H (TS regex), I (TS test fixture dirs) | `new_security_hotspots_reviewed` -> 100 % | medium, per-hotspot |
| **sq4** | F (high-density list.go pair) decision + D / E debt waiver | ADR-0008 records option chosen | small (decision-quality) |

| Follow-up plan (proposed) | Reason |
|----------------------------|--------|
| **`go-test-fixture-extraction`** (cluster D) | 2,500-line test duplication is the bulk of the duplication-density failure; deferred per sq4 ADR option (B). Extract `internal/testutil` once across the 18+ `*_test.go` files. |
| **`production-code-helper-extraction`** (cluster E) | Cross-module list/render/promote duplication left after the in-progress mass-fix; needs design review. Defer per sq4 ADR option (B). Optionally, sq2 can include cluster F cherry as a tiny scope-bump. |
| **(no plan)** for clusters A/B/C/G/H/I | Handled in PR #10 by sq2 / sq3. |

## Verification — re-confirm before sq2/sq3/sq4 start

```text
# Live gate snapshot
mcp__sonarqube__get_project_quality_gate_status \
  --projectKey NikashPrakash_dot-agents --pullRequest 10

# Reliability inventory (47 issues currently)
mcp__sonarqube__search_sonar_issues_in_projects \
  --projects NikashPrakash_dot-agents --pullRequestId 10 \
  --impactSoftwareQualities RELIABILITY --issueStatuses OPEN,CONFIRMED --ps 500

# Hotspots inventory (35 TO_REVIEW currently)
mcp__sonarqube__search_security_hotspots \
  --projectKey NikashPrakash_dot-agents --pullRequest 10 --status TO_REVIEW --ps 500

# Duplication inventory (40 files, 2501 lines currently)
mcp__sonarqube__search_duplicated_files \
  --projectKey NikashPrakash_dot-agents --pullRequest 10
```

Counts drift on every push to PR head; re-run the live snapshot at the start of each downstream task.

## Anti-scope reminder for downstream tasks

- **sq2:** no `// NOSONAR`, no rule severity changes. Fix the smell, not the signal.
- **sq3:** no mass status changes without per-hotspot rationale visible in SonarCloud's status-change comment.
- **sq4:** no "decide later" ADR. Each option's tradeoffs explicit, one option chosen, follow-up plan id recorded if option (B) or (C).
