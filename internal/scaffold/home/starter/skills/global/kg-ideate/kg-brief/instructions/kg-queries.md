# KG Queries

Rules for Phase 1 KG traversal:

- **CLI-first**: prefer `da kg query "<topic>"` over MCP tools. Fall back to MCP only when the CLI is unavailable or returns no useful results.
- When using MCP, run `semantic_search_nodes_tool` and `query_graph_tool` in parallel.
- Filter `query_graph_tool` results to `NoteType` in `[decision, entity]` only.
- **Contradiction handling is adapter-conditional** and gates only Phase 2 step 7:
  before that downstream step, preflight whether the active profile exposes a
  `contradicting_claims` named query via `da graph query --list-queries` or the adapter
  schema. If present, contradiction handling is enabled for that session. If absent, the
  pipeline falls back to competing decisions with conflicting rationale.
- If the active profile uses `dotagents-builtin:graph/citation@^1.0`, treat
  `claims_citing_source`, `claims_supported_by_source`, and `contradicting_claims` as the
  preferred structural path for citation-bearing topics.
- If `query_graph_tool` degrades to generic search for an unsupported intent, treat that
  output as semantic search rather than a structured decision list, and label it that way in
  the briefing.
- If semantic results return more than 10 nodes, narrow to the top 5 by relevance score.
- Call `get_impact_radius_tool` only when the topic explicitly names a file, function, or
  module. Its output seeds downstream write-scope derivation.
- If no KG nodes match, record `KG has no prior decisions on this topic` and do not
  fabricate prior context.
