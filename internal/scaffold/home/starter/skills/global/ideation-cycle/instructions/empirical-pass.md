# Step 4: Empirical pass

For every fork tagged **empirically-determinable** in step 3, settle it with a prototype
that runs the REAL scenario and asserts the hypotheses — and that survives the fidelity
gate. **Read `instructions/fidelity-gate.md` before trusting any result here.** A prototype
result that has not passed the gate does not exist as far as the spec is concerned.

## Build a self-contained prototype

- **Own `go.mod`, isolated from the coverage gate.** Put each prototype under its own
  module (e.g. `.agents/active/experiments/<fork-id>/`) with its own `go.mod` so it is
  excluded from the repo's test/coverage gate and never ships in product code. It is
  throwaway evidence, not a feature.
- **Run the real scenario.** Model the actual data, schema, enums, and the specific
  failure case — not a simplified shape (the fidelity gate enforces this; see step 5
  there).
- **Assert the hypotheses.** Each fork's options become assertions: the prototype proves
  which option holds under the real scenario. Deterministic asserts; for concurrency,
  `-race` × many randomized iterations.

## Dispatch the authoring; the driver orchestrates

The ideation driver does **not** hand-build the prototype. Dispatch it to a subagent with
a bundle that states, up front (Hybrid orchestration model):

- the fork(s) and the exact hypotheses to assert;
- the fidelity directive verbatim — **faithful inputs + a negative control + real
  execution + no hidden losses + a fidelity self-audit in the report** (see
  `fidelity-gate.md`);
- the real schema/data to model and where it lives;
- the isolation requirement (own `go.mod`, out of the coverage gate).

The driver's leverage is framing the hypotheses and running the gate — not typing the
experiment. Brief the worker so the fidelity directive propagates down the delegation
chain (it binds every subagent).

## Relay discipline — how workers hand back (load-bearing)

Every hand-back from a dispatched worker (and every hop → hop in a multi-hop chain) MUST be
**structured / pointer-based: the artifact path(s) + a structured constraint/decision
checklist — NOT a retold prose summary.** This is not a style preference; it is an
evidence-backed rule. Lossy summary relay verifiably drops **non-reconstructable** detail and
that loss **reaches the deliverable** (evidence sidecar
`evidence/depth-degradation-dogfood.md` v3 family-2: a summarised hand-back compressed away an
arbitrary schema choice → terminal artifact 16/16 → 13/16; verbatim relay was lossless across
8 hops). State this rule in every worker bundle — it binds every subagent and hop you spawn.

If an experiment's own SUBJECT is multi-hop delegation, it must be **driver-orchestrated
hop-by-hop** (a fresh top-level `Agent` per hop, relay via on-disk file), never recursively
nested: nested `Agent`-tool delegation collapses past ~hop 4 *on the current harness* (sidecar
v2/v3 fold-back; this ceiling is per-harness — re-assess for any new harness), and bundles for
such experiments must forbid CLI-driver improvisation.

## On return

1. Read the worker's report **and** its fidelity self-audit as two separate things.
2. Re-run the negative control yourself — confirm the broken impl actually fails.
3. Hand off to the fidelity gate (step 5 of `fidelity-gate.md`): the independent
   cross-harness audit whose job is to invalidate the experiment. Re-run until it passes.
4. Only then record the result as evidence for the fork. Surface the result honestly,
   including any field that could not round-trip or case that broke — those are findings,
   never things to drop for a green check.

## Output

Per empirically-determinable fork: the audited verdict (which option holds), a pointer to
the prototype module, and the fidelity audit that cleared it — collected into the fork's
**evidence sidecar**. At converge (step 6) the decision + sidecar pointer are RETURNED (to
`spec-scaffold` when dispatched, or to a spec-drafting step when standalone); `ideation-cycle`
does not write the spec prose itself.
