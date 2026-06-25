# TTRPG adapter wishlist — §12 budget signals

Signals only. Per the Anti-scope, this list does NOT extend the §5.1 grammar.
It accumulates against the §12.1 5-point budget so a future v1.5 design review
binds to real demand, not speculation. Each entry records the weight class from
§12.1 and whether a v1 workaround exists (a clean workaround keeps it a
*richness* signal, not a blocker).

| # | Signal | §12.1 class | Weight | v1 workaround? |
|---|--------|-------------|--------|----------------|
| 1 | **Reverse / undirected traversal.** "Who knows character X?" and "factions allied with Y" are naturally reverse (`<-[:knows]-`) or undirected (`-[:allied_with]-`) reads. v1 is directed-positive-hop only (§5.1), so the author must store both edge directions at bootstrap or query from the other endpoint. | additional named query that's a workaround for a missing primitive | 1 | yes — emit symmetric edges at bootstrap, or MATCH from the other side |
| 2 | **`ORDER BY` / top-N.** "Most recent 3 events", "nearest locations first" want ordering. v1 RETURN has no `ORDER BY` (§5.1, confirmed §11.1 flows row). Author sorts client-side after the query returns. | materialized view to express what the DSL can't | 1 | yes — sort in the caller, or a view materializing row order (§11.1) |
| 3 | **Shortest-path / weighted path.** `connects_to` carries `travel_days` (weight_field); "cheapest route from A to B" wants Dijkstra returning the path. v1 returns end-node + hop_count only, no paths-as-objects (§5.2), so route reconstruction is impossible in-query. | additional named query workaround | 1 | partial — hop-count reachability works; actual route does not |
| 4 | **Aggregation beyond count/min/max.** "Average party size per session", "sum of item rarity weight" want `avg`/`sum`. v1's allowed-function set is `{coalesce, count, min, max, hop_count, STARTS_WITH}` (§5.1.1). | additional named query workaround | 1 | yes — return rows, aggregate client-side |

**Current budget total: 4 points** (threshold for v1.5 design review is 5;
implementation is 8 — §12.2). No blocker-class entries: every wishlist item has
a v1 workaround, so none qualifies for the §12.4 v1 fast-path. The TTRPG domain
fits inside v1 as authored — these are ergonomic richness requests, exactly the
class the accumulating budget is designed to meter.

## How these were surfaced

Each signal is a place where authoring `queries.yaml` (the 8 named queries)
forced a rewrite to stay in v1 grammar. They are documented in `queries.yaml`
inline (`exercises:` lines) and analyzed in `AUTHOR-UX-NOTES.md`. The live DM
dogfood (deferred) is expected to add domain-driven signals on top of these
contract-driven ones.
