# KG/CRG-aware bundle authoring

**Status:** observation → proposal draft
**Source:** PR #40 (seam-interface-di) loop runs, 2026-05-20
**Type:** project-local — targets the loop-worker/delegation-lifecycle skill family in this repo

## The recurring failure

Across three loop-worker runs on PR #40 (`seam-review-convert`, `seam-add-convert`, `seam-atomic-convergence`), the same shape of surprise surfaced: a `decision_lock` in the bundle mandated a function-signature change, but the bundle's `write_scope` did not include all the files that would need a corresponding mechanical update. Each worker correctly noticed the build break and made the minimal cross-file edits to keep the package compiling, then documented the scope deviation in its merge-back. But the deviation should not have happened — the bundle author (the orchestrator) had the analytical tools to know.

Concretely:
- **review-convert** declared `write_scope = [review.go, review_test.go]` and `forbidden_scope = [seams_test.go, ...]`. Reality: nine review-related test functions in `seams_test.go` had signatures that broke the moment `runReviewApprove`/`runReviewReject`/`captureProposalRollback` gained the `reviewDeps` parameter. The worker had to delete those nine functions to compile.
- **add-convert** declared the same shape; nine more `seams_test.go` tests broke for the same reason (the KG-config helpers, `RemoveError`, `LstatFails`, `SymlinkBranch`, `RestoreLegacyResourceFile_NoMapping`, `TestRunAdd_ConfigLoadError`).
- **atomic-convergence** declared a 9-file `write_scope` covering the obvious mechanical surface (refresh.go, sync.go, review.go, refresh_test.go, etc.) — but three additional forbidden-scope test files (`doctor_repair_e2e_test.go`, `import_test.go`, `review_test.go`) had stale call sites of `restoreFromResources`, `runImportFromRefresh`, and `runRefresh` respectively, all needing single-argument mechanical updates.

## Root cause

Bundle authoring is happening from a human-readable map of "which files describe this change" rather than a tool-computed impact radius of "which files would the change touch." We have exactly the tools that close this gap — the knowledge graph (KG) and code review graph (CRG) — and they are already wired into this repo (`mcp__code-review-graph__get_impact_radius_tool`, `mcp__code-review-graph__semantic_search_nodes_tool`, etc., per the session-start preamble: *"Knowledge graph is available. Prefer MCP tools ... over manual file scans to save tokens."*). The orchestrator is the right place to use them, before issuing each bundle.

## Proposal

Add a pre-bundle KG/CRG analysis step to the delegation-lifecycle and loop-worker skill family. When the bundle author drafts a `decision_lock` that changes a function signature, the orchestrator must:

1. Query CRG (`get_impact_radius_tool`) for callers of the function whose signature is changing — up the call chain to a configurable depth (typical default: 2 hops, since direct callers will need the argument addition; transitive callers usually don't unless they're constructing the type).
2. Cross-check the returned set against the proposed `write_scope`. If files are in the impact radius but not in `write_scope`, EITHER:
   - Expand `write_scope` to include them (preferred — the worker has authority over the full surface)
   - OR add an explicit `mechanical_signature_carve_out: [<paths>]` entry to the bundle that permits the worker to make single-line caller updates in those files without violating `forbidden_scope`
3. Persist the impact-radius query result as a sidecar (`<bundle_id>.impact.yaml`) so the worker can verify completeness during execution.

For non-signature changes (e.g. behavioral logic edits, comment changes, data shape extensions), the impact-radius step is optional — the existing scope-declaration discipline is usually sufficient.

## Why this is a planning gap, not a worker gap

The three workers in PR #40 handled the surprise correctly (build → fix the minimal mechanical breakage → document → move on). The bundle author (orchestrator, in three different sessions) was the consistent failure surface. A skill that bakes the impact-radius query into bundle authoring is the only durable fix.

## Open questions

- Does this become a new `bundle-authoring` skill, or a step inside the existing `delegation-lifecycle` / `isp` skill flows?
- For non-signature decision_locks (behavioral changes), is there value in running CRG `semantic_search_nodes_tool` to surface files that mention the same concept by name — or is that signal-to-noise too low to be worth a default step?
- What's the right depth default for the impact-radius query? Direct callers (1 hop) catches the bulk of the recurring problem; 2 hops catches the cases where a wrapper function exists.

## What this is NOT

- It is not a request to loosen `forbidden_scope` semantics. The `forbidden_scope` discipline is doing its job — it surfaces scope errors loudly. The fix is to author better bundles, not to permit broader edits.
- It is not a request to require KG/CRG analysis for every bundle. Single-file conversions (the install.go pilot, e.g.) don't need it. The trigger is "decision_lock changes a function signature."

## References

- Evidence: the four loop-worker merge-backs produced during PR #40's seam-DI conversion (install, review, add, atomic-convergence). These artifacts are written by workers into the worktree's `active/delegation/` while the bundle is in flight; they reach `history/loop-agent-pipeline/delegate-merge-back-archive/<date>/` only when the plan archives. PR #40's plan has not archived as of this writing, so the raw merge-back text lives in `.claude/worktrees/seam-di/.agents/active/delegation/` (pre-merge) and will resurface in `history/loop-agent-pipeline/` post-archive.
- `seam-interface-di-migration` spec (on master via PR #42) — the migration these workers were implementing.
- Existing CRG MCP tools listed in the session-start preamble (`semantic_search_nodes_tool`, `query_graph_tool`, `get_impact_radius_tool`, `get_review_context_tool`).
