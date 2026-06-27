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
  ONLY after the freshness gate (`inputs_digest` match AND no dependency-manifest entry
  changed). On any mismatch, re-run `kg-brief`. A stale brief poisons every fork downstream —
  re-running is cheap; silently propagating staleness is not.

## Don't cite a depth-2–3 hop "fidelity cliff" — it didn't replicate

- The tier contract's "fidelity degrades past depth ~2–3 hops" premise **did NOT replicate**
  (no same-agent hop cliff found ≤ depth 10 / ~15k tokens on local self-checkable constraints,
  two harnesses; null **power-limited**, ceiling ~97.6%). Write "no cliff found in the tested
  regime," NEVER "no cliff exists."
- **v4 found a NARROW drift on COMPOUNDING work** (error-prone referential constraints, binary
  metric: Haiku 80%→40%→0%, Opus slips to 80%) — but its **GATE-2 audit ruled the broad claim
  NOT-SOUND** (one family; confounds length/edges/output-size; not a route-by-tier law). Fold it
  ONLY as: decompose error-prone compounding chains into verifiable sub-artifacts; prefer a
  code-executing agent for computable closures. NOT a mechanism, NOT a routing law — mechanism
  deferred to **v5**. It is about compounding-chain length, not hop depth.
- Cite the evidence sidecar (`evidence/depth-degradation-dogfood.md`, §v4/§v4.9). The composition
  is bounded by ENGINEERING, not a measured cognitive cliff: **infra delegation-nesting ceiling
  (~hop 4)** + **relay discipline** + **decomposition of compounding work** + hygiene.
  Tier-adjacency is dropped, so the chain is legal regardless.
- **These bounds are CURRENT-HARNESS-OBSERVED, not universal laws.** The ~hop-4 ceiling and the
  v4 capability map were measured on Claude Code's `Agent` tool + `codex exec`; **re-assess per
  harness** (Hermes, Pi-agent, Aider, Antigravity-CLI, …) — a new harness's nesting/capability
  limits are a variable to **measure, not assume**. The tiering model is stable; its numbers are
  per-harness.

## Relay hand-backs MUST be structured/pointer-based — never retold prose

- The one positive finding: lossy summary relay drops **non-reconstructable** detail and it
  **reaches the deliverable** (sidecar v3 family-2: 16→13). Every hand-back (worker → driver,
  hop → hop) returns the **artifact path + a constraint/decision checklist**, not a prose
  retelling. Reconstructable defaults survive a retell; arbitrary structural detail does not.
- Deep multi-hop delegation is **driver-orchestrated hop-by-hop** (fresh `Agent` per hop,
  relay via on-disk file), never recursively nested — nested `Agent` delegation collapses past
  ~hop 4 *on the current harness* (re-assess per harness).

## ideation-cycle RETURNS the decision; spec-scaffold writes the prose

- `ideation-cycle` is a **compound** that produces a ratified decision + a per-fork evidence
  sidecar — it does NOT type the spec file. Dispatched: return the decision + sidecar pointer
  to `spec-scaffold`. Standalone: hand them to a delegated spec-drafting step. Writing the
  spec prose yourself breaks the segment-ownership split.

## Fork evidence is a sidecar, not inline

- A hard fork's prototype + negative-control + cross-brain audit is its own sidecar
  artifact, LINKED from the decision entry (anticipating the lineage `derives_from` evidence
  edge). Don't paste it into the spec body or bury it in transient task notes. The founding
  instance is this skill's own `evidence/depth-degradation-dogfood.md`.

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
