# Gotchas: Delegation Lifecycle

Common failure points:

## Write Scope Discipline

- Declare write scope at fanout time and treat it as immutable. Changing scope informally later defeats the conflict model.
- Prefer directory paths such as `commands/` rather than ad hoc file globs for common cases. The overlap checks are built around prefix containment.

## Coordination Drift

- The system records scope and status, but it cannot enforce write discipline at edit time. Sub-agents still need to honor the scope contract.
- If a sub-agent needs parent attention, set `pending_intent`. Do not rely on the parent to infer the need from prose alone.

## Cleanup And Completion

- After merge-back, the parent still needs to advance the canonical task status to `completed`.
- Orphaned delegations remain active until explicitly resolved. If a sub-agent stops without merge-back, clean up the delegation rather than leaving stale active state behind.
