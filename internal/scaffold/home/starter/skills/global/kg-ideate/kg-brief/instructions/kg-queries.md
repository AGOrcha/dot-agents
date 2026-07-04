# kg-queries — Phase 1 KG Traversal Rules

Rules for Phase 1 KG traversal:

- **CLI-first**: prefer `da kg query "<topic>"` (deterministic, intent-aware) over MCP tools.
  Fall back to MCP tools only when the da CLI is unavailable or returns no results.
- When using MCP: run semantic_search_nodes_tool and query_graph_tool in parallel.
- Filter query_graph_tool results to NoteType in [decision, entity] only.
- **Contradiction handling is adapter-conditional** (and gates ONLY Phase 2 step 7 — the
  rest of the pipeline ships without it):
  Before Phase 2 step 7, check whether the active profile's adapter ships a
  `contradicting_claims` named query. Preflight: run
  `da graph query --list-queries` (or inspect the adapter schema) — if
  `contradicting_claims` is present, contradiction handling is enabled for this session.
  If absent, fall through to competing-decisions treatment (two decision nodes with
  conflicting rationale). The `dotagents-builtin:graph/citation@^1.0` adapter ships
  `contradicting_claims` and can be used today for citation-backed projects.
- query_graph_tool degrades to generic search for unrecognized intents — if the intent
  field is unsupported, treat the result as semantic search output, not as a structured
  decision list. Label it accordingly in the briefing.
- If semantic results return > 10 nodes, narrow to top 5 by relevance score.
- get_impact_radius_tool: only call if topic explicitly names a file, function, or module.
  Its output also seeds Phase 3 write-scopes (instructions/plan-scaffolding.md step 12).
- If no KG nodes match, note "KG has no prior decisions on this topic" — do not fabricate.
- When the active profile selects `dotagents-builtin:graph/citation@^1.0`, prefer
  structural traversal via the adapter's named queries (`claims_citing_source`,
  `claims_supported_by_source`, `contradicting_claims`) over semantic search for
  citation-bearing topics. Per the v1 DSL, patterns the adapter doesn't ship as named
  queries go through adapter escape hatches (spec §8.5): additional named queries,
  materialized views, or an adapter-owned MCP server.
