---
name: spec-scaffold
description: |-
  Phase 2 of kg-ideate. Consumes the Phase 1 KG briefing and guides spec authoring:
  prior decision review → gap conversion to open questions → contradiction framing →
  done criteria → writes .agents/workflow/specs/<id>/design.md. Invoke standalone
  when a briefing already exists. Spec tier only — no file paths, function names, or
  task lists (those belong in plan-scaffold).
argument-hint: "<topic>"
tier: molecule
calls:
  - decision-review
  - gap-conversion
  - contradiction-framing
  - done-criteria
  - spec-write
verifier: batch
review_gate: default
---

# Spec Scaffold

Phase 2 of the `kg-ideate` compound skill. Consumes the structured KG briefing produced
by `kg-brief` (Phase 1) and guides creation of a spec at
`.agents/workflow/specs/<id>/design.md`.

May be invoked standalone when a briefing already exists (e.g. when resuming after Phase 1
completed in a prior session, or when delegating Phase 2/3 to a planning subagent).

## Steps

5. **Prior decision review**
   Load → `instructions/decision-review.md`
   For each prior_decision in briefing: present it and ask whether it still applies
   or needs revisiting in the new spec. Record responses.

6. **Gap → open questions**
   Load → `instructions/gap-conversion.md`
   For each gap in briefing: propose it as a candidate open question.
   Confirm which to include; discard the rest.

7. **Contradiction → decision points** (adapter-conditional — see kg-brief's kg-queries.md preflight)
   Load → `instructions/contradiction-framing.md`
   For each contradiction in briefing: frame it as a decision the spec must explicitly
   resolve. Draft the decision statement; get user input before locking.
   Degrades gracefully: if the adapter lacks `contradicting_claims`, fall back to the
   competing-decisions treatment (two decision nodes with conflicting rationale). The
   rest of the pipeline does not depend on this step.

8. **Done criteria draft**
   Load → `instructions/done-criteria.md`
   Propose 3-5 verifiable done criteria derived from briefing context. Revise with user.

9. **Write spec**
   Load → `templates/spec-output.md`
   Once scaffolding steps are confirmed, write `.agents/workflow/specs/<id>/design.md`.
   Preflight: verify `.agents/workflow/specs/` exists before writing — create the
   directory if absent.
   Structure: problem statement, decisions with rationale, requirements (behavioral),
   open questions, done criteria, deferred items.
   Never include file paths, function names, or task breakdowns — those belong in the
   plan tier (plan-scaffold, Phase 3).
