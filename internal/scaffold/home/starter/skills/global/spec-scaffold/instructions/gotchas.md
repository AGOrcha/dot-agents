# Gotchas: spec-scaffold

Common failure points:

## Standalone Invocation Needs a Real Briefing, Not an Assumed One

- This skill is invoked standalone "when a briefing already exists," but
  `gap-conversion.md` and `decision-review.md` both assume there IS a Phase 1 briefing
  block in hand to review/convert. If it's invoked with only a bare topic and no actual
  `kg-brief` output, there is nothing to run steps 5–6 against — don't invent gaps or
  prior decisions to fill the gap; either fetch/re-run the briefing first or say so.

## The Contradiction-Framing Adapter Check Lives Outside This Skill

- Step 7 is adapter-conditional on `kg-brief`'s `kg-queries.md` preflight
  (`contradicting_claims` availability) — that preflight is NOT re-run here. If
  `spec-scaffold` is invoked standalone without ever having run `kg-brief`'s preflight in
  this session, there is no adapter-availability signal; default to the
  competing-decisions fallback rather than guessing the adapter is present.

## Spec-Tier Discipline Is a Hard Boundary, Not a Suggestion

- No file paths, function names, or task breakdowns belong in `design.md` — the
  boundary is stated in both this skill's own description and in
  `templates/spec-output.md` independently, which is itself a signal it's a recurring
  drift point. An agent trying to be "helpful" by sketching implementation details here
  duplicates `plan-scaffold`'s job and pollutes the spec tier.

## The `.agents/workflow/specs/` Preflight Is Easy to Skip

- Step 9 requires creating `.agents/workflow/specs/` if it doesn't exist before writing
  — skipping straight to a write call on an assumed-existing directory will fail on a
  fresh repo. Also never fall back to the bare `workflow/specs/` path; the canonical
  root warning appears in three separate files in this family precisely because it's a
  repeated mistake.
