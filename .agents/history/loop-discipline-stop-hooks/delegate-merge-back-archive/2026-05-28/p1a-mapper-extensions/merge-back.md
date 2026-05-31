---
schema_version: 1
task_id: p1a-mapper-extensions
parent_plan_id: loop-discipline-stop-hooks
title: 'Platform mapper extensions: Codex SubagentStop, Copilot agentStop, Cursor subagentStop'
summary: 'Extended hooks.go gate-critical mappings: codex SubagentStop, copilot agentStop+subagentStop, cursor subagentStop. Added table regression + negative copilot-stop guard in coverage_gap_test.go. Branch feature/p1a-mapper-extensions @ fc09eaf7. PR https://github.com/NikashPrakash/dot-agents/pull/88 (open, NOT merged). Fold-back recorded for bundle write_scope mismatch (hooks.go is the real edit target, not codex.go/copilot.go/cursor.go).'
files_changed: []
verification_result:
    status: pass
    summary: Bundle write_scope listed codex.go/copilot.go/cursor.go but the event-mapping switch statements live in internal/platform/hooks.go. Worker proceeded under the contract (the authoritative artifact). Fold-back at .agents/active/fold-back/p1a-bundle-write-scope-points-at-wrong-files.md. Reviewer to confirm matcher behaviour for new SubagentStop (left unset per contract D6). Sibling P1b/P1c/P1d/P1e still pending.
integration_notes: Bundle write_scope listed codex.go/copilot.go/cursor.go but the event-mapping switch statements live in internal/platform/hooks.go. Worker proceeded under the contract (the authoritative artifact). Fold-back at .agents/active/fold-back/p1a-bundle-write-scope-points-at-wrong-files.md. Reviewer to confirm matcher behaviour for new SubagentStop (left unset per contract D6). Sibling P1b/P1c/P1d/P1e still pending.
created_at: "2026-05-26T12:30:20Z"
---

## Summary

Extended hooks.go gate-critical mappings: codex SubagentStop, copilot agentStop+subagentStop, cursor subagentStop. Added table regression + negative copilot-stop guard in coverage_gap_test.go. Branch feature/p1a-mapper-extensions @ fc09eaf7. PR https://github.com/NikashPrakash/dot-agents/pull/88 (open, NOT merged). Fold-back recorded for bundle write_scope mismatch (hooks.go is the real edit target, not codex.go/copilot.go/cursor.go).

## Integration Notes

Bundle write_scope listed codex.go/copilot.go/cursor.go but the event-mapping switch statements live in internal/platform/hooks.go. Worker proceeded under the contract (the authoritative artifact). Fold-back at .agents/active/fold-back/p1a-bundle-write-scope-points-at-wrong-files.md. Reviewer to confirm matcher behaviour for new SubagentStop (left unset per contract D6). Sibling P1b/P1c/P1d/P1e still pending.
