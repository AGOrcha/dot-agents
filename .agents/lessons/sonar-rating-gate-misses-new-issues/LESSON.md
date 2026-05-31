# SonarCloud rating-based quality gate misses new CRITICAL issues

## Pattern

SonarCloud's built-in "Sonar way" quality gate uses rating-based pass conditions: `new_maintainability_rating ≤ A`, `new_coverage ≥ 80%`, duplication < 3%, etc. A PR that introduces multiple CRITICAL code smells (e.g., `S3776` cognitive complexity, `S1192` duplicated literals) on a meaningfully-sized new-code surface can still pass the gate with a "green" badge because:

1. Each individual CRITICAL issue has low absolute weight against a full-codebase maintainability rating.
2. A single "A" maintainability rating aggregates hundreds of non-issues with a few smells.
3. The gate checks the *aggregate rating*, not the *issue count*.

Result: "Quality Gate Passed ✓" can hide new CRITICAL issues. Bit us on 7 slate PRs (#172, #176, #177, #178, #179, #181 + merged #174) where each shipped new S3776 / S1192 / S108 violations on gates that showed green.

Symptom: PR shows "Quality Gate Passed" in GitHub checks, but `gh api repos/owner/repo/pulls/N/reviews` or SonarCloud issue list reveals new CRITICAL/BLOCKER issues on the code.

## Root cause

SonarCloud's *built-in gates are not editable*. The "Sonar way" gate hardcodes rating-aggregation logic that prioritizes code smell density (how bad your code is overall) over issue *count* (how many new issues did this PR add). The gate is designed for projects that accept some percentage of code debt; it's unsuitable for projects with a zero-new-issues policy.

The Sonar QG API documents the gate conditions but does not expose a UI to edit them on built-in gates. **Custom gates are required to enforce zero-new-issues.**

Encountered on PR #174–#181 (feature/slate-prs): gate showed green despite new violations; discovered the "Sonar way" gate does not have an explicit `new_violations > 0 → FAIL` condition.

## Rule

1. **Server-side enforcement:** Create a custom quality gate (if not already created):
   - Copy the default "Sonar way" gate via `POST /api/qualitygates/copy` with `name: dot-agents-strict`.
   - Add the explicit condition: `POST /api/qualitygates/create_condition` with `metric: new_violations`, `op: GT`, `error: 0` (fail if any new issue).
   - Optionally add `metric: new_coverage`, `op: LT`, `error: 95` (bump project coverage floor from default 80% to 95%).
   - Assign the gate to the project in the SonarCloud UI (`Organization Settings > Quality Gates > Default`).

2. **Agent-side belt-and-suspenders:** The `pr-ci-watch` verifier (per `[[verifier-owns-ci-watch-shift-left]]`) **independently checks new-issue count**:
   ```
   search_sonar_issues_in_projects(
     projects=[project_key],
     pullRequestId=<n>,
     issueStatuses=["OPEN", "CONFIRMED"]
   )
   if len(issues) > 0:
     escalate("new_violations > 0 — PR not ready per new_issues=0 gate")
   ```
   The verifier's count check becomes the ground truth on the agent side, complementing the server gate. This catches:
   - Gate assignment not yet applied.
   - Gate misconfigured or accidentally disabled.
   - Transient SonarCloud API delays propagating the gate to PR context.

3. **Never suppress via allowlist.** The gate condition is non-negotiable per `[[no-lazy-allowlist-tech-debt]]`. If an issue cannot be auto-fixed (e.g., a legitimate design trade-off), the PR should not be in the queue yet; escalate to the author for resolution, or defer the entire task.

## How to apply

**For maintenance (one-time setup):**
1. Log into SonarCloud as an org admin.
2. Create the `dot-agents-strict` custom gate: `Organization Settings > Quality Gates > Copy from "Sonar way"` → name it `dot-agents-strict` → add `new_violations > 0 → FAIL` condition.
3. Set it as the project default: `Organization Settings > Projects > <project> > Edit > Quality Gate = "dot-agents-strict"`.

**For reviewers (every PR):**
1. When "Quality Gate Passed" appears in GitHub but you suspect new smells: query via sonar MCP:
   ```
   search_sonar_issues_in_projects(
     projects=[project_key],
     pullRequestId=<n>,
     issueStatuses=["OPEN", "CONFIRMED"]
   )
   ```
2. If count > 0: the gate badge is stale or misconfigured. Escalate as a blocker.

**For `pr-ci-watch` implementation:**
1. In the pass-criteria check, place the new-issues count check BEFORE the gate-green check (defense in depth).
2. Make escalation of count > 0 non-recoverable by auto-fix (only escalate to impl). This forces the PR author to fix the root code issue, not suppress the gate.

## Cross-references

- `[[verifier-owns-ci-watch-shift-left]]` — the pr-ci-watch verifier owns the agent-side count check
- `[[no-lazy-allowlist-tech-debt]]` — never suppress new_violations via coverage exceptions
- `[[worker-owns-pr-readiness-loop]]` — the prior pattern that lacked explicit new_issues gate enforcement
- `.agents/proposals/pr-ci-verifier-integration-audit.md` §4.9 — detailed rationale for the new_issues=0 criterion
