# KG Command Surface Readiness Analysis

Status: active

## Goal

- Extend the earlier graph-bridge resurrection work to the rest of the `dot-agents kg` command surface.
- Separate native KG commands from code-graph commands that still depend on the Python `code-review-graph` path directly or indirectly.
- Identify which readiness gaps matter for near-term planner/orchestrator work and which are separate product debt.

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

If those commands are not operationally trustworthy, the planning and orchestration surfaces will keep inheriting false assumptions.

## Surface Inventory

### Native KG / note-oriented commands

These appear primarily Go-native and not inherently blocked on Python CRG:

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

These depend on code-graph data or bridge components that are still populated through the Python path:

- `kg bridge query` for code-structure intents
- `workflow graph query` for code-structure intents (forwarded to `kg bridge query`)
- `kg serve` MCP tools through `internal/graphstore.MCPServer`
- review/build graph skills that assume code-graph freshness and useful query results

## Key Findings

1. The repo has two different “graph” stories:
   - native KG note/query/health lifecycle
   - Python-backed code-graph build/update/query lifecycle
2. The command tree hides that split behind one `kg` namespace, which makes historical “Phase complete” statements look more complete than they really are.
3. The previous resurrection covered the workflow and bridge query path, but not the operational trustworthiness of:
   - build/update freshness
   - code-status provenance
   - change/impact usefulness
   - flows/communities/postprocess behavior
   - MCP `kg serve` parity
4. For near-term planner work, the most relevant non-bridge commands are:
   - `kg build`
   - `kg update`
   - `kg changes`
   - `kg impact`
   because they shape the evidence story and the freshness of downstream bridge queries.
5. `kg flows`, `kg communities`, `kg postprocess`, and `kg serve` matter, but they are less immediate for evidence-backed `write_scope` than freshness and impact correctness are.

## Historical Lineage

Relevant prior work spans:

1. `.agents/history/knowledge-graph-subproject-spec/`
   - product/spec line for native KG and future CRG phases
2. `.agents/history/crg-kg-integration/`
   - chose the Python subprocess bridge for Phase B/C as the fast path
3. `.agents/history/loop-orchestrator-layer/`
   - wired workflow-side routing to `kg bridge`
4. `.agents/active/graph-bridge-command-readiness-resurrection.plan.md`
   - reopened the narrow bridge/query readiness problem

## Recommended Breakdown

### Slice 1: code-graph freshness and provenance

- audit `kg build`, `kg update`, `kg code-status`
- define what “graph is ready” means for downstream planner/review work
- add smoke verification that proves a repo can get from source tree → usable code graph

### Slice 2: change and impact trustworthiness

- audit `kg changes` and `kg impact`
- verify output shape and usefulness on real repo fixtures
- identify where empty/zero results mean “no impact” versus “graph/query limitation”

### Slice 3: advanced graph surfaces

- audit `kg flows`, `kg communities`, `kg postprocess`
- decide whether they are ready for agent consumption or should remain expert/escape-hatch surfaces

### Slice 4: MCP and transport surface

- audit `kg serve` and MCP tool parity
- decide whether MCP should remain bridge-backed for now or become subordinate to the Go-native plan

## Dependency Relationship

- This analysis is a sibling to [graph-bridge-command-readiness-resurrection.plan.md](/Users/nikashp/Documents/dot-agents/.agents/active/graph-bridge-command-readiness-resurrection.plan.md).
- The bridge resurrection plan is the immediate blocker for planner automation.
- This broader KG command analysis should feed a follow-on canonical plan that covers the rest of the code-graph command surface, especially build/update/changes/impact readiness.

## Immediate Recommendation

Treat the next practical work as:

1. finish the narrow bridge/query readiness resurrection first
2. then open a broader canonical plan for code-graph command readiness

Do not mix the lower-priority Go-native replacement effort into that immediate readiness fix.
