# KG Command Surface Readiness — Canonical Plan

**Plan ID:** kg-command-surface-readiness
**Status:** active
**Spec:** [design.md](../../specs/kg-command-surface-readiness/design.md)
**Upstream dependency:** [graph-bridge-command-readiness](../graph-bridge-command-readiness/PLAN.yaml) (completed)
**Downstream consumer:** [planner-evidence-backed-write-scope](../planner-evidence-backed-write-scope/PLAN.yaml)

---

## Why this exists

The graph-bridge resurrection work proved that `workflow graph query` and `kg bridge query`
can return useful results for planner-facing intents. That was the minimum viability bar.

But the broader `kg` command surface is referenced by planning skills, review skills, hooks,
and the MCP server. If those commands are not operationally trustworthy, every surface that
consumes them inherits false assumptions. The symptom is plans that describe graph-backed scope
derivation as a capability when in practice the commands silently return empty results or fail
with undocumented preconditions.

This plan resolves that gap command-by-command before the planner-evidence work tries to
automate scope derivation at the command level.

---

## Key decisions and invariants (do not reopen without a fold-back)

1. **Python CRG bridge is the current code-graph path.** The scope of this plan is making
   that path operationally honest, not replacing it with a Go-native implementation. Any
   work toward Go-native graph building belongs in a separate plan.

2. **Audit before implement.** Each implementation task is explicitly gated on its audit
   counterpart. Implementations that skip the audit phase will over-scope.

3. **Empty result ≠ no impact.** The most dangerous current failure mode is `kg changes`
   or `kg impact` returning empty when the graph is stale. The planner evidence system
   cannot use these commands safely until that distinction is visible in output and exit codes.

4. **MCP parity is a decision, not an automatic implementation target.** Some MCP tools
   should remain bridge-backed for now. The audit produces a parity matrix with explicit
   keep/implement/defer decisions per tool; implementation only covers "implement now" entries.

5. **Slices 1 and 2 unblock the downstream planner plan.** Slices 3 and 4 are product-debt
   resolution that can run in parallel after Slice 1 lands.

---

## Task sequence

```
kg-freshness-audit
  └─ kg-freshness-impl  ──────────────────────────────────────┐
       └─ kg-change-impact-impl                               │
                                                              ▼
kg-change-impact-audit ─► kg-change-impact-impl         kg-mcp-transport-impl
                                                              ▲
kg-advanced-surfaces-audit (no impl, audit + doc only)        │
                                                              │
kg-mcp-transport-audit ────────────────────────────────────────┘
```

`kg-freshness-audit` is the only entry point. All other tasks have at least one dependency.
`kg-advanced-surfaces-audit` has no blocking implementation task — the output is a doc decision only.

---

## Relationship to planner-evidence-backed-write-scope

- `kg-freshness-impl` completion is the readiness gate that unblocks
  `planner-evidence-backed-write-scope/derive-scope-command`.
- `kg-change-impact-impl` completion is what makes excluded_path validation in evidence
  sidecars trustworthy enough to be machine-checked.
- Slices 3 and 4 do not block any planner-evidence tasks.

---

## Out of scope

- Go-native graph building (replacing Python CRG)
- Redesigning the MCP server architecture
- Changes to `workflow graph query` or `kg bridge query` (owned by graph-bridge-command-readiness)
- Any changes to TASKS.yaml schema or workflow orchestration primitives
