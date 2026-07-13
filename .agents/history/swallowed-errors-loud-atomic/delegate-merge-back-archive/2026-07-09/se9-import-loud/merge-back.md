---
schema_version: 1
task_id: se9-import-loud
parent_plan_id: swallowed-errors-loud-atomic
title: import/hook-bundle error-surfacing
summary: 'Opened PR #366 (swallowed/9-import-loud, base master) for the se9-import swallowed-errors slice. commands/import.go: extracted statImportSourceCandidate so processImportCandidate''s source os.Stat distinguishes a real error (warned + counted as skipped) from legitimate os.IsNotExist absence (silent no-op, unchanged). Added isJSONHookSyntaxError (errors.As *json.SyntaxError) and wired it into all 4 canonical hook-bundle readers (Copilot/Cursor/Codex/ClaudeCompat) so a genuinely corrupt JSON file warns loudly while still returning false; ordinary not-a-hook-bundle content stays silent. commands/import_plugins.go: same treatment on loadImportedPackagePluginManifest''s json.Unmarshal. Updated existing InvalidJSON/InvalidJSONReturnsFalse tests (Cursor x2, Codex, Copilot, plugin manifest) to assert the new warning via the existing captureRelinkStdout helper; added TestCanonicalHookBundleOutputsFromClaudeCompatFile_InvalidJSON (no prior coverage) and two new processImportCandidate tests (generic + canonical hook-bundle path) using testutil.MakeDirUnreadable to force a real Stat error. go build/test/vet all green. Per-file coverage: import.go 95.70% (clears 95% gate outright), import_plugins.go 93.44% (pre-existing [legacy-tail] allowlist entry in scripts/coverage-exceptions.txt, refreshed from 93.39% -> 93.44%; the touched function loadImportedPackagePluginManifest is itself 100% covered, remaining gap is unrelated pre-existing debt). Disjoint file set from the sibling se9-commands-loud PR (kg/lifecycle/workflow/remove/refresh) per the fresh-off-master decomposition. PR left open, not merged.'
files_changed:
    - .agents/active/delegation-bundles/del-cg-project-local-overlay-1780781079.yaml
    - .agents/active/delegation-bundles/del-gcc5-verify-close-unblock-1779848143.yaml
    - .agents/active/delegation-bundles/del-l1-followups-1782677806.yaml
    - .agents/active/delegation-bundles/del-p1c-verifier-profile-source-aware-1780784015.yaml
    - .agents/active/delegation-bundles/del-p1e-docs-hooks-consistency-1779841818.yaml
    - .agents/active/delegation-bundles/del-p2-hook-scripts-1779841782.yaml
    - .agents/active/delegation-bundles/del-p3-starter-promotion-1779841783.yaml
    - .agents/active/delegation-bundles/del-t14-import-graph-assertion-1779841818.yaml
    - .agents/active/delegation-bundles/del-t2-config-relevance-resolver-1780554684.yaml
    - .agents/active/delegation-bundles/del-t3-cli-readback-1779841781.yaml
    - .agents/active/delegation-bundles/del-t4-relevance-recompute-1780556883.yaml
    - .agents/active/delegation/gcc5-verify-close-unblock.yaml
    - .agents/active/delegation/l1-followups.yaml
    - .agents/active/delegation/p1c-verifier-profile-source-aware.yaml
    - .agents/active/delegation/p1e-docs-hooks-consistency.yaml
    - .agents/active/delegation/p2-hook-scripts.yaml
    - .agents/active/delegation/p3-starter-promotion.yaml
    - .agents/active/delegation/t14-import-graph-assertion.yaml
    - .agents/active/delegation/t3-cli-readback.yaml
    - .agents/active/merge-back/gcc5-verify-close-unblock.md
    - .agents/active/merge-back/t3-cli-readback.md
    - .agents/active/verification/gcc5-verify-close-unblock/custom.result.yaml
    - .agents/active/verification/gcc5-verify-close-unblock/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/merge-back.result.yaml
    - .agents/active/verification/t3-cli-readback/unit.result.yaml
    - .agents/lessons/index.md
    - .agents/workflow/plans/swallowed-errors-loud-atomic/PLAN.yaml
    - .agents/workflow/plans/swallowed-errors-loud-atomic/TASKS.yaml
    - .agentsrc.json
    - commands/import.go
    - commands/import_test.go
    - commands/internal/lifecycle/deps.go
    - commands/internal/lifecycle/project.go
    - commands/internal/lifecycle/project_test.go
    - commands/root.go
    - internal/config/fetcher.go
    - internal/config/fetcher_test.go
    - internal/dashboard/handlers/handlers_test.go
verification_result:
    status: pass
    summary: Files import.go/import_test.go/import_plugins.go/import_plugins_test.go are still listed in se9-commands-loud's write_scope in TASKS.yaml from before the revert -- that entry is stale and should be trimmed when se9-commands-loud closes out, but was left untouched here since Se9CommandsFinalize owns that task.
integration_notes: Files import.go/import_test.go/import_plugins.go/import_plugins_test.go are still listed in se9-commands-loud's write_scope in TASKS.yaml from before the revert -- that entry is stale and should be trimmed when se9-commands-loud closes out, but was left untouched here since Se9CommandsFinalize owns that task.
created_at: "2026-07-09T06:14:09Z"
---

## Summary

Opened PR #366 (swallowed/9-import-loud, base master) for the se9-import swallowed-errors slice. commands/import.go: extracted statImportSourceCandidate so processImportCandidate's source os.Stat distinguishes a real error (warned + counted as skipped) from legitimate os.IsNotExist absence (silent no-op, unchanged). Added isJSONHookSyntaxError (errors.As *json.SyntaxError) and wired it into all 4 canonical hook-bundle readers (Copilot/Cursor/Codex/ClaudeCompat) so a genuinely corrupt JSON file warns loudly while still returning false; ordinary not-a-hook-bundle content stays silent. commands/import_plugins.go: same treatment on loadImportedPackagePluginManifest's json.Unmarshal. Updated existing InvalidJSON/InvalidJSONReturnsFalse tests (Cursor x2, Codex, Copilot, plugin manifest) to assert the new warning via the existing captureRelinkStdout helper; added TestCanonicalHookBundleOutputsFromClaudeCompatFile_InvalidJSON (no prior coverage) and two new processImportCandidate tests (generic + canonical hook-bundle path) using testutil.MakeDirUnreadable to force a real Stat error. go build/test/vet all green. Per-file coverage: import.go 95.70% (clears 95% gate outright), import_plugins.go 93.44% (pre-existing [legacy-tail] allowlist entry in scripts/coverage-exceptions.txt, refreshed from 93.39% -> 93.44%; the touched function loadImportedPackagePluginManifest is itself 100% covered, remaining gap is unrelated pre-existing debt). Disjoint file set from the sibling se9-commands-loud PR (kg/lifecycle/workflow/remove/refresh) per the fresh-off-master decomposition. PR left open, not merged.

## Integration Notes

Files import.go/import_test.go/import_plugins.go/import_plugins_test.go are still listed in se9-commands-loud's write_scope in TASKS.yaml from before the revert -- that entry is stale and should be trimmed when se9-commands-loud closes out, but was left untouched here since Se9CommandsFinalize owns that task.
