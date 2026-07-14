# Gotchas: plan-scaffold

Common failure points:

## Verification Must Trace to Done Criteria You Actually Have

- Step 13 requires every task's verification to trace back to the spec's done criteria
  (Phase 2 step 8). If this skill is invoked standalone "when a spec is already stable"
  but the done-criteria section wasn't actually read, it's easy to invent plausible but
  disconnected verification steps — `plan-scaffolding.md` explicitly forbids criteria
  that "contradict or ignore" the spec's own.

## `verification_required` Is a Boolean, Not a Text Field

- There is no free-text `verification:` key in `TASKS.yaml`. The verification intent
  goes in the `notes` block scalar; `verification_required` is only `true`/`false`.
  Inventing a `verification:` field or putting prose there instead of `notes` produces a
  task record that doesn't match the schema the rest of the tooling expects.

## The YAML Colon-Space Rule Bites `notes`/`rationale`/`summary`

- Any free-text field containing `: ` (a colon followed by a space) MUST use block
  scalar (`|-`) — plain-scalar `notes: Implements two-lens: phase 1 review gate` is a
  parse trap, not just a style preference. This is called out with an explicit
  WRONG/CORRECT pair in `plan-scaffolding.md` because it recurs.

## The Concurrency Decision Can Be Owned Upstream

- Step 10's heuristic is the SAME one `kg-ideate`'s own "D5" fork applies before
  dispatching here. When run as Phase 3 (compound-dispatched), the mode may already be
  decided — silently re-deciding independently risks contradicting what was stated at
  dispatch time. When invoked standalone, this skill owns the decision itself and must
  still record the mode + rationale in plan notes either way.
