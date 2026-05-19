# Graph Bridge Command Readiness

Status: Active

## Outcome

Turn the reopened graph-bridge usability audit into a canonical workflow plan that makes:

- `dot-agents workflow graph query`
- `dot-agents kg bridge query`

dependable enough for downstream planning and write-scope derivation.

For planning, "dependable enough" does not only mean symbol lookup returns something. The bridge surface has to support both planner lanes:

- **scope lane:** symbol, caller, callee, test, and impact evidence that justifies `write_scope`
- **context lane:** plan/decision memory queries that let planners emit `decision_locks`, `required_reads`, verification targets, and contradiction checks instead of leaving those decisions to workers

## Why This Exists

Historical work landed routing, docs, and some skill wiring, but it did not prove repo-ready usefulness. The current repo still shows two concrete gaps:

1. `workflow graph query` can fail without repo-local bridge configuration.
2. `kg bridge query` can return weak or empty answers for normal workflow-package symbols even when the code graph is otherwise healthy.

That means graph-backed planning should stay blocked on readiness work instead of assuming the bridge is already operational.

## Scope

This plan owns the near-term, executable readiness work:

- reproduce and document current failures on real repo fixtures
- define the product contract for missing-config behavior and planner-facing query expectations
- implement the highest-value bridge, config, and query fixes
- add durable smoke verification that proves usefulness rather than just command routing

Planner-facing usefulness should be judged against real execution-contract authoring needs, not only raw command output. A planner should be able to derive both:

- a bounded candidate scope
- a concise context pack that removes major ambiguity before fanout

Related analysis that should stay adjacent but separate:

- [KG Command Surface Readiness](../../specs/kg-command-surface-readiness/design.md)
- [Go Native Code Graph Analysis](../../specs/go-native-code-graph-analysis/design.md)

## Exit Condition

The plan is complete when planner-facing graph queries are trustworthy enough that specs such as [Planner Evidence-Backed Write Scope](../../specs/planner-evidence-backed-write-scope/design.md) no longer need to treat graph-backed derivation as blocked on legacy readiness work, and when planners can use the bridge to author execution contracts rather than only coarse `write_scope` guesses.
