# ISP Step 2: Select Work

`max_batch` from the eligible output is the authoritative fanout set — do not recompute it. The Go binary already ran conflict detection and chose the maximal non-conflicting subset.

## Parallel mode trigger

Both conditions must hold:
1. `len(max_batch) > 1`
2. No active delegation bundles currently open

If either fails, take only the first task in `max_batch` and run serialized.

## Scoped-plan constraint

Stay inside the scoped plan set passed to `workflow eligible --plan <scope>`. Do not jump to tasks in plans outside this scope.

## If eligible output is empty

`total_eligible == 0` means no actionable task remains:
- Check `workflow orient` for locked or paused state
- Surface the block clearly rather than searching outside scope

## Task readiness labels (from evidence_confidence)

| evidence_confidence | label |
|---|---|
| high / medium | delegation-ready |
| low | delegation-possible (flag thin context, suggest review) |
| none | cautious (recommend derive-scope first if KG is ready) |

These labels inform bundle context loading in step 4, not task selection order.
