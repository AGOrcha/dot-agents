# Pre-validate bundle write_scope via code-review-graph + grep

## Pattern

When authoring a `workflow fanout` bundle's `write_scope`, do NOT hand-write the file list from the task notes alone. Pre-validate with the `mcp__code-review-graph__*` tools + grep to catch callers that would force a fold-back later. The t13 fold-back chain (3 worker iterations: original t13 → t13a → t13a v2) burned 3 worker spawns because the original bundle missed 13 test-file callers and the first split missed `commands/root.go`. All preventable with a pre-flight scope walk.

## Root cause

Workers correctly refuse to partial-land per `[[no-lazy-allowlist-tech-debt]]` + `[[validate-bundle-against-head]]`. The cost is a full spawn + closeout cycle per missed caller. The fix is upstream — author bundles against the actual import/caller graph, not against the plan's static write_scope list.

## Rule (per task before `da workflow fanout`)

1. **Enumerate target symbols** with `mcp__code-review-graph__query_graph_tool` pattern `file_summary` on every file in the proposed write_scope. This surfaces symbols that aren't obvious from the file name (e.g. `commands/install.go` has 29 functions, not just `runInstall`).

2. **Find textual callers** via grep for each unexported symbol:
   ```bash
   grep -rln "<symbol>\b" <relevant-dirs>/ --include="*.go"
   ```
   Use word-boundary (`\b`) to avoid matching substrings. This is the **most reliable** method for Go unexported names — the code graph's `callers_of` underreports for two specific reasons:
   - cobra `RunE` lambdas don't trace as CALLS edges in tree-sitter parsing
   - test files calling unexported symbols via type aliases (`stdInstallDeps`) often land as Test nodes without a CALLS-back edge to the definition
   Pair the grep with `query_graph_tool` `tests_for` when the symbol is exported — that one IS reliable.

3. **Subtract owned files** — the symbol's own file is in scope; the destination subpackage that re-exports it is the move target, not a caller.

4. **For exported renames or moves**, also run `query_graph_tool` `callers_of` with the **fully qualified** name (`/abs/path/file.go::Symbol`) — the bare name returns ambiguous matches across language ports.

5. **Bundle template guidance**: if a `write_scope` touches a `*EventName` mapper or `coverage_gap*_test.go` fixture, add the adjacent test files (`coverage_gap{,2,3,4,5}_test.go`) by default.

## Tool limits to remember

- `get_impact_radius_tool` with broad changed_files can emit 5MB+ output — narrow to ≤3 files at a time or use `query_graph_tool` per-symbol instead.
- `importers_of` on Go files returns 0 — Go imports packages, not files. Useless for cross-file caller discovery.
- The graph rebuilds on every commit (per CLAUDE.md), so the snapshot stays fresh, but check `list_graph_stats_tool` `last_updated` if you suspect drift.

## Cross-references

- `[[validate-bundle-against-head]]` — companion check: bundle's write_scope files must EXIST on HEAD
- `[[verify-task-status-vs-pr-history]]` — companion check: bundle's TASK may already be done
- `[[no-lazy-allowlist-tech-debt]]` — fold-back is the right move when scope expansion is required
