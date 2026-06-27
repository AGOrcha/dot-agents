# Step 4: Empirical pass — run the scientific-method spine

For every fork tagged **empirically-testable** in step 3, settle it by running the
**scientific-method spine** to gather faithful evidence in the fork's evidence form — and
survive the fidelity gate. **Read `instructions/fidelity-gate.md` before trusting any result
here.** A result that has not passed the gate does not exist as far as the spec is concerned.

## The spine (this is the order; the rest of this file expands each step)

1. **Frame** competing hypotheses + state *what evidence would discriminate* between them.
2. **Pre-register** predictions + power/discrimination criteria BEFORE gathering evidence.
3. **GATE 1 — design audit (PRE-run)**: an independent brain tries to invalidate the *design*
   before the spend (`cross-brain.md`). Fix and re-audit until sound.
4. **Gather faithful evidence** in the domain-appropriate form, with a real negative control.
5. **Power + regime check**: can this evidence discriminate, and is it measuring where the
   effect lives?
6. **GATE 2 — results audit (POST-run)**: an independent brain tries to invalidate the
   *conclusion* (`fidelity-gate.md` step + `cross-brain.md`).
7. **Null is first-class**; iterate or extract the sound subset.

## The evidence form is domain-general (not Go-prototype-only)

The spine is invariant; the **evidence form** comes from the evidence-form table in
`classify-forks.md`. Instantiate the form for THIS fork:

| Fork type | Gather as | Negative control |
|---|---|---|
| Algorithmic / software | throwaway prototype (own `go.mod`) + tests | the broken impl must fail the test |
| Agent-behavior | controlled eval across conditions | a condition engineered to fail must fail |
| Research / factual | corpus + KG evidence (`article-extract` → ingest → KG) | a disconfirming source actively sought |
| Product / UX | user data, A/B, instrumented usage | the variant predicted worse must read worse |
| Ops / infra | a controlled measurement / load test | a known-bad config must trip the metric |

The software-prototype guidance below is the *most common* instantiation; read it as the
worked example of step 4, not the only shape. For an agent-behavior eval, "own `go.mod`"
becomes "a scripted condition matrix"; for a factual fork, it becomes "an ingested corpus +
the KG query that discriminates." The fidelity requirements (faithful inputs, negative
control, real execution, no hidden losses, independent audit) hold for **every** form.

## Pre-registration (required — commit BEFORE gathering)

Before any evidence is gathered, write down — and commit to the fork's sidecar — the
**pre-registration**:

- the competing **hypotheses** and the **predictions** each makes (what the evidence should
  show if H1 holds vs if H2 holds);
- the **discrimination criterion** — the specific observation that would falsify each option
  (the negative control made concrete);
- the **power criterion** — the sub-ceiling baseline that proves the instrument *could* have
  shown a difference (so a null reads as "no effect," not "task too easy / measurement maxed
  out"), and the **regime** the evidence must sample (where the effect actually lives).

Pre-registration exists to stop **post-hoc goalpost-moving**: you cannot retro-fit the
success criterion to whatever the run happened to produce. A result that wasn't predicted
in advance is a hypothesis for the *next* cycle, not a confirmation of this one.

## Build a self-contained prototype (software-fork instantiation)

- **Own `go.mod`, isolated from the coverage gate.** Put each prototype under its own
  module (e.g. `.agents/active/experiments/<fork-id>/`) with its own `go.mod` so it is
  excluded from the repo's test/coverage gate and never ships in product code. It is
  throwaway evidence, not a feature.
- **Run the real scenario.** Model the actual data, schema, enums, and the specific
  failure case — not a simplified shape (the fidelity gate enforces this).
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

## GATE 1 — design audit, BEFORE the run (the cheaper, higher-leverage gate)

Once the pre-registration + experiment bundle exist but **before any evidence is gathered**,
run GATE 1: an independent cross-harness brain whose job is to **invalidate the DESIGN**
(`cross-brain.md`, the two-gate section). It checks: are the hypotheses falsifiable, the
predictions pre-committed, the power/regime adequate, the negative control real, the inputs
faithful, the evidence form appropriate? If it breaks the design, fix and re-audit — *do not
spend the run yet*.

This gate exists because the expensive error class is the **wrong experiment**, not the wrong
conclusion: a flawed design that only gets caught after the run has already burned the spend
(the v1/v2/v3 arc each paid for a full run a pre-run audit would have flagged). A pre-run
design audit is cheap and catches that class before any cost. GATE 1 is mandatory for any
experiment whose run is non-trivial to produce.

## Relay discipline — how workers hand back (load-bearing)

Every hand-back from a dispatched worker (and every hop → hop in a multi-hop chain) MUST be
**structured / pointer-based: the artifact path(s) + a structured constraint/decision
checklist — NOT a retold prose summary.** This is not a style preference; it is an
evidence-backed rule. Lossy summary relay verifiably drops **non-reconstructable** detail and
that loss **reaches the deliverable** (in a measured case, a summarised hand-back compressed
away an arbitrary schema choice → terminal artifact 16/16 → 13/16; verbatim relay was lossless
across 8 hops). State this rule in every worker bundle — it binds every subagent and hop you
spawn.

If an experiment's own SUBJECT is multi-hop delegation, it must be **driver-orchestrated
hop-by-hop** (a fresh top-level `Agent` per hop, relay via on-disk file), never recursively
nested: nested `Agent`-tool delegation collapses past ~hop 4 *on the current harness* (this
ceiling is per-harness — re-assess for any new harness), and bundles for
such experiments must forbid CLI-driver improvisation.

## Power + regime check (on the gathered evidence)

Before trusting any verdict, confirm the run could actually have discriminated:

- **Power** — does the **sub-ceiling baseline** the pre-registration named actually exist in
  the data? If the easy/control condition already maxes out the instrument (e.g. a 97.6%
  ceiling with no headroom), a null is uninformative — the task was too easy, not the effect
  absent. Internal rigor ≠ a powered experiment.
- **Regime** — is the evidence sampled **where the effect lives**? A perfectly clean run in
  the wrong regime (the few-KB band when the effect is a lost-in-the-middle phenomenon)
  measures the wrong thing. Regime validity is separate from instrument correctness.

A failed power or regime check sends the experiment back to design (re-pre-register, re-GATE-1),
or the result is recorded as an explicit **null** (see below) — never laundered into a verdict.

## On return — GATE 2 (results audit, POST-run)

1. Read the worker's report **and** its fidelity self-audit as two separate things.
2. Re-run the negative control yourself — confirm the falsifying case actually fails.
3. Run the **power + regime check** above.
4. Hand off to GATE 2 — the fidelity gate (`fidelity-gate.md`) + the independent
   cross-harness audit whose job is to **invalidate the CONCLUSION** (`cross-brain.md`, the
   two-gate section): does the evidence actually support the claim, what confounds remain,
   where is it over-claimed? Re-run until it passes.
5. Only then record the result as evidence for the fork. Surface the result honestly,
   including any field that could not round-trip, any case that broke, or any **null** (an
   effect that couldn't be induced or a regime that couldn't be reached) — those are findings,
   never things to drop for a green check. A null is a first-class result.

## Output

Per empirically-testable fork: the audited verdict (which option holds, or an explicit null),
a pointer to the evidence (prototype module, eval matrix, or ingested corpus + KG query), the
pre-registration, the GATE 1 design-audit verdict, and the GATE 2 fidelity audit that cleared
it — collected into the fork's **evidence sidecar**. At converge (step 6) the decision +
sidecar pointer are RETURNED (to `spec-scaffold` when dispatched, or to a spec-drafting step
when standalone); `ideation-cycle` does not write the spec prose itself.
