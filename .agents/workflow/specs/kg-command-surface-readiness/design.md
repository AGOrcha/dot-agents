# KG Command Surface Readiness

**Status:** design artifact that inventories the broader `dot-agents kg` readiness surface and should feed follow-on canonical plan work after the narrower [Graph Bridge Command Readiness](../../plans/graph-bridge-command-readiness/PLAN.yaml) plan stabilizes planner-facing queries.

## Goal

- extend the earlier graph-bridge resurrection work to the rest of the `dot-agents kg` command surface
- separate native KG commands from code-graph commands that still depend on the Python `code-review-graph` path directly or indirectly
- identify which readiness gaps matter for near-term planner and orchestrator work and which are separate product debt

## Why This Exists

The previous resurrection note focused on:

- `workflow graph query`
- `kg bridge query`

That was too narrow. The broader `kg` command family is now referenced by:

- planning ideas
- review skills
- build-graph skill
- hooks
- MCP server behavior

If those commands are not operationally trustworthy, planning and orchestration surfaces inherit false assumptions.

## Surface Inventory

### Native KG and note-oriented commands

These appear primarily Go-native and are not inherently blocked on Python CRG:

- `kg setup`
- `kg health`
- `kg ingest`
- `kg queue`
- `kg query`
- `kg lint`
- `kg maintain`
- `kg sync`
- `kg warm`
- `kg link`
- workflow-side note bridge via `.agents/workflow/graph-bridge.yaml`

### Code-graph commands with direct Python CRG dependency

These call `graphstore.NewCRGBridge(...)` and shell out to `code-review-graph` today:

- `kg build`
- `kg update`
- `kg code-status`
- `kg changes`
- `kg impact`
- `kg flows`
- `kg communities`
- `kg postprocess`

### Surfaces with indirect Python CRG dependency

These depend on code-graph data or bridge components still populated through the Python path:

- `kg bridge query` for code-structure intents
- `workflow graph query` for code-structure intents
- `kg serve` MCP tools through `internal/graphstore.MCPServer`
- graph-aware skills and hooks

## Key Findings

1. The repo has two different graph stories: a native KG note/query lifecycle and a Python-backed code-graph build/query lifecycle.
2. The command tree hides that split behind one `kg` namespace, which made older phase-complete statements look more complete than they really were.
3. The narrower bridge resurrection work covers planner-facing query readiness, but not the operational trustworthiness of build/update freshness, code-status provenance, change and impact usefulness, advanced graph queries, or MCP parity.
4. For near-term planner work, the most relevant non-bridge commands are `kg build`, `kg update`, `kg changes`, and `kg impact`, because they shape freshness and evidence quality for downstream queries.

## Recommended Breakdown

### Slice 1: code-graph freshness and provenance

- audit `kg build`, `kg update`, and `kg code-status`
- define what "graph is ready" means for downstream planner and review work
- add smoke verification that proves a repo can get from source tree to usable code graph

### Slice 2: change and impact trustworthiness

- audit `kg changes` and `kg impact`
- verify output shape and usefulness on real repo fixtures
- identify where empty results mean "no impact" versus "graph or query limitation"

### Slice 3: advanced graph surfaces

- audit `kg flows`, `kg communities`, and `kg postprocess`
- decide whether they are ready for agent consumption or should remain expert surfaces

### Slice 4: MCP and transport surface

- audit `kg serve` and MCP tool parity
- decide whether MCP should remain bridge-backed for now or become subordinate to a more native path

## Dependency Relationship

- This design is a sibling to the executable [Graph Bridge Command Readiness](../../plans/graph-bridge-command-readiness/PLAN.yaml) plan.
- The bridge readiness plan remains the immediate blocker for planner automation.
- This broader KG command analysis should feed a later canonical plan for the rest of the code-graph command surface after the narrower bridge/query readiness work lands.
