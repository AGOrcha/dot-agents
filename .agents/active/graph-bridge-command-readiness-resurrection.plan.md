# Graph Bridge Command Readiness Resurrection

Status: active

## Goal

- Resurrect the unfinished practical work behind the graph command surfaces that the planning system now wants to depend on.
- Reconcile the gap between what the historical plans claimed was complete and what the commands actually do in this repo today.
- Make the dependency explicit so planning work does not proceed as if `workflow graph query` and `kg bridge query` are already dependable.

## Why This Exists

The new planning work wants to rely on:

- `dot-agents workflow graph query --intent ...`
- `dot-agents kg bridge query --intent symbol_lookup|tests_for|callers_of|impact_radius ...`

In practice, the current repo state showed:

1. `workflow graph query --intent plan_context ...` fails unless `.agents/workflow/graph-bridge.yaml` is configured.
2. `kg bridge query` currently returns no useful results for the workflow-package code anchors we tried, despite a healthy code graph.

That means the historical work closed the routing and template story before finishing command usefulness and readiness for downstream planning.

## Historical Lineage To Reopen

1. `.agents/history/workflow-automation-follow-on-spec/wave-5-knowledge-graph-bridge/wave-5-knowledge-graph-bridge.plan.md`
   - promised one deterministic graph-backed query contract for workflow context
2. `.agents/history/loop-orchestrator-layer/loop-orchestrator-layer.plan.md` / `TASKS.yaml`
   - Phase 5 completed routing of code-structure intents from `workflow graph query` to `kg bridge`
3. `.agents/history/crg-kg-integration/crg-kg-integration.plan.md` / `TASKS.yaml`
   - closed the CRG/KG integration and skill wiring story

## Diagnosis

The historical plans mostly landed these things:

- command routing
- docs
- skill templates
- MCP and hook integration

They did **not** fully prove these things:

- repo-ready default graph-bridge configuration for workflow-side queries
- useful symbol/test/caller lookup results for normal planner inputs
- smoke-level evidence that the bridge commands are trustworthy enough to drive planning boundaries

## Required Outcomes

1. Decide the product contract for `workflow graph query`:
   - should it require repo-local bridge config for workflow-note intents
   - should init/install scaffold that config by default
   - should the command degrade more helpfully when config is absent
2. Audit `kg bridge query` usability on real repo symbols and file anchors:
   - `symbol_lookup`
   - `tests_for`
   - `callers_of`
   - `impact_radius`
3. Add durable smoke verification for bridge usefulness, not just routing.
4. Only after those are true, treat graph-backed planning features as operational rather than speculative.

## Suggested Work Slices

1. Reproduce the current failures and capture exact command behavior.
2. Trace the storage/config path used by:
   - `kg code-status`
   - `kg bridge query`
   - `workflow graph query`
3. Decide whether the mismatch is:
   - missing repo bridge config
   - wrong graph home / store selection
   - weak query normalization
   - incomplete code-graph ingestion assumptions
4. Add focused tests and one or more repo-local smoke commands that prove useful results on known fixtures.

## Dependency Relationship

The planning spec at:

- `.agents/workflow/specs/planner-evidence-backed-write-scope/design.md`

is blocked on this resurrection work for any feature that depends on live graph-backed scope derivation or workflow-side context lookup.

Spec-only design can continue, but planner automation must not assume graph-readiness until this plan is closed.

Related follow-on analyses:

- [kg-command-surface-readiness-analysis.plan.md](/Users/nikashp/Documents/dot-agents/.agents/active/kg-command-surface-readiness-analysis.plan.md) — broader `kg` command audit beyond bridge/query
- [go-native-code-graph-analysis.plan.md](/Users/nikashp/Documents/dot-agents/.agents/active/go-native-code-graph-analysis.plan.md) — lower-priority plan framing for removing Python CRG reliance
