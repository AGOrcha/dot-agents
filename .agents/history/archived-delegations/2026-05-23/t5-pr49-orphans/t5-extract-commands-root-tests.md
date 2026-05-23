---
schema_version: 1
task_id: t5-extract-commands-root-tests
parent_plan_id: go-test-fixture-extraction
title: Refactor commands/*_test.go (root) to use internal/testutil
summary: |-
    Refactored 3 of 4 write_scope files in commands/ to call internal/testutil.WriteScopeFile. 3 commits on branch t5-extract-commands-root-tests, PR #49 open against master (do not merge — user gates per one-PR-in-flight policy).

    Per-file dedup:
    - commands/rules_test.go: -16 LOC; removed writeRulesRule helper; 5 callers migrated. (6bf2de22)
    - commands/mcp_test.go + commands/settings_test.go: -34 LOC combined; removed writeSettings; retained writeMCPConfig with original body (no aliasing) because commands/coverage_test.go (out of scope) imports it. The bundle write_scope listed mcp_settings_test.go which doesn't exist in commands/ — the planner conflated the internal/platform single-file layout with commands/ split files; treated both as covering the typo'd slot. (036f90e6)
    - commands/refresh_test.go: -5 LOC; two seed sites fit WriteScopeFile cleanly. (25115529)
    - commands/import_test.go: NO commit. Every remaining dup pattern requires extending internal/testutil (out of scope per bundle policy).

    Verification at HEAD 25115529:
    - go build ./...                                       (clean)
    - go test ./commands/ -race -count=1 -timeout 240s     (PASS, 70.5s)
    - go vet ./commands/                                   (clean)
    - gofmt -l commands/{rules,mcp,settings,refresh,import}_test.go (empty)
files_changed:
    - .agents/workflow/plans/go-test-fixture-extraction/TASKS.yaml
verification_result:
    status: pass
    summary: |-
        Four findings the orchestrator should triage before advancing the plan:

        1. [HIGH] bundle-authoring: write_scope listed commands/mcp_settings_test.go which does not exist. Update bundle templates / fan-out scripts to validate scope paths against the live tree before delegation.

        2. [MEDIUM] testutil-extension wanted: the dominant remaining duplication is the 5-line tmp/HOME/agentsHome/Setenv/MkdirAll preamble repeated ~17x in refresh_test and ~10x in import_test. No existing helper covers it (NewTempProject overshoots). Recommend a t5.5 bundle that adds a NewTempAgentsHome(t) helper before t6/t7 land, otherwise those plans will hit the same wall.

        3. [MEDIUM] WriteScopeFile is single-basename only; nested resource paths (e.g. resources/<project>/.github/hooks/pre-tool.json) need a WriteScopeFilePath variant. Same t5.5 candidate.

        4. [LOW] commands/coverage_test.go still calls writeMCPConfig. Add to t8 sweep so the helper can be removed outright.

        Decision needed: spawn t5.5 testutil-extension bundle, or accept partial dedup and let t6/t7 surface the same findings? PR #49 stands either way — its diff is minimal and clean.

        Full per-file LOC deltas and finding details in:
        .agents/active/verification/t5-extract-commands-root-tests/impl-handoff.yaml
integration_notes: |-
    Four findings the orchestrator should triage before advancing the plan:

    1. [HIGH] bundle-authoring: write_scope listed commands/mcp_settings_test.go which does not exist. Update bundle templates / fan-out scripts to validate scope paths against the live tree before delegation.

    2. [MEDIUM] testutil-extension wanted: the dominant remaining duplication is the 5-line tmp/HOME/agentsHome/Setenv/MkdirAll preamble repeated ~17x in refresh_test and ~10x in import_test. No existing helper covers it (NewTempProject overshoots). Recommend a t5.5 bundle that adds a NewTempAgentsHome(t) helper before t6/t7 land, otherwise those plans will hit the same wall.

    3. [MEDIUM] WriteScopeFile is single-basename only; nested resource paths (e.g. resources/<project>/.github/hooks/pre-tool.json) need a WriteScopeFilePath variant. Same t5.5 candidate.

    4. [LOW] commands/coverage_test.go still calls writeMCPConfig. Add to t8 sweep so the helper can be removed outright.

    Decision needed: spawn t5.5 testutil-extension bundle, or accept partial dedup and let t6/t7 surface the same findings? PR #49 stands either way — its diff is minimal and clean.

    Full per-file LOC deltas and finding details in:
    .agents/active/verification/t5-extract-commands-root-tests/impl-handoff.yaml
created_at: "2026-05-23T22:25:04Z"
---

## Summary

Refactored 3 of 4 write_scope files in commands/ to call internal/testutil.WriteScopeFile. 3 commits on branch t5-extract-commands-root-tests, PR #49 open against master (do not merge — user gates per one-PR-in-flight policy).

Per-file dedup:
- commands/rules_test.go: -16 LOC; removed writeRulesRule helper; 5 callers migrated. (6bf2de22)
- commands/mcp_test.go + commands/settings_test.go: -34 LOC combined; removed writeSettings; retained writeMCPConfig with original body (no aliasing) because commands/coverage_test.go (out of scope) imports it. The bundle write_scope listed mcp_settings_test.go which doesn't exist in commands/ — the planner conflated the internal/platform single-file layout with commands/ split files; treated both as covering the typo'd slot. (036f90e6)
- commands/refresh_test.go: -5 LOC; two seed sites fit WriteScopeFile cleanly. (25115529)
- commands/import_test.go: NO commit. Every remaining dup pattern requires extending internal/testutil (out of scope per bundle policy).

Verification at HEAD 25115529:
- go build ./...                                       (clean)
- go test ./commands/ -race -count=1 -timeout 240s     (PASS, 70.5s)
- go vet ./commands/                                   (clean)
- gofmt -l commands/{rules,mcp,settings,refresh,import}_test.go (empty)

## Integration Notes

Four findings the orchestrator should triage before advancing the plan:

1. [HIGH] bundle-authoring: write_scope listed commands/mcp_settings_test.go which does not exist. Update bundle templates / fan-out scripts to validate scope paths against the live tree before delegation.

2. [MEDIUM] testutil-extension wanted: the dominant remaining duplication is the 5-line tmp/HOME/agentsHome/Setenv/MkdirAll preamble repeated ~17x in refresh_test and ~10x in import_test. No existing helper covers it (NewTempProject overshoots). Recommend a t5.5 bundle that adds a NewTempAgentsHome(t) helper before t6/t7 land, otherwise those plans will hit the same wall.

3. [MEDIUM] WriteScopeFile is single-basename only; nested resource paths (e.g. resources/<project>/.github/hooks/pre-tool.json) need a WriteScopeFilePath variant. Same t5.5 candidate.

4. [LOW] commands/coverage_test.go still calls writeMCPConfig. Add to t8 sweep so the helper can be removed outright.

Decision needed: spawn t5.5 testutil-extension bundle, or accept partial dedup and let t6/t7 surface the same findings? PR #49 stands either way — its diff is minimal and clean.

Full per-file LOC deltas and finding details in:
.agents/active/verification/t5-extract-commands-root-tests/impl-handoff.yaml
