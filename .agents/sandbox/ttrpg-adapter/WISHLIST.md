# DM Wishlist — Queries the Named Templates Don't Cover

Each entry: a query a DM asked for that the existing named queries
(`./queries.yaml`) can't express. This list is the input signal for the
v1 → v1.5 trigger criteria in §11.7 of the graph backend adapter contract.

## How to log an entry

```markdown
## YYYY-MM-DD — <campaign name> — <DM name>

**Query (plain English):**
<what the DM asked for>

**Why named queries don't cover it:**
<which existing template comes closest, and what's missing>

**Escape hatch coverage (§11.6):**
- Additional named query: <yes/no — would shipping a new template work?>
- Materialized view: <yes/no — could bootstrap precompute this?>
- Adapter MCP server: <yes/no — would an ad-hoc query tool over KG storage work?>

**Severity:** <nice-to-have | useful | blocker>

**Wishlist priority:** <low | medium | high>
```

---

## Trigger evaluation reminder

Promote to v1.5 (richer pattern-match DSL in `da` core) only when:

- ≥2 distinct adapters (TTRPG + a second domain) independently request a
  feature requiring composition the adapter author cannot anticipate
- Those features cannot be served by any of the three §11.6 escape hatches

Until both conditions fire: solve via additional named queries (option 1),
materialized views via bootstrap skill (option 2), or a dedicated adapter
MCP server (option 3).

---

## Entries

_(none yet — first DM dogfood pending)_
