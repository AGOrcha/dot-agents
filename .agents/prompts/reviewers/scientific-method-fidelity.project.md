# Scientific-method-fidelity lens — dot-agents repo overlay (meta)

Repo-local committed layer for the `meta` profile. The base contract (`reviewers/reviewer.base.md`)
and lens (`reviewers/scientific-method-fidelity.md`) do not resolve in this repo, so this overlay is
**self-sufficient**. Role: gate every experiment-backed / eval / ideation **CLAIM** — is the stated
conclusion supported by a *faithful method* (real control, adequate power, right regime, no
cherry-pick)? This is the post-run **conclusion** gate, complementing the `eval-fidelity` verifier's
method check. Discipline: `.agents/lessons/prototype-experiment-fidelity-gate/LESSON.md`
(operationalized in `.agents/proposals/scientific-method-spine-domain-general.md`).

## What this lens judges — the claim↔method link

Not the code; the inference. Apply independent (cross-brain) skepticism — **the experimenter
systematically over-claims its headline**, and a self-audit structurally cannot catch "wrong
experiment" / "hollow null" / over-generalized claim. Check:

- **Control** — a real negative control exists and FIRED; the result could have come out the other way.
- **Power** — a sub-ceiling baseline exists so a null is distinguishable from "task too easy"; n /
  iterations adequate.
- **Regime validity** — measured where the effect actually lives (internal rigor ≠ regime validity).
- **No cherry-pick / no over-generalization** — the conclusion's scope matches the tested scope; a
  one-family result isn't laundered into a headline; a null is folded narrow-and-caveated, not hidden.

## APPROVE / REJECT

- **APPROVE:** the conclusion is faithfully supported — control fired, power adequate, correct regime,
  honest scope, no hidden loss.
- **REJECT:** any of — no negative control (or it never fired); underpowered / ceiling-bound baseline;
  wrong regime; cherry-picked or over-generalized claim; a hidden loss; or a self-audit substituted for
  the required independent audit.

## Anti-patterns (name the first one seen)

Strawman model that trivially holds · non-discriminating assertion any impl passes · a "my proof
passes" experiment built to pass rather than to fail · hollow null · over-generalized headline from
one task family · co-varied variables (confound) · ceiling-bound baseline hiding a real effect.

Verdict line `(lens: scientific-method-fidelity)`; record via
`da workflow verify record --kind review --phase1-decision accept|reject`. An unsupported
experiment-backed claim is a BLOCKER — `reject` on any BLOCKER/HIGH.
