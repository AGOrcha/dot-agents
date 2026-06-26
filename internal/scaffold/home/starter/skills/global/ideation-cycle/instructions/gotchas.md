# Gotchas: Ideation Cycle

The highest-signal failure points. Read before the empirical pass.

## A green prototype can prove the wrong thing

- This caught **both** KG prototypes (lease/conflict and projection). A prototype whose
  tests pass is NOT evidence until it clears the fidelity gate. The strawman model, the
  non-discriminating assert, the input that dodges the hard case, the fake mutation-check
  — all pass green and all prove nothing.
- The negative control is load-bearing: if no broken implementation fails the test, the
  test certifies nothing. Re-run the negative control yourself; don't trust the
  self-reported mutation-check.
- The independent cross-harness audit's job is to **invalidate**, not confirm. If it
  can't break the experiment, only then is the result usable. Reference:
  `prototype-experiment-fidelity-gate`.

## Don't turn the ideation driver into the prototype author

- The driver scopes, steers, and audits. Dispatch the prototype and spec authoring to
  subagents (Hybrid orchestration model). A driver that hand-builds the experiment loses
  the independence that makes the audit meaningful, and burns the context the framing
  needs.
- Propagate the fidelity directive into every worker bundle — it binds down the whole
  delegation chain, not just the top.

## Don't fake-empiricize a judgment call

- A prototype can only confirm what you set it up to assert. Pointing one at a trade-off
  fork produces a "proof" of whatever option you encoded — not a decision. Trade-offs go
  to cross-brain (step 5), not to a prototype.
- Conversely: don't send an empirically-determinable fork to opinion because prototyping
  felt like work. "Empirical where possible, else cross-brain" — the test is whether you
  can write a negative control.

## Don't reinvent `kg-brief` grounding

- The grounding stage IS the `kg-brief` molecule, RUN by `ideation-cycle`. If you find
  yourself grepping proposals/specs/lessons by hand in step 1, stop — that duplicates
  `kg-brief` and splits the briefing into two divergent shapes. Reuse the upstream briefing
  by artifact ONLY if it passes the freshness gate; otherwise run `kg-brief`. See
  `instructions/ground-via-kg-brief.md`.
- A fork raised without the prior thinking gets re-litigated from scratch — the exact
  "we keep re-explaining this" the skill exists to kill. Every fork must trace to a
  briefing row. If it doesn't, you skipped grounding.
- A `[PROPOSED]` decision is NOT settled — it still needs ratification in step 6. A
  *ratified* decision is a constraint — don't re-open it without new evidence.

## Don't author the surrounding spec when dispatched from `kg-ideate`

- Dispatched mode resolves ONE fork and returns the decision + evidence to `spec-scaffold`,
  which owns the spec. Writing the spec yourself in dispatched mode duplicates
  `spec-scaffold` and breaks the role-split. Only standalone mode seeds/refines a spec.
- Conversely, don't pull a briefing-decidable decision into a full empirical cycle. If the
  briefing already settles it, `spec-scaffold` authors it directly — `ideation-cycle` is
  for the HARD/OPEN forks only (see `instructions/composition.md` boundary test).

## Don't reuse a stale upstream briefing

- When dispatched from `kg-ideate`, you MAY reuse its Phase 1 briefing by artifact — but
  ONLY after the freshness gate (`inputs_digest` match AND no prior-fork mutation of a
  brief input). On any mismatch, re-run `kg-brief`. A stale brief poisons every fork
  downstream — re-running the leaf is cheap; silently propagating staleness is not.

## Dispatch depth is governed by DEPTH, not tier-adjacency (resolved)

- `kg-ideate → spec-scaffold → ideation-cycle` is a 2-hop path, in-bound. `kg-brief` is a
  TERMINAL leaf (reuse-by-artifact = 0 hops, or re-run = leaf) — it never extends depth, so
  NO hoist back to the compound is needed. A molecule calling a molecule is legal under the
  refined contract (`.agents/proposals/skill-tiering-molecule-composition.md`); the lint
  warns on static call-graph depth >2, not on a molecule-calls-molecule edge.

## Fork evidence is a sidecar, not inline

- A hard fork's prototype + negative-control + cross-brain audit is its own sidecar
  artifact, LINKED from the spec's decision entry (anticipating the lineage `derives_from`
  evidence edge). Don't paste it into the spec body or bury it in transient task notes.

## Don't let the spec become a plan

- The spec owns what/why + decisions + open questions + done-criteria. File paths,
  function names, and task breakdowns belong in the plan (execution half). A spec that
  accumulates them has drifted; move that content out. Reference: `workflow-artifact-model`.

## Isolate experiments from the coverage gate

- Each prototype gets its own `go.mod` and lives under `.agents/active/experiments/` (or
  similar), out of the repo's test/coverage gate. It is throwaway evidence, not product
  code — it must never ship or count toward coverage.

## Don't declare the loop closed on a modeled result

- An audited prototype validates a *modeled* scenario. The dogfood pass validates the
  *real corpus* (e.g. zero-diff migration). A clean prototype + a dirty dogfood diff
  means the decision is not yet validated — the diff is a new fork, not a rounding error.
