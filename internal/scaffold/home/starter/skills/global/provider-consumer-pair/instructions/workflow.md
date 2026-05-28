# Workflow: Provider Consumer Pair

Use this skill when two phases or waves must move together because one side defines an interface and the other consumes it.

## Pattern

1. Read both canonical plans in parallel.

   Canonical plan files live under `.agents/workflow/plans/<plan-id>/`, never under `.agents/active/` (which is reserved for transient artifacts like delegation bundles, merge-back, fold-back, and iteration logs — see the workflow artifact model). The plan markdown sits alongside its `PLAN.yaml` + `TASKS.yaml`:

   ```text
   Read(.agents/workflow/plans/<provider-plan-id>/<provider-plan-id>.plan.md)
   Read(.agents/workflow/plans/<consumer-plan-id>/<consumer-plan-id>.plan.md)
   ```

   If both sides live in the same plan as distinct waves/phases, read the single plan plus its `TASKS.yaml`:

   ```bash
   da workflow plan show <plan-id>
   da workflow tasks <plan-id>
   ```

2. Identify the interface boundary.

   Determine which side is the provider and which side is the consumer:

   - Provider: defines the query or response contract (types, response envelopes, adapter health structs, command registration)
   - Consumer: uses that contract (config load, adapter calls, downstream rendering)

   If a machine-readable contract artifact exists (e.g. `bridge-contract.yaml`, an OpenAPI/JSONSchema doc, or a generated stub), treat it as the source of truth for the boundary — not the prose in either plan.

3. Pre-validate the write_scope on both sides against current HEAD.

   Bundles authored from stale task notes routinely miss callers — see the lesson cross-references in `instructions/gotchas.md`. Before fanning out either side:

   - Run a graph-backed enumeration of the symbols you expect to touch (`mcp__code-review-graph__query_graph_tool` with `file_summary` if available, or `da kg impact <file>`).
   - `grep -rln '<symbol>\b' <relevant-dirs>/` for each unexported provider symbol the consumer will reference.
   - Confirm every file listed in either side's proposed `--write-scope` actually exists on HEAD.

4. Implement the provider first.

   Typical provider work:
   - define interface types
   - define response envelopes
   - define adapter health / capability structs
   - wire command registration or transport plumbing
   - compile and unit-test before the consumer side fans out

5. Implement the consumer once the provider compiles.

   If the consumer can use a local adapter or a fixture against the contract artifact (rather than importing the provider directly), do that first to avoid circular imports and keep the architectural boundary intact.

6. Test in three layers.

   - provider tests: interface compliance, query dispatch, error envelopes
   - consumer tests: config load, adapter health, query results, downstream rendering
   - one end-to-end integration test from consumer intent to provider result — passing unit tests on each side is **not** enough for a paired-wave slice

## Fanout sequencing

When both sides need to be delegated:

1. Fanout the provider task first with its own `--write-scope` and `--feedback-goal "<contract surface that must compile>"`.
2. Wait for the provider's merge-back (or run `workflow delegation closeout --decision accept` on it) before fanning out the consumer.
3. The consumer fanout's `--prompt` should pin the provider's contract — the merged file paths and any decision locks from the provider's scope sidecar (`.agents/workflow/plans/<provider-plan-id>/evidence/<task-id>.scope.yaml`).

If both sides are too tightly coupled to serialize, fanout in parallel only when:
- the contract artifact is checked in first (single trivial PR), AND
- both `--write-scope` values are disjoint, AND
- a passing integration test exists on a feature branch the second-to-merge can rebase onto.

Otherwise serialize. The paired-wave coordination cost is lower than untangling a circular merge conflict.
