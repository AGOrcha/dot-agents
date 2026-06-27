# Step 7: Dogfood / migration — close the loop

A ratified decision backed by an audited prototype is still a decision about a *modeled*
scenario. The final check is the live one: does the decision hold against the real
corpus? Dogfood it before the loop is closed.

## Validate against real data

- **Run the decision on the live data, not the experiment's data.** The prototype modeled
  the real schema; the dogfood pass runs the real *corpus*. A migration decision, for
  example, is validated by a **zero-diff migration** on the actual store — the migrated
  output must match the expected target exactly, or the diff is a finding.
- **The dogfood result is itself evidence.** An "already-settled (dogfood)" tag in step 3
  comes from exactly this kind of observation — a decision confirmed against real usage.

## Close the loop

- **If the dogfood pass is clean**, the decision is validated end to end:
  baseline → fork → audited experiment → ratified spec → confirmed on live data. Record
  the dogfood result alongside the spec evidence.
- **If the dogfood pass surfaces a gap**, it becomes a new fork. Route it back to step 2
  (enumerate) — or, if it is an observation from an execution loop, to `active/fold-back/`
  then `workflow fold-back create`, per the proposal-routing rules. Do not paper over a
  real diff to call the loop closed.

## Capture lessons

If the cycle corrected a wrong assumption — a prototype that proved the wrong thing, a
fork misclassified, a baseline decision missed — capture it in
`.agents/lessons/<name>/LESSON.md` (or extend an existing one) and update the index. The
fidelity gate (`fidelity-gate.md`) itself came from exactly this kind of close-out.

## The loop is closed when

- Every fork from step 2 is resolved (ratified, deferred, or kicked to a later cycle).
- Every empirical decision passed the fidelity gate.
- The spec is seeded/refined and handed to the execution half.
- The ratified decision is confirmed against real data (or its gap routed as a new fork).
