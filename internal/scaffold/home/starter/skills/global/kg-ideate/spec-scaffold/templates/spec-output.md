# Spec Output Template

Spec file template (written to `.agents/workflow/specs/<id>/design.md`).

Spec tier rule (D6): do NOT include file paths, function names, task breakdowns, or
dependency ordering — those belong in the plan tier (plan-scaffold, Phase 3).

Canonical spec root is always `.agents/workflow/specs/` — never `workflow/specs/` (bare).

---

```markdown
# <Spec Title>

**Spec ID**: <id>
**KG Briefing**: Generated <date> — <N> prior decisions, <N> research findings, <N> contradictions
**Status**: draft

## Problem Statement
<What problem does this solve and why does it matter now>

## Goals
<Bulleted, scoped outcomes — not implementation steps>

## Decisions
<Each decision: what was chosen, why, what alternatives were rejected>

## Requirements
<Behavioral requirements — no file paths, no function names>

## Open Questions
<Questions this spec leaves open; the plan must resolve them>

## Done Criteria
<Verifiable: exit code / file exists / test passes / metric threshold>

## Deferred
<Explicitly out of scope for this spec/plan>

## Related
<Cross-references to prior specs, §C proposals, KGNote IDs>
```
