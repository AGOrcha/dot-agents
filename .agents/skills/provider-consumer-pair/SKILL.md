# Skill: Provider-Consumer Pair Implementation

## Purpose
When two phases/waves must be implemented together because one provides an interface the other consumes (e.g., KG bridge provider + workflow bridge consumer), use this workflow to keep both sides in sync without circular blocking.

## Pattern

### 1. Read both plans in parallel
```bash
# Read both in the same LLM turn using parallel tool calls
Read(.agents/active/kg-phase-5-bridge-readiness.plan.md)
Read(.agents/active/wave-5-knowledge-graph-bridge.plan.md)
```

### 2. Identify the interface boundary
Determine which side defines the contract and which consumes it:
- **Provider** = the system that defines the query/response contract (e.g., KG Phase 5's `KGAdapter` interface)
- **Consumer** = the system that uses the contract (e.g., Wave 5's `GraphBridgeAdapter`)

### 3. Implement provider first
- Define interface types, response envelopes, adapter health structs
- Wire in to command registration
- Compile before moving to consumer

### 4. Implement consumer independently where possible
- If the consumer has its own adapter (e.g., `LocalGraphAdapter` doing direct FS scan), implement it without importing the provider
- This avoids circular imports and keeps subsystems decoupled

### 5. Write tests for each side separately, then one integration test
- Provider tests: interface compliance, query dispatch
- Consumer tests: config load, adapter health, query results
- Integration test: end-to-end from consumer intent → provider query → results

## Gotchas
- Don't import `commands/kg.go` functions from `commands/workflow.go` — they're in the same package so it works, but keep the mental model of them as separate concerns
- If both sides define the same struct (e.g., `GraphBridgeResult` in workflow and `GraphQueryResult` in kg), that's intentional — they're different layers with different field sets
- Contract YAML files (bridge-contract.yaml) serve as machine-readable documentation; always generate them in `setup` so they're available before first use
