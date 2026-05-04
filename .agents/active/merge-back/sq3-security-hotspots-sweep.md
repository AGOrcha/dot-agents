---
schema_version: 1
task_id: sq3-security-hotspots-sweep
parent_plan_id: sonarqube-pr10
title: Review and resolve security hotspots flagged on PR#10
summary: 'BLOCKED: mcp__sonarqube__change_security_hotspot_status denied by sandbox (5/5 attempted writes returned permission-denied). Read-only MCP tools (search/show hotspot, gate status) work. Per-hotspot inspection complete for all 35 hotspots across clusters G/H/I — every Go LookPath site uses a hard-coded literal binary name; the 3 TS regexes are bounded to small repo-local file content (front-matter, loop-state.md sections); the 4 TS test publicly-writable-dir hotspots use deterministic /tmp paths confined to the test process. No code changes made. Quality gate unchanged: new_security_hotspots_reviewed still 0.0% / 35 TO_REVIEW. Fold-back filed (slug sq3-mcp-write-denied). To unblock: grant the change_security_hotspot_status MCP capability to the worker, then re-run sq3 — the inspection is cached below so the re-run is purely mechanical status transitions.'
files_changed:
    - .agents/active/delegation-bundles/del-sq1-current-state-audit-1777864449.yaml
    - .agents/active/delegation-bundles/del-t2-extend-self-review-skill-1777864417.yaml
    - .agents/active/delegation-bundles/del-tp1-current-gap-audit-1777864446.yaml
    - .agents/active/delegation/sq1-current-state-audit.yaml
    - .agents/active/delegation/t2-extend-self-review-skill.yaml
    - .agents/active/delegation/tp1-current-gap-audit.yaml
    - .agents/active/merge-back/sq1-current-state-audit.md
    - .agents/active/merge-back/t2-extend-self-review-skill.md
    - .agents/active/merge-back/tp1-current-gap-audit.md
    - .agents/active/verification/sq1-current-state-audit/doc-audit.result.yaml
    - .agents/active/verification/sq1-current-state-audit/merge-back.result.yaml
    - .agents/active/verification/t2-extend-self-review-skill/merge-back.result.yaml
    - .agents/active/verification/t2-extend-self-review-skill/review-decision.yaml
    - .agents/active/verification/t2-extend-self-review-skill/skill-extension.result.yaml
    - .agents/active/verification/tp1-current-gap-audit/doc-audit.result.yaml
    - .agents/active/verification/tp1-current-gap-audit/merge-back.result.yaml
    - .agents/workflow/plans/binary-rename-da-sweep/PLAN.yaml
    - .agents/workflow/plans/binary-rename-da-sweep/TASKS.yaml
    - .agents/workflow/plans/binary-rename-da-sweep/binary-rename-da-sweep.plan.md
    - .agents/workflow/plans/self-review-iteration-close-wiring/PLAN.yaml
    - .agents/workflow/plans/self-review-iteration-close-wiring/TASKS.yaml
    - .agents/workflow/plans/sonarqube-pr10/PLAN.yaml
    - .agents/workflow/plans/sonarqube-pr10/TASKS.yaml
    - .agents/workflow/plans/typescript-port/PLAN.yaml
    - .agents/workflow/plans/typescript-port/TASKS.yaml
verification_result:
    status: fail
    summary: |-
        Per-hotspot dispositions and rationales follow. All 35 hotspots should transition to REVIEWED (resolution=SAFE) with the listed comments.

        CLUSTER G — Go S4036 (27 hotspots) — all REVIEWED-SAFE with comment template: 'Hard-coded tool name <NAME>; user-shell context; dot-agents runs on dev workstations (not privileged); no escalation surface. Binary name is a string literal at the cited line, not config-derived.'
          AZ2iONnvyZ11xJYYqyWC commands/sync/commit.go:31  exec.Command("git", ...)
          AZ2iONnvyZ11xJYYqyWD commands/sync/commit.go:33  exec.Command("git", ...)
          AZ2iONnvyZ11xJYYqyWE commands/sync/commit.go:34  exec.Command("git", ...)
          AZ2iONnvyZ11xJYYqyWF commands/sync/commit.go:46  exec.Command("git", ...)
          AZ2iONnvyZ11xJYYqyWG commands/sync/commit.go:47  exec.Command("git", ...)
          AZ2iONn7yZ11xJYYqyWH commands/sync/helpers.go:60  exec.Command("git", ...)
          AZ2iONn7yZ11xJYYqyWI commands/sync/helpers.go:68  exec.Command("git", ...)
          AZ2iONn7yZ11xJYYqyWJ commands/sync/helpers.go:83  exec.Command("git", ...)
          AZ2iONn7yZ11xJYYqyWK commands/sync/helpers.go:117 exec.Command("git", ...)
          AZ2iONnpyZ11xJYYqyV9 commands/sync/init.go:30  exec.Command("git", ...)
          AZ2iONnpyZ11xJYYqyV- commands/sync/init.go:61  exec.Command("git", ...)
          AZ2iONnpyZ11xJYYqyV_ commands/sync/init.go:71  exec.Command("git", ...)
          AZ2iONnpyZ11xJYYqyWA commands/sync/init.go:72  exec.Command("git", ...)
          AZ2iONnZyZ11xJYYqyV3 commands/sync/log.go:18  exec.Command("git", ...)
          AZ2iONnOyZ11xJYYqyV2 commands/sync/pull.go:21  exec.Command("git", ...)
          AZ2iONneyZ11xJYYqyV4 commands/sync/push.go:25  exec.Command("git", ...)
          AZ2iONneyZ11xJYYqyV5 commands/sync/push.go:42  exec.Command("git", ...)
          AZ2iONneyZ11xJYYqyV6 commands/sync/push.go:43  exec.Command("git", ...)
          AZ2iONneyZ11xJYYqyV7 commands/sync/push.go:56  exec.Command("git", ...)
          AZ2iZ3fwr9jJuyXHSd43 commands/workflow/delegation.go:970  exec.Command("git", ...)
          AZ2iZ3g5r9jJuyXHSd5O commands/workflow/iter_log.go:132  exec.Command("git", ...)
          AZ2wRK7jQSoJ_LA-t2p6 commands/workflow/plan_task.go:2425  exec.Command("git", ...)
          AZ2wRK7jQSoJ_LA-t2p7 commands/workflow/plan_task.go:2431  exec.Command("git", ...)
          AZ2iZ3dzr9jJuyXHSd4q commands/workflow/state.go:724  exec.Command("git", ...)
          AZ2iZ3dzr9jJuyXHSd4r commands/workflow/state.go:729  exec.Command("git", ...)
          AZ2ilgYzr9jJuyXHTjJr commands/kg/sync_code_warm_link.go:54  exec.Command("git", ...) (gitArgs is a []string{} of literals built earlier in func)
          AZ2oWQlZyuBU8AxYy5kE internal/graphstore/crg.go:484  exec.Command("git", ...)
          AZ2E5bwPt-zbsGOGos1e internal/platform/cursor.go:63  exec.CommandContext(ctx, "defaults", ...) (NAME="defaults")

        CLUSTER H — TS S5852 regex backtracking (3 hotspots) — all REVIEWED-SAFE.
          AZ2NiAVT8vXIT0bFW4x3 ports/typescript/src/commands/agents.ts:33  /^description:\s*(.+)$/m
            rationale: Applied to file content read from in-repo AGENT.md (controlled by the dot-agents project, not user input); the (.+) matches a single line under the multiline flag with no nested quantifier, so backtracking is bounded by line length. SAFE: no DoS surface; input is repo-local front-matter, typically <100 chars per match.
          AZ2NiAUx8vXIT0bFW4xz ports/typescript/src/commands/skills.ts:34  /^description:\s*(.+)$/m
            rationale: Identical pattern to agents.ts:33; applied to in-repo SKILL.md front-matter. Same SAFE rationale: bounded line length, repo-controlled content.
          AZ2NiAR48vXIT0bFW4xf ports/typescript/src/commands/workflow.ts:81  /^## Current Position\s*\n([\s\S]*?)(?=^## |\z)/m
            rationale: Lazy quantifier ([\s\S]*?) with explicit lookahead terminator (?=^## |\z); applied to ~/.agents/loop-state.md content (repo-local file produced by dot-agents itself). Lazy-with-anchor pattern is the canonical non-catastrophic shape; not user-controllable. SAFE.

        CLUSTER I — TS S5443 publicly-writable-dir in tests (4 hotspots) — all REVIEWED-SAFE with comment: 'Tests use deterministic /tmp/__sentinel__ paths confined to the test process; bounded to fixture isolation; no privilege escalation surface.'
          AZ2NiAXL8vXIT0bFW4yE ports/typescript/tests/commands.test.ts:98   '/tmp/__does_not_exist_999__' (literal string passed as a stale-path probe)
          AZ2NiAXL8vXIT0bFW4yF ports/typescript/tests/commands.test.ts:163  '/tmp/__ghost_project_9999__' (literal in fake config.json fixture)
          AZ2NiAXL8vXIT0bFW4yG ports/typescript/tests/commands.test.ts:293  '/tmp/__stale_project__' (literal in fake config.json fixture)
          AZ2NiAW38vXIT0bFW4yC ports/typescript/tests/kg.test.ts:92  '/tmp/fake-kg' (kgHomeOverride; the runKgQuery stub ignores the override per test assertion)

        NEXT STEP for parent / re-run:
          for each hotspot key K above:
            mcp__sonarqube__change_security_hotspot_status \
              --hotspotKey K --status REVIEWED --resolution SAFE --comment <line above>
          Then re-run mcp__sonarqube__get_project_quality_gate_status to confirm new_security_hotspots_reviewed == 100%.
integration_notes: |-
    Per-hotspot dispositions and rationales follow. All 35 hotspots should transition to REVIEWED (resolution=SAFE) with the listed comments.

    CLUSTER G — Go S4036 (27 hotspots) — all REVIEWED-SAFE with comment template: 'Hard-coded tool name <NAME>; user-shell context; dot-agents runs on dev workstations (not privileged); no escalation surface. Binary name is a string literal at the cited line, not config-derived.'
      AZ2iONnvyZ11xJYYqyWC commands/sync/commit.go:31  exec.Command("git", ...)
      AZ2iONnvyZ11xJYYqyWD commands/sync/commit.go:33  exec.Command("git", ...)
      AZ2iONnvyZ11xJYYqyWE commands/sync/commit.go:34  exec.Command("git", ...)
      AZ2iONnvyZ11xJYYqyWF commands/sync/commit.go:46  exec.Command("git", ...)
      AZ2iONnvyZ11xJYYqyWG commands/sync/commit.go:47  exec.Command("git", ...)
      AZ2iONn7yZ11xJYYqyWH commands/sync/helpers.go:60  exec.Command("git", ...)
      AZ2iONn7yZ11xJYYqyWI commands/sync/helpers.go:68  exec.Command("git", ...)
      AZ2iONn7yZ11xJYYqyWJ commands/sync/helpers.go:83  exec.Command("git", ...)
      AZ2iONn7yZ11xJYYqyWK commands/sync/helpers.go:117 exec.Command("git", ...)
      AZ2iONnpyZ11xJYYqyV9 commands/sync/init.go:30  exec.Command("git", ...)
      AZ2iONnpyZ11xJYYqyV- commands/sync/init.go:61  exec.Command("git", ...)
      AZ2iONnpyZ11xJYYqyV_ commands/sync/init.go:71  exec.Command("git", ...)
      AZ2iONnpyZ11xJYYqyWA commands/sync/init.go:72  exec.Command("git", ...)
      AZ2iONnZyZ11xJYYqyV3 commands/sync/log.go:18  exec.Command("git", ...)
      AZ2iONnOyZ11xJYYqyV2 commands/sync/pull.go:21  exec.Command("git", ...)
      AZ2iONneyZ11xJYYqyV4 commands/sync/push.go:25  exec.Command("git", ...)
      AZ2iONneyZ11xJYYqyV5 commands/sync/push.go:42  exec.Command("git", ...)
      AZ2iONneyZ11xJYYqyV6 commands/sync/push.go:43  exec.Command("git", ...)
      AZ2iONneyZ11xJYYqyV7 commands/sync/push.go:56  exec.Command("git", ...)
      AZ2iZ3fwr9jJuyXHSd43 commands/workflow/delegation.go:970  exec.Command("git", ...)
      AZ2iZ3g5r9jJuyXHSd5O commands/workflow/iter_log.go:132  exec.Command("git", ...)
      AZ2wRK7jQSoJ_LA-t2p6 commands/workflow/plan_task.go:2425  exec.Command("git", ...)
      AZ2wRK7jQSoJ_LA-t2p7 commands/workflow/plan_task.go:2431  exec.Command("git", ...)
      AZ2iZ3dzr9jJuyXHSd4q commands/workflow/state.go:724  exec.Command("git", ...)
      AZ2iZ3dzr9jJuyXHSd4r commands/workflow/state.go:729  exec.Command("git", ...)
      AZ2ilgYzr9jJuyXHTjJr commands/kg/sync_code_warm_link.go:54  exec.Command("git", ...) (gitArgs is a []string{} of literals built earlier in func)
      AZ2oWQlZyuBU8AxYy5kE internal/graphstore/crg.go:484  exec.Command("git", ...)
      AZ2E5bwPt-zbsGOGos1e internal/platform/cursor.go:63  exec.CommandContext(ctx, "defaults", ...) (NAME="defaults")

    CLUSTER H — TS S5852 regex backtracking (3 hotspots) — all REVIEWED-SAFE.
      AZ2NiAVT8vXIT0bFW4x3 ports/typescript/src/commands/agents.ts:33  /^description:\s*(.+)$/m
        rationale: Applied to file content read from in-repo AGENT.md (controlled by the dot-agents project, not user input); the (.+) matches a single line under the multiline flag with no nested quantifier, so backtracking is bounded by line length. SAFE: no DoS surface; input is repo-local front-matter, typically <100 chars per match.
      AZ2NiAUx8vXIT0bFW4xz ports/typescript/src/commands/skills.ts:34  /^description:\s*(.+)$/m
        rationale: Identical pattern to agents.ts:33; applied to in-repo SKILL.md front-matter. Same SAFE rationale: bounded line length, repo-controlled content.
      AZ2NiAR48vXIT0bFW4xf ports/typescript/src/commands/workflow.ts:81  /^## Current Position\s*\n([\s\S]*?)(?=^## |\z)/m
        rationale: Lazy quantifier ([\s\S]*?) with explicit lookahead terminator (?=^## |\z); applied to ~/.agents/loop-state.md content (repo-local file produced by dot-agents itself). Lazy-with-anchor pattern is the canonical non-catastrophic shape; not user-controllable. SAFE.

    CLUSTER I — TS S5443 publicly-writable-dir in tests (4 hotspots) — all REVIEWED-SAFE with comment: 'Tests use deterministic /tmp/__sentinel__ paths confined to the test process; bounded to fixture isolation; no privilege escalation surface.'
      AZ2NiAXL8vXIT0bFW4yE ports/typescript/tests/commands.test.ts:98   '/tmp/__does_not_exist_999__' (literal string passed as a stale-path probe)
      AZ2NiAXL8vXIT0bFW4yF ports/typescript/tests/commands.test.ts:163  '/tmp/__ghost_project_9999__' (literal in fake config.json fixture)
      AZ2NiAXL8vXIT0bFW4yG ports/typescript/tests/commands.test.ts:293  '/tmp/__stale_project__' (literal in fake config.json fixture)
      AZ2NiAW38vXIT0bFW4yC ports/typescript/tests/kg.test.ts:92  '/tmp/fake-kg' (kgHomeOverride; the runKgQuery stub ignores the override per test assertion)

    NEXT STEP for parent / re-run:
      for each hotspot key K above:
        mcp__sonarqube__change_security_hotspot_status \
          --hotspotKey K --status REVIEWED --resolution SAFE --comment <line above>
      Then re-run mcp__sonarqube__get_project_quality_gate_status to confirm new_security_hotspots_reviewed == 100%.
created_at: "2026-05-04T04:22:15Z"
---

## Summary

BLOCKED: mcp__sonarqube__change_security_hotspot_status denied by sandbox (5/5 attempted writes returned permission-denied). Read-only MCP tools (search/show hotspot, gate status) work. Per-hotspot inspection complete for all 35 hotspots across clusters G/H/I — every Go LookPath site uses a hard-coded literal binary name; the 3 TS regexes are bounded to small repo-local file content (front-matter, loop-state.md sections); the 4 TS test publicly-writable-dir hotspots use deterministic /tmp paths confined to the test process. No code changes made. Quality gate unchanged: new_security_hotspots_reviewed still 0.0% / 35 TO_REVIEW. Fold-back filed (slug sq3-mcp-write-denied). To unblock: grant the change_security_hotspot_status MCP capability to the worker, then re-run sq3 — the inspection is cached below so the re-run is purely mechanical status transitions.

## Integration Notes

Per-hotspot dispositions and rationales follow. All 35 hotspots should transition to REVIEWED (resolution=SAFE) with the listed comments.

CLUSTER G — Go S4036 (27 hotspots) — all REVIEWED-SAFE with comment template: 'Hard-coded tool name <NAME>; user-shell context; dot-agents runs on dev workstations (not privileged); no escalation surface. Binary name is a string literal at the cited line, not config-derived.'
  AZ2iONnvyZ11xJYYqyWC commands/sync/commit.go:31  exec.Command("git", ...)
  AZ2iONnvyZ11xJYYqyWD commands/sync/commit.go:33  exec.Command("git", ...)
  AZ2iONnvyZ11xJYYqyWE commands/sync/commit.go:34  exec.Command("git", ...)
  AZ2iONnvyZ11xJYYqyWF commands/sync/commit.go:46  exec.Command("git", ...)
  AZ2iONnvyZ11xJYYqyWG commands/sync/commit.go:47  exec.Command("git", ...)
  AZ2iONn7yZ11xJYYqyWH commands/sync/helpers.go:60  exec.Command("git", ...)
  AZ2iONn7yZ11xJYYqyWI commands/sync/helpers.go:68  exec.Command("git", ...)
  AZ2iONn7yZ11xJYYqyWJ commands/sync/helpers.go:83  exec.Command("git", ...)
  AZ2iONn7yZ11xJYYqyWK commands/sync/helpers.go:117 exec.Command("git", ...)
  AZ2iONnpyZ11xJYYqyV9 commands/sync/init.go:30  exec.Command("git", ...)
  AZ2iONnpyZ11xJYYqyV- commands/sync/init.go:61  exec.Command("git", ...)
  AZ2iONnpyZ11xJYYqyV_ commands/sync/init.go:71  exec.Command("git", ...)
  AZ2iONnpyZ11xJYYqyWA commands/sync/init.go:72  exec.Command("git", ...)
  AZ2iONnZyZ11xJYYqyV3 commands/sync/log.go:18  exec.Command("git", ...)
  AZ2iONnOyZ11xJYYqyV2 commands/sync/pull.go:21  exec.Command("git", ...)
  AZ2iONneyZ11xJYYqyV4 commands/sync/push.go:25  exec.Command("git", ...)
  AZ2iONneyZ11xJYYqyV5 commands/sync/push.go:42  exec.Command("git", ...)
  AZ2iONneyZ11xJYYqyV6 commands/sync/push.go:43  exec.Command("git", ...)
  AZ2iONneyZ11xJYYqyV7 commands/sync/push.go:56  exec.Command("git", ...)
  AZ2iZ3fwr9jJuyXHSd43 commands/workflow/delegation.go:970  exec.Command("git", ...)
  AZ2iZ3g5r9jJuyXHSd5O commands/workflow/iter_log.go:132  exec.Command("git", ...)
  AZ2wRK7jQSoJ_LA-t2p6 commands/workflow/plan_task.go:2425  exec.Command("git", ...)
  AZ2wRK7jQSoJ_LA-t2p7 commands/workflow/plan_task.go:2431  exec.Command("git", ...)
  AZ2iZ3dzr9jJuyXHSd4q commands/workflow/state.go:724  exec.Command("git", ...)
  AZ2iZ3dzr9jJuyXHSd4r commands/workflow/state.go:729  exec.Command("git", ...)
  AZ2ilgYzr9jJuyXHTjJr commands/kg/sync_code_warm_link.go:54  exec.Command("git", ...) (gitArgs is a []string{} of literals built earlier in func)
  AZ2oWQlZyuBU8AxYy5kE internal/graphstore/crg.go:484  exec.Command("git", ...)
  AZ2E5bwPt-zbsGOGos1e internal/platform/cursor.go:63  exec.CommandContext(ctx, "defaults", ...) (NAME="defaults")

CLUSTER H — TS S5852 regex backtracking (3 hotspots) — all REVIEWED-SAFE.
  AZ2NiAVT8vXIT0bFW4x3 ports/typescript/src/commands/agents.ts:33  /^description:\s*(.+)$/m
    rationale: Applied to file content read from in-repo AGENT.md (controlled by the dot-agents project, not user input); the (.+) matches a single line under the multiline flag with no nested quantifier, so backtracking is bounded by line length. SAFE: no DoS surface; input is repo-local front-matter, typically <100 chars per match.
  AZ2NiAUx8vXIT0bFW4xz ports/typescript/src/commands/skills.ts:34  /^description:\s*(.+)$/m
    rationale: Identical pattern to agents.ts:33; applied to in-repo SKILL.md front-matter. Same SAFE rationale: bounded line length, repo-controlled content.
  AZ2NiAR48vXIT0bFW4xf ports/typescript/src/commands/workflow.ts:81  /^## Current Position\s*\n([\s\S]*?)(?=^## |\z)/m
    rationale: Lazy quantifier ([\s\S]*?) with explicit lookahead terminator (?=^## |\z); applied to ~/.agents/loop-state.md content (repo-local file produced by dot-agents itself). Lazy-with-anchor pattern is the canonical non-catastrophic shape; not user-controllable. SAFE.

CLUSTER I — TS S5443 publicly-writable-dir in tests (4 hotspots) — all REVIEWED-SAFE with comment: 'Tests use deterministic /tmp/__sentinel__ paths confined to the test process; bounded to fixture isolation; no privilege escalation surface.'
  AZ2NiAXL8vXIT0bFW4yE ports/typescript/tests/commands.test.ts:98   '/tmp/__does_not_exist_999__' (literal string passed as a stale-path probe)
  AZ2NiAXL8vXIT0bFW4yF ports/typescript/tests/commands.test.ts:163  '/tmp/__ghost_project_9999__' (literal in fake config.json fixture)
  AZ2NiAXL8vXIT0bFW4yG ports/typescript/tests/commands.test.ts:293  '/tmp/__stale_project__' (literal in fake config.json fixture)
  AZ2NiAW38vXIT0bFW4yC ports/typescript/tests/kg.test.ts:92  '/tmp/fake-kg' (kgHomeOverride; the runKgQuery stub ignores the override per test assertion)

NEXT STEP for parent / re-run:
  for each hotspot key K above:
    mcp__sonarqube__change_security_hotspot_status \
      --hotspotKey K --status REVIEWED --resolution SAFE --comment <line above>
  Then re-run mcp__sonarqube__get_project_quality_gate_status to confirm new_security_hotspots_reviewed == 100%.
