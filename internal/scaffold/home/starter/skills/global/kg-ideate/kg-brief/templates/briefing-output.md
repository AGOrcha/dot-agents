# briefing-output — Briefing Block Template

Briefing block template (single shared artifact consumed by Phases 2–4 and by a spawned
subagent planner):

```
## KG Briefing: <topic>
Generated: <date>

### Prior Decisions (<N> found)
<For each: KGNote ID or §C proposal ID — decision summary — rationale source>
[none] if empty

### Research Findings (<N> relevant)
<§A article title — key insight — §B theme it supports>
[none] if empty

### Contradictions (<N> found)
<Contradiction description — competing positions — source refs>
[none] if empty  |  [adapter-absent] if contradicting_claims query unavailable

### Applicable Lessons (<N> found)
<Lesson name — relevant pattern>
[none] if empty

### Gaps (<N> identified)
<Gap description — why it matters for this spec/plan>
[none] if empty

### Prior Spec / Plan Overlap
<Spec or plan ID — scope overlap description>
[none] if empty

### Impact Radius
<files/functions/modules — seeds Phase 3 write-scopes>
[none] if topic did not name code
```
