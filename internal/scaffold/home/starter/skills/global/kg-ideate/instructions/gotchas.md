# Gotchas: KG-Ideate

Common failure points:

## Only Two Molecules Are Standalone-Invocable

- `spec-scaffold` and `plan-scaffold` are top-level `skills/global/` entries and can be
  invoked directly. `kg-brief` and `staged-execution-handoff` are nested under
  `kg-ideate/` on purpose — the skill loader scans each scope directory ONE level deep,
  so a molecule a second level down is never independently discoverable. Don't assume
  all four `calls:` entries are equally reachable outside this compound's own dispatch
  flow; two of them only run as a step of `kg-ideate` itself.
- If you need "just the briefing" or "just the handoff" without the rest of the
  pipeline, dispatch this compound's Phase 1 or Phase 4 directly rather than expecting
  `kg-brief`/`staged-execution-handoff` to be separately invocable skills.

## The D5 Concurrency Decision Has One Owner

- This compound (not `plan-scaffold`) owns the spec↔plan concurrency-mode call before
  dispatching Phase 3 — see "D5" above. `plan-scaffold`'s own step 10 restates the same
  heuristic because it can also run standalone. When dispatched from here, decide the
  mode ONCE and pass it down explicitly; don't let Phase 3 silently re-decide and
  potentially diverge from what was stated at dispatch time.

## The Phase 1 Briefing Must Be Produced Before Any File Is Written

- No spec or plan file may be written before the briefing block is rendered and
  presented to the user (Phase 1 rule, repeated in `kg-brief/SKILL.md`). Skipping
  straight to Phase 2 on a stale or assumed briefing breaks the "self-contained
  cold-start context" property the subagent-planning path depends on.

## Canonical Spec Root Is Always `.agents/workflow/specs/`

- Every downstream molecule's docs (Phase 2, `context-scan.md`, `spec-output.md`)
  repeat the same warning against the bare `workflow/specs/` path — that repetition is
  itself a signal this is a recurring mistake, not boilerplate. Don't let a delegated
  planner default to the unprefixed path.

## Phase 2 Step 7 Is Adapter-Conditional — the Preflight Lives in Phase 1

- Contradiction handling only applies if `kg-brief`'s adapter preflight
  (`da graph query --list-queries`) found `contradicting_claims`. If Phase 1 skipped or
  forgot that preflight, Phase 2 has no signal to fall back correctly and may attempt an
  unsupported named query instead of the competing-decisions fallback.
