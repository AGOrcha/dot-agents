# Workflow: Provider Consumer Pair

Use this skill when two phases or waves must move together because one side defines an interface and the other consumes it.

## Pattern

1. Read both plans in parallel.

   ```text
   Read(.agents/active/kg-phase-5-bridge-readiness.plan.md)
   Read(.agents/active/wave-5-knowledge-graph-bridge.plan.md)
   ```

2. Identify the interface boundary.

   Determine which side is the provider and which side is the consumer:

   - Provider: defines the query or response contract
   - Consumer: uses that contract

3. Implement the provider first.

   Typical provider work:
   - define interface types
   - define response envelopes
   - define adapter health structs
   - wire command registration
   - compile before moving on

4. Implement the consumer independently where possible.

   If the consumer can use its own adapter without importing the provider directly, do that first to avoid circular imports and tighter coupling.

5. Test in three layers.

   - provider tests for interface compliance and query dispatch
   - consumer tests for config load, adapter health, and query results
   - one integration test from consumer intent to provider result
