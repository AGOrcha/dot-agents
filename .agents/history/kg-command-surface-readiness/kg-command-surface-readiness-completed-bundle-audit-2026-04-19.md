# KG Command Surface Readiness Completed-Bundle Audit

Date: 2026-04-19

Scope: spec-vs-implementation audit of the live `kg-command-surface-readiness` workflow bundle
after it was marked `completed` in canonical state. This audit follows the method in
[`completed-plan-audit-analysis`](../../workflow/specs/completed-plan-audit-analysis/design.md).

## Verdict

`reopen-recommended`

The current codebase appears to have implemented most of the bundle's intended readiness fixes:

- `kg code-status --json` now emits JSON
- change/impact commands now do freshness pre-checks
- `--require-graph` is present
- `graph_state` is present in JSON output
- MCP review-context freshness/files handling has been improved and tested

So the bundle no longer looks blocked on the exact gaps described in the original audit docs.

The remaining problem is no longer just an evidence gap:

- an active fold-back still records the graph-warm transaction defect
- a fresh isolated manual repro on 2026-04-19 confirmed the defect still exists on current code
- the canonical bundle had been marked `completed` even though a clean-home `kg build` still fails

That means the bundle needs a narrow reopen, not a broad rollback of all KG surface work. The
follow-on scope is now captured canonically as `kg-fresh-build-transaction-fix`.

## Spec Anchors

- [PLAN.yaml](../../workflow/plans/kg-command-surface-readiness/PLAN.yaml)
- [TASKS.yaml](../../workflow/plans/kg-command-surface-readiness/TASKS.yaml)
- [design.md](../../workflow/specs/kg-command-surface-readiness/design.md)

## Implementation Anchors

- [commands/kg/sync_code_warm_link.go](../../../commands/kg/sync_code_warm_link.go)
- [commands/kg/cmd.go](../../../commands/kg/cmd.go)
- [internal/graphstore/mcp_server.go](../../../internal/graphstore/mcp_server.go)
- [internal/graphstore/crg.go](../../../internal/graphstore/crg.go)

## Verification Anchors

- [impl-results.md](./impl-results.md)
- [kg-freshness-audit.md](../../../docs/research/kg-freshness-audit.md)
- [kg-change-impact-audit.md](../../../docs/research/kg-change-impact-audit.md)
- [kg-advanced-surfaces-audit.md](../../../docs/research/kg-advanced-surfaces-audit.md)
- [kg-mcp-transport-audit.md](../../../docs/research/kg-mcp-transport-audit.md)
- [graph-warm-build-transaction-defect.yaml](../../active/fold-back/graph-warm-build-transaction-defect.yaml)

## Confirmed Findings

### 1. `kg code-status --json` is now implemented

The freshness audit said `kg code-status --json` still rendered prose. Current source now emits
JSON when command JSON mode is active:

- [sync_code_warm_link.go:192](../../../commands/kg/sync_code_warm_link.go:192)
- [kg_test.go:557](../../../commands/kg/kg_test.go:557)

This is a concrete example where the research note is now stale relative to live implementation.

### 2. `kg changes` and `kg impact` freshness/readability fixes are present

The earlier audit said these were missing:

- freshness pre-checks
- `--require-graph`
- `graph_state` in JSON output
- human advisory when results are empty

Current implementation now contains all of those surfaces:

- [sync_code_warm_link.go:228](../../../commands/kg/sync_code_warm_link.go:228)
- [cmd.go:237](../../../commands/kg/cmd.go:237)
- [cmd.go:251](../../../commands/kg/cmd.go:251)
- [sync_code_warm_link.go:251](../../../commands/kg/sync_code_warm_link.go:251)
- [sync_code_warm_link.go:306](../../../commands/kg/sync_code_warm_link.go:306)
- [sync_code_warm_link.go:456](../../../commands/kg/sync_code_warm_link.go:456)

The corresponding tests are also present:

- [kg_test.go:702](../../../commands/kg/kg_test.go:702)
- [kg_test.go:733](../../../commands/kg/kg_test.go:733)
- [kg_test.go:757](../../../commands/kg/kg_test.go:757)
- [kg_test.go:791](../../../commands/kg/kg_test.go:791)
- [kg_test.go:822](../../../commands/kg/kg_test.go:822)
- [kg_test.go:846](../../../commands/kg/kg_test.go:846)

So `kg-change-impact-audit.md` is now mostly a historical audit document rather than a description
of still-missing implementation.

### 3. MCP parity fixes also appear landed

The MCP audit identified `handleGetReviewContext` as the primary gap. Current source now has:

- readiness guarding for unbuilt / busy graphs
- `DetectChangesOptions{Files: req.Files}`
- tests covering busy and ready behavior

Direct evidence:

- [mcp_server.go:497](../../../internal/graphstore/mcp_server.go:497)
- [mcp_server.go:513](../../../internal/graphstore/mcp_server.go:513)
- [mcp_server.go:533](../../../internal/graphstore/mcp_server.go:533)
- [mcp_server_test.go:304](../../../internal/graphstore/mcp_server_test.go:304)

The implementation-results artifact also records this work as completed:

- [impl-results.md](./impl-results.md:1)

### 4. The advanced-surfaces decision looks intentionally audit-only

`kg flows`, `kg communities`, and `kg postprocess` were primarily decision/doc work. The plan task
notes said no structural implementation was needed there.

That still appears coherent with current bundle status:

- [TASKS.yaml:82](../../workflow/plans/kg-command-surface-readiness/TASKS.yaml:82)

## Confirmed Drift / Evidence Gaps

### 1. Research docs overstate current missing behavior

The research notes still describe several items as absent which are now visibly present in source:

- `kg code-status --json`
- `--require-graph`
- `graph_state`
- MCP freshness guard

This is documentation drift rather than product drift.

Direct evidence:

- [kg-freshness-audit.md:65](../../../docs/research/kg-freshness-audit.md:65)
- [kg-change-impact-audit.md:157](../../../docs/research/kg-change-impact-audit.md:157)
- [kg-change-impact-audit.md:163](../../../docs/research/kg-change-impact-audit.md:163)
- [kg-mcp-transport-audit.md:136](../../../docs/research/kg-mcp-transport-audit.md:136)

### 2. The graph-warm transaction defect is still reproducible

There is an active fold-back saying a fresh isolated `kg build` failed with:

- `sqlite3.OperationalError: cannot start a transaction within a transaction`

and that this should be treated as working-scope product debt rather than deferred heavy-lane debt:

- [graph-warm-build-transaction-defect.yaml](../../active/fold-back/graph-warm-build-transaction-defect.yaml:1)

That defect was then reproduced directly on current code in a fresh temp setup with isolated
`HOME`, `AGENTS_HOME`, and `KG_HOME`:

```text
./bin/dot-agents kg build --repo .
sqlite3.OperationalError: cannot start a transaction within a transaction
```

This means the bundle's old `completed` status was materially wrong for the clean-home build path.

### 3. The fold-back had been routed to a completed audit task

The active fold-back had been attached to `kg-freshness-audit`, not to an implementation task or a
follow-on task:

- [graph-warm-build-transaction-defect.yaml](../../active/fold-back/graph-warm-build-transaction-defect.yaml:3)

That was structurally awkward once the bundle was marked `completed`. This is now corrected by the
new canonical follow-on task:

- `kg-fresh-build-transaction-fix`

## Open Questions

1. Is the root cause inside the CRG Python store transaction lifecycle, the Go bridge invocation
   pattern, or an interaction between postprocess/warm-side effects?
2. Should this defect stay as a narrow follow-on inside `kg-command-surface-readiness`, or does it
   justify a more explicit CRG bridge/runtime stabilization bundle if deeper failures emerge?

## Required Follow-Up

1. Fix `kg build` so a fresh isolated `KG_HOME` no longer fails with the nested-transaction error.
2. Add regression coverage for a fresh-home build path so repo-local persisted graph state cannot
   hide the failure.
3. Keep the reopened scope narrow: do not reopen already-landed change/impact or MCP parity work.
4. After the fix lands, update stale research notes so they stop describing already-landed fixes as
   missing and record closure evidence for the fresh-build defect.
