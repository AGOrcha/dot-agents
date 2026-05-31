# Merge-back artifacts require full schema_version=1 frontmatter

**Captured:** 2026-05-28
**Triggered by:** coach + backlog-hygiene worker both hit silent closeout failures when writing minimal merge-back artifacts.

## The mistake

Writing a merge-back artifact at `.agents/active/merge-back/<task-id>.md` with a partial frontmatter block (e.g. only `task_id` + `summary`) — or no frontmatter at all — then running `da workflow delegation closeout --plan <P> --task <T> --decision accept`.

The closeout CLI rejects the artifact with a parse error or a missing-field error, and the failure mode is silent enough to be confusing on first hit (worker thinks closeout succeeded; it didn't).

## Why it happens

`commands/workflow/delegation.go`'s closeout reads the merge-back YAML frontmatter and asserts every documented field is present. Missing fields → reject. Schema-malformed YAML (e.g. unquoted `: ` inside `files_changed: [varies — see PR diff]`) → parse-fail → reject.

## The rule

When writing a merge-back artifact (whether to satisfy closeout for a properly-delegated worker or to back-fill for a historical orphan), use the **full schema_version=1 frontmatter** every time:

```yaml
---
schema_version: 1
task_id: <task-id>
parent_plan_id: <plan-id>
title: '<task title verbatim from TASKS.yaml>'
summary: '<one-paragraph what-shipped narrative>'
files_changed:
    - <repo-relative path>
    - <repo-relative path>
verification_result:
    status: pass        # or fail / rejected
    summary: '<CI / Sonar / gate state in one line>'
integration_notes: '<anything the parent needs to know — none usually fine>'
created_at: "<RFC3339 timestamp>"
---

## Summary

<free-text body — can mirror summary above>

## Integration Notes

<free-text — can mirror integration_notes>
```

## YAML safety subtleties

- `files_changed:` MUST be either `[]` (empty inline list) OR a block list with `- <path>` per entry. Free text inside inline list (e.g. `[varies — see diff]`) triggers YAML parse failure per the `[[schema-usage]]` colon-space rule.
- `summary:` and `integration_notes:` should use block scalar (`|-`) if the text contains `: ` (colon-space) anywhere — same colon-space hazard.
- `created_at:` is a quoted string in RFC3339 (e.g. `"2026-05-28T00:00:00Z"`), not a YAML date scalar.

## Where this came up in practice

- Backlog-hygiene worker (2026-05-28): wrote partial frontmatter for 21 orphan closeouts; closeout silently no-op'd until full frontmatter added.
- Session coach (2026-05-28): same — first auto-closeout attempts for #146/#148/#151/#152 failed with bare body; recovered by re-writing with full frontmatter.
- Orchestrator (this session): hit `files_changed: [varies — see PR diff]` parse failure on `pr8a-stale-refs-sweep` merge-back; fixed by using `files_changed: []` instead.

## Cross-references

- `[[schema-usage]]` — the underlying YAML colon-space rule
- `[[verify-task-status-vs-pr-history]]` — what triggers a closeout (PR merged, not just task_marked_completed)
- `[[no-lazy-allowlist-tech-debt]]` — closeout is part of the workflow contract; never skip via direct YAML edits

## Future systemization

A `da workflow merge-back --template` command that emits the full required frontmatter would prevent recurrence. Filed as part of [[workflow-orchestrator-daemon]] proposal scope (the daemon would generate these on closeout-mark events).
