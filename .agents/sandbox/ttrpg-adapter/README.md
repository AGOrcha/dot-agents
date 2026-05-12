# TTRPG Campaign Graph — Dogfood Adapter

**Status:** sandbox / dogfood prototype
**Purpose:** stress-test the graph backend adapter contract (§11 of
`app-type-profiles/design.md`, per
`.agents/proposals/graph-backend-adapter-contract.md`) against a domain
that is genuinely combinatorial and has unanticipatable user queries.

This adapter is **not** part of `da` core. It is a public-source adapter
intended for tabletop RPG game masters to manage campaign continuity:
characters, locations, factions, events, plotlines, session logs.

---

## Why this exists

The compliance-register worked example in the spec demonstrates that v1's
named queries cover well-bounded enterprise domains. But "well-bounded" is
load-bearing — compliance has decades of canonical frameworks (NIST, SOC2,
HIPAA) shaping what the queries look like.

TTRPG worldbuilding has no such canon. DMs invent new relationships every
session ("the dragon is now a politician in the upper kingdom"). The
adapter author cannot anticipate the schema, let alone the queries.

This makes TTRPG the strongest test of:

1. **Schema flexibility** — does the adapter contract handle in-the-field
   schema additions, or do users need to fork the adapter?
2. **Query ceiling** — when do DMs hit a wall the named-query DSL can't
   express?
3. **Escape hatches** (§11.6) — do additional named queries, materialized
   views, and adapter-owned MCP servers cover the gap, or is v1.5's richer
   pattern-match DSL actually needed?

If TTRPG holds for ~3 months of real campaign use without forcing v1.5,
the v1 contract is validated for the open-ended-domain case.

---

## Files

```
.agents/sandbox/ttrpg-adapter/
├── README.md                      # this file
├── schema.yaml                    # note + edge types, planner hints, drivers
├── queries.yaml                   # 3 named queries beyond impact_radius
├── bootstrap-skill/
│   └── SKILL.md                   # placeholder for the bootstrap skill
└── WISHLIST.md                    # DM feedback log — query patterns that
                                   # the named templates can't express
```

---

## Distribution model (eventual)

When the adapter graduates from sandbox to public release:

1. Move to a public git repo: `github.com/<owner>/da-adapter-ttrpg`
2. Consumers add to their `.agentsrc.json`:
   ```json
   {
     "sources": [
       { "id": "ttrpg",
         "type": "git",
         "url": "git@github.com:<owner>/da-adapter-ttrpg.git",
         "ref": "main" }
     ]
   }
   ```
3. Profile that uses it (e.g., a `worldbuilding` profile) declares:
   ```yaml
   graph_backend: ttrpg:graph/campaign@^1.0
   ```
4. Bootstrap skill ships as an OCI artifact:
   `oci://ghcr.io/<owner>/da-pkgs/skill/ttrpg-campaign-bootstrap@^1.0`

For the dogfood phase, DM friends consume from the local sandbox path via
a `local` source.

---

## Dogfood feedback loop

DMs use the adapter during real sessions. Any time they want a query the
named templates can't express, they (or you on their behalf) log it to
`WISHLIST.md` with:

- The query in plain English
- Why the existing named queries don't cover it
- Whether one of the §11.6 escape hatches would solve it (additional
  named query, materialized view, MCP server)
- Severity (nice-to-have / blocker)

After ~3 months, review the wishlist:

- If it's mostly "needs more named queries the adapter could ship" → v1
  holds; just add the templates.
- If it's mostly "needs ad-hoc patterns the adapter author couldn't
  anticipate, and escape hatches don't cover" → v1.5 trigger fires.
- If schema additions dominate (new note/edge types DMs want) → that's a
  separate signal about adapter schema migration (Q7 in the spec).

---

## Status checklist

- [x] Sandbox directory created
- [x] Schema drafted (`schema.yaml`)
- [x] Three named queries drafted (`queries.yaml`)
- [ ] Bootstrap skill stubbed (`bootstrap-skill/SKILL.md`) — pending real
      session-log parsing logic
- [ ] First DM friend onboarded
- [ ] First wishlist entry from real campaign use
- [ ] 3-month review checkpoint
