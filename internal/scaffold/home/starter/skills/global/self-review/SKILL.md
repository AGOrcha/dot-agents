---
name: self-review
description: "Review your own changes and produce a structured pass/fail verdict before committing or creating a pull request. Use before any git commit of significant changes, before creating a PR, or when invoked as part of iteration-close's review step."
tier: T2
contract:
  reads:
    - "git diff (staged + unstaged) — the change set under review"
    - "git status — untracked files that may need review"
    - "active delegation contract or `workflow status` — to resolve <task_id> when fired inside iteration-close"
    - "`da kg changes --brief` and `da kg impact <files>` output — Step 0 graph context (see instructions/kg-context.md). Optional: gracefully degrades when the KG bridge is unavailable."
    - "instructions/{kg-context,code-quality,security,performance,gotchas,output-format}.md — per-step rules"
    - "eval/{advisory-board,checklist}.md — multi-persona review and final pass/fail gate"
  writes:
    - ".agents/active/verification/<task_id>/review-decision.yaml — strict on-disk schema; see instructions/output-format.md for the full field contract."
  escape_hatches:
    - "Pause for human (overall_decision = escalate) when: ambiguous architectural intent that is not resolvable from diff + KG context alone; security-sensitive change touching auth, secrets, or external boundaries with no clear precedent; review uncovers behavioral drift from a referenced spec or design doc. Always populate `escalation_reason` with the concrete trigger; the schema rejects empty escalations."
    - "Skip Step 0 (graceful degradation) when the KG bridge is unavailable. Emit a single-line WARN, capture it into reviewer_notes, and continue — do not abort self-review. See instructions/kg-context.md."
---

# Self-Review Orchestrator

This skill orchestrates a structured self-review of your changes. Each step loads only the relevant instruction file to keep context lean.

The skill's load-bearing deliverable is the structured artifact at
`.agents/active/verification/<task_id>/review-decision.yaml` (schema
documented in `instructions/output-format.md`). Your chat-narrative
summary is secondary — the YAML is what the rest of the workflow
pipeline (`workflow checkpoint --log-to-iter --role review`) consumes.
When fired inside iteration-close, the skill runs after `verify record
--kind test` and before `workflow checkpoint`.

## Workflow

### Step 0: KG Context

Load → `instructions/kg-context.md`

- Run `da kg changes --brief` and `da kg impact <changed_files>` against the staged diff.
- Capture stdout verbatim into the running review narrative; this becomes part of `reviewer_notes` in the final artifact.
- Degrade gracefully if the KG bridge is unavailable (warn, skip the failed sub-call, proceed).

Per-file review starts with global blast-radius, not in the blind — this
step exists so later steps can reason about downstream impact instead of
reviewing each file in isolation.

### Step 1: Gather the Diff

- Run `git diff` (unstaged) and `git diff --cached` (staged) to collect all changes.
- Run `git status` to identify untracked files that may need review.
- Note which files changed and categorize them (source, test, config, docs).

### Step 2: Code Quality Review

Load → `instructions/code-quality.md`

- Apply code quality rules to every changed file.
- Flag anything that violates naming, readability, or error handling standards.

### Step 3: Security Scan

Load → `instructions/security.md`

- Run through the security checklist against all changes.
- Pay extra attention to files handling user input, authentication, or external data.

### Step 4: Performance Check

Load → `instructions/performance.md`

- Evaluate changed code for performance concerns.
- Only flag issues that are realistic for the scope of the change.

### Step 5: Gotchas Sweep

Load → `instructions/gotchas.md`

- Check for the most commonly missed issues.
- These are fast to check and frequently caught in real code reviews.

### Step 6: Advisory Board

Load → `eval/advisory-board.md`

- Run the three reviewer personas in parallel against the changes.
- Collect their independent findings.

### Step 7: Final Checklist

Load → `eval/checklist.md`

- Run the pass/fail checklist.
- Every item must pass before the review is considered complete.
- If any item fails, fix it and re-run from the relevant step.

### Step 8: Write the Structured Output

Load → `instructions/output-format.md`

- Produce the `review-decision.yaml` artifact at `.agents/active/verification/<task_id>/review-decision.yaml`.
- Resolve `<task_id>` from the active iteration context (or synthesize `adhoc-<RFC3339>` for standalone runs).
- When fired inside iteration-close, prefer `da workflow verify record --kind review` so the CLI validates the file against its schema before persisting.
- Pack the telemetry envelope (resource_type, outcome, post_invocation, improvement_signals) into `reviewer_notes` as a fenced YAML block — the on-disk schema is `additionalProperties: false` today, so envelope fields are not promoted to top-level keys.

## Output Format

The structured YAML artifact is the load-bearing deliverable. The
chat narrative below is a human-readable summary; it does **not**
replace Step 8.

```
## Self-Review Summary

**Files reviewed:** [count]
**Issues found:** [count]
**Severity breakdown:** [critical/warning/info counts]

### Findings
[List each finding with file, line, severity, and recommendation]

### Checklist Result
[Pass/Fail with any failing items noted]

### Verdict
[PASS — ready to commit | NEEDS FIXES — list what to address]

### Artifact
Wrote review-decision.yaml at .agents/active/verification/<task_id>/
```
