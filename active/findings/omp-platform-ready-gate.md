# omp-platform findings — ready_gate stage (D14/R8)

da↔omp friction hit while running the terminal READY/FOLD-BACK gate under omp. Terse bullets:
exact operation + what happened.

- **The gate decision is pure line-1 grep across 7 upstream signal files — no da rollup.** The gate
  had to read `COORD/{impl,verify-unit,verify-cli-runner,review-*}.md` and hand-evaluate
  `impl==DONE ∧ both verify==PASS ∧ ALL 4 reviews==APPROVE`. There is no
  `da workflow gate --require impl=DONE,verify.*=PASS,review.*=APPROVE` primitive that aggregates the
  stage verdicts and emits READY/FOLD-BACK. A malformed or missing line-1 token in any of the 7 files
  would silently mis-gate; the ready_gate stage is the only thing enforcing the rollup, in prose.

- **No structured reviewer verdict → the gate can't distinguish "REJECT with BLOCKERs" from a soft
  no.** cross-harness-adversarial returned `REJECT` (2 BLOCKERs) while adversarial returned `APPROVE`
  after finding the SAME neverIgnored bypass at MEDIUM. The severity→verdict mapping lives entirely in
  each reviewer's prose; da emits no `{verdict, findings:[{severity}]}` schema, so the gate cannot
  programmatically confirm a REJECT is backed by a BLOCKER/HIGH (vs a lens that downgraded the same
  finding). Two lenses converging on one hole with DIFFERENT verdicts is exactly what a structured
  severity contract would reconcile.

- **`lens_concurrency: gated` is emulated, not enforced — the gate pays for it.** Because the swarm has
  no native short-circuit (CONVENTIONS RULE 0), all four review lenses RAN even though
  cross-harness-adversarial ultimately REJECTs; the blocking verdict only surfaces at THIS rollup, not
  as an early chain stop. The gate therefore always waits for the full lens fan-out before it can fold
  back. A first-class gated edge would let a blocking lens short-circuit and fold back sooner. (Full
  divergence recorded in `CONSOLIDATED.md` §3.1.)

- **Coordination-ref commit is last-writer-wins, not CAS.** The tasked ref update is
  `git update-ref refs/agents/state <HEAD>` (bare form). Per `git-ref-reconciliation-model.md`, this
  is a D9/D10 gap: a stray concurrent writer would be silently clobbered. Safe THIS run only because it
  is sequential (ready_gate is the sole committer). da should expose a CAS-form WorkStore commit
  (`update-ref <new> <old>`) so the coordination lineage is race-safe by construction.

- **Owner-held merge + no-board-mutation is honored by NOT calling da.** RULE 4 / RULE 10: the gate
  drove to a terminal FOLD-BACK, opened NO PR, ran NO CI, wrote NO merge-back artifact, and did NOT run
  `da workflow advance/closeout/merge-back` (0.4.2 store race). The human orchestrator reconciles the
  board after the owner acts. Worked as intended — but "don't touch the board" is enforced only by
  convention; there is no da read-only/gate-only mode that would refuse board-mutating verbs for a
  gate-role agent.

- **Positive:** reading the 7 coordination signals + 9 findings files and writing FOLD-BACK /
  CONSOLIDATED under the `_state` worktree had no embedded-shell quirks; the file-only protocol is
  legible. The friction is entirely the ABSENCE of da primitives (gate rollup, structured verdicts,
  CAS commit, gated short-circuit), not the harness fighting the filesystem.
