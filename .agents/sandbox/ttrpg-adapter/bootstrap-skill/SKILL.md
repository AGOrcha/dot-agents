---
name: ttrpg-campaign-bootstrap
description: |-
  Populate a fresh TTRPG campaign graph from session-log markdown.
  Parses session logs in a conventional format, extracts character /
  location / faction / event / plotline mentions, writes them to the
  active KG scope chain, and registers environmental predicates.
type: skill
status: stub
---

# TTRPG Campaign Bootstrap

**Status:** stub — implementation pending first real DM dogfood

This skill is the bootstrap entry point for the `ttrpg-campaign` graph
backend adapter (`../schema.yaml`). The graph backend adapter contract
(§11 of `app-type-profiles/design.md` per
`.agents/proposals/graph-backend-adapter-contract.md`) requires a
`bootstrap_skill` ref; this is that skill for the TTRPG adapter.

## Inputs

- `--session-logs <path>`: directory containing session-log markdown files
  (one per session, named `session-NN.md`)
- `--scope <repo|user|team>`: KG scope to write into (default: `repo`)
- `--dry-run`: parse and report counts without writing

## Expected session-log format

```markdown
# Session 12 — 2026-04-30

**In-world:** 14th of Therendor, Year 1492 → 17th of Therendor

## Characters present
- Kaelith (PC, half-elf rogue)
- Brogan (PC, dwarf cleric)
- Lord Astaroth (NPC, faction: Crimson Hand)

## Location
Whispering Pines, north of Neverwinter

## Events
- Kaelith and Brogan ambushed by Crimson Hand scouts
- Lord Astaroth revealed himself as the hidden architect
- The party recovered the Shard of Therandil

## Plotlines advanced
- "The Shadow Conspiracy" — advanced (Astaroth revealed)
- "The Shard Hunt" — resolved (Shard recovered)
```

## Output

Writes to the configured scope:

- `session_log` note for the session
- `character` notes for each named participant (created if new, refreshed
  if existing)
- `event` notes for each event in the session
- `location` notes referenced
- `plotline` notes mentioned, with status updates per the "Plotlines
  advanced" section
- Edges: `present_at`, `occurred_in`, `member_of`, `advances`, `resolves`

Materialized views per the schema's planner hints are computed after the
final write, not per-event (avoids N rebuilds during a multi-event
session import).

## Implementation notes for the eventual real version

- Use the existing markdown parser shared with research/article-extract
- Character disambiguation should fall back to "needs GM confirmation"
  rather than auto-merging when names are ambiguous
- Plotline status updates should fire `derivation_mutation` drivers on
  any events that previously cited the plotline
- Bootstrap is idempotent — re-running with the same session logs is a
  no-op
- For the dogfood phase, ship a `--from-roll20-export` flag that converts
  Roll20 campaign exports to the expected session-log format
