---
name: "kg-brief"
description: "Phase 1 molecule for kg-ideate. Produces the shared briefing artifact by scanning the KG, research corpus, lessons, prior specs/plans, and optional impact radius before any spec or plan authoring begins."
argument-hint: "<topic-or-task-id>"
tier: molecule
calls:
  - kg-query
  - corpus-scan
  - context-scan
  - briefing-render
verifier: batch
review_gate: default
---

# KG Brief

`kg-brief` is the Phase 1 molecule in `kg-ideate`. It produces the single shared
briefing artifact consumed by downstream spec, plan, and handoff phases. This phase
grounds the work; it does not write spec or plan files.

## Workflow

1. **KG traversal**
   Load -> `instructions/kg-queries.md`
   Query the KG for prior decisions, entities, and optional impact radius grounded in the
   topic. Prefer the CLI; fall back to MCP only when needed.

2. **Research corpus scan**
   Load -> `instructions/corpus-scan.md`
   Search the research corpus for related proposals, themes, and article evaluations that
   should shape the briefing.

3. **Lessons and prior artifact overlap**
   Load -> `instructions/context-scan.md`
   Scan lessons plus prior specs and plans for terminology, scope, or task-pattern overlap.

4. **Render the briefing**
   Load -> `templates/briefing-output.md`
   Produce the structured briefing block. Present it as the self-contained context artifact
   for downstream phases and delegated planners.
