# P7 Legacy Override Migration Contract

- task: `p7-legacy-override-migration`
- requirements: R9.3, DC12
- dependencies: `p6-payout-backfill`, `p1d-claude-lifecycle-parity`

## Goal

Finish the plan by reconciling downstream project-specific copies of the
discipline skills with the starter-shipped canonical versions.

## Why Last

Overrides may represent intentional downstream policy. Payout must first
demonstrate the canonical adoption path, and platform event parity must be
settled, before a broader migration can distinguish stale copies from
necessary variants.

## Inventory and Migration Record

Write:

```text
.agents/history/loop-discipline-stop-hooks/downstream-override-migration.md
```

Inventory managed projects for overrides of:

```text
iteration-close
isp
loop-worker
agent-handoff
delegation-lifecycle
```

For each located override, record its owner, differences from the canonical
starter asset, migration decision, execution evidence, and readback result.

## Migration Rules

- Migrate an override to starter inheritance only when it contains no
  intentional project-specific policy.
- Retain documented variants rather than erasing local policy.
- Treat downstream filesystem changes as separately authorized work in each
  target workspace; the dot-agents repository owns the migration inventory
  and canonical source only.

## Acceptance

- Payout appears as the first completed entry.
- Each discovered managed-project override is marked migrated, retained with
  rationale, or blocked with an explicit follow-up.
- No downstream override is deleted merely because its canonical base now
  exists.
