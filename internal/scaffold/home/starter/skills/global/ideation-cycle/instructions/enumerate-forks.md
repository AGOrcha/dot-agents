# Step 2: Enumerate the forks

A **fork** is one open design decision with more than one defensible answer. Turn the
unresolved rows of the step-1 briefing (the `kg-brief` block — its **Gaps**,
**Contradictions**, and any **Open Question** a prior spec left) into an explicit, numbered
list of forks. This is the agenda the rest of the cycle works through.

When dispatched from `kg-ideate` for a single named decision, the fork is already given —
enumerate only its sub-forks (the distinct sub-decisions hidden inside it), still anchored
to the briefing.

## What counts as a fork

- An **open question** a spec names but does not answer.
- A **deferred** item (`[DEFERRED]`, "out of scope for now") that is now in scope.
- A **`[PROPOSED]`** decision that needs ratification or empirical backing.
- A **recurring re-explanation** with no artifact — the thing that gets re-derived in
  every session because nobody wrote down the answer.
- A **hypothesis** a prototype could confirm or kill (these flow to the empirical pass).

## How to frame each fork

Each fork is one entry with:

- **ID + question** — a sharp, answerable question (not "how should config work?" but
  "does profile resolution layer scopes last-wins or first-wins when two profiles set
  the same key?").
- **Options** — the 2+ defensible answers, each one sentence.
- **Briefing anchor** — the `kg-brief` row it traces to (a Gap, a Contradiction, a prior
  Open Question, a `[PROPOSED]`): the prior decision it extends or the gap it fills. A fork
  with no anchor means you skipped grounding for it — go back to step 1.
- **Stakes** — what depends on getting this right (what breaks downstream if wrong).

## Frame against the baseline, not in a vacuum

The point of grounding first is that forks inherit prior thinking. State each fork as
"given <settled decision X>, the open question is Y" — not as a fresh blank-slate
question. This keeps the cycle additive and prevents re-opening settled ground.

## Output

A numbered fork list. Keep it tight — these are the *decisions*, not a task breakdown
(that is the plan's job, downstream). If the list is large, that is a signal the spec
was badly under-specified; surface that to the owner. The list feeds step 3, where each
fork gets a resolution route.
