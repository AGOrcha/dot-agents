# P6 Payout Backfill Contract

- task: `p6-payout-backfill`
- requirements: R9.2, DC11; resolves Q7
- dependencies: `p5-e2e-integration`, `p3b-companion-discipline-skills`
- downstream target: `/Users/nikashp/proj-docs/payout`

## Goal

Migrate the known active payout consumer deliberately and prove that the
new starter-shipped discipline assets and hook behavior function in its
existing loop workflow.

> **Revised 2026-06-30 — scope expanded to the shipped config-architecture.**
> Beyond the hooks + `isp`/`loop-worker` adoption below, fold payout onto the
> config-architecture-impl benefits now merged (L1-L3): the unified-config
> **profile** model (selector-merge; also the substrate `app-type-profiles`
> builds on — payout's `po-core-api-se` multi-mode is the canonical multi-profile
> case), `init --from` distributable-config-manifest adoption (L2), the
> home-config machine-local split + `kind:project-set` identity registry (L3,
> keep binding-table/cache out of the synced tree, rebind by identity), and
> layered lock-pinning with content-driven (not clock-driven) staleness. Set
> payout up to receive **L4 multi-harness** (descriptor schema, owner-gated,
> upcoming) without rework — but do not block on it.

## Grounded Starting State

- Payout has active workflow state under `.agents/active/` and canonical
  workflow plans under `.agents/workflow/plans/`.
- Its managed rules at `~/.agents/rules/payout/agents.md` already direct
  implementation commits through `/iteration-close`.
- Its `.agentsrc.json` currently declares `iteration-close` and
  `delegation-lifecycle`, omits `isp` and `loop-worker`, and sets
  `"hooks": false`; adoption therefore requires a configuration migration,
  not merely observational readback.
- This makes payout an adoption target with live behavior to protect, not a
  passive consumer that can be assumed correct after refresh.

## Execution Boundary

Payout-side writes are not performed as incidental edits in the dot-agents
implementation worktree. Execute them in an explicitly authorized payout
workspace or delegated migration slice. Authorized canonical migration
targets are:

```text
/Users/nikashp/proj-docs/payout/.agentsrc.json
/Users/nikashp/proj-docs/payout/.agents/workflow/plans/loop-discipline-stop-hooks-backfill/
```

Generated platform link/config changes produced by `da refresh` are
verification output; do not manually rewrite them around the generator.
This task's dot-agents-owned readback artifact is:

```text
.agents/history/loop-discipline-stop-hooks/payout-migration-readback.md
```

## Required Migration Readback

The migration record must identify:

- pre-migration resolved skill and hook paths;
- the `.agentsrc.json` delta enabling hooks and declaring the required
  discipline skills;
- the refresh/install or explicit migration operation used;
- post-migration resolution of `iteration-close`, `isp`, `loop-worker`,
  `agent-handoff`, and `delegation-lifecycle`;
- materialized gate hooks and platform outputs relevant to payout;
- confirmation that existing active plans, merge-back, verification, and
  fold-back artifacts were not removed or overwritten;
- one representative payout loop dry run or sandboxed hook invocation.

## Acceptance

- Payout migration is explicitly executed and evidenced after P5 passes.
- Failures or intentional project-local overrides are recorded for P7
  rather than silently replaced.

## Out of Scope

- Migrating every downstream project; P7 owns the broader sweep.
