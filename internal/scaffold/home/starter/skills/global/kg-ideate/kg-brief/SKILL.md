---
name: kg-brief
description: |-
  Phase 1 briefing producer for the kg-ideate pipeline. Queries the knowledge graph,
  research corpus, and lessons index to produce a structured briefing block — the single
  shared artifact consumed by Phases 2–4 (spec-scaffold, plan-scaffold,
  staged-execution-handoff) and by a spawned subagent planner. Use when starting a new
  spec/plan, when the user asks what we know about a topic, or when prepping context for
  delegation before any downstream scaffolding begins.
argument-hint: "<topic>"
tier: molecule
calls:
  - kg-query
  - corpus-scan
  - context-scan
  - briefing-render
verifier: batch
review_gate: default
---

# kg-brief — Briefing Producer

KG/research/lessons traversal that produces the shared briefing block consumed by all
downstream kg-ideate phases and by a spawned subagent planner.

## Phase 1 — Briefing Producer

1. **KG traversal**
   Load → `instructions/kg-queries.md`
   Run semantic_search_nodes_tool + query_graph_tool in parallel against the topic.
   Pull decision-type nodes, entities, and contradiction nodes. If topic touches
   existing code, run get_impact_radius_tool (this also seeds Phase 3 write-scopes).

2. **Research corpus scan**
   Load → `instructions/corpus-scan.md`
   Search research/articles-evaluation-kg-and-adjacent.md §C for applicable proposals.
   Pull top-5 §A entries and §B themes by relevance. Note research gaps.

3. **Lessons and spec overlap**
   Load → `instructions/context-scan.md`
   Preflight: verify .agents/workflow/specs/ exists before scanning — note absence, do not error.
   Read .agents/lessons/index.md; pull full lesson files that match the topic.
   List .agents/workflow/specs/ — flag prior specs that share scope or terminology.

4. **Produce briefing**
   Load → `templates/briefing-output.md`
   Render the structured briefing block and present to user before proceeding.
   Do not write any spec or plan file in this phase.
   The briefing is the single shared artifact consumed by Phases 2–4 (and by a spawned
   subagent planner — see the kg-ideate compound skill's "Subagent-planning path" section).
