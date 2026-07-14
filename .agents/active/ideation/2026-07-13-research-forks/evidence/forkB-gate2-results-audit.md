# Fork B — GATE 2 (cross-harness results audit)

**Auditor:** codex (codex-cli 0.144.1) — different harness/model family. Adversarial pass on the
prototype's conclusion. Run 2026-07-13 (bbandd6jo), ~99k tokens.

**Verdict: CONCLUSION-OVERCLAIMED (scope it).** The prototype proves the toy two-phase rule is
deterministic FOR THIS FIXTURE — not that H_B1 preserves the REAL engine.

1. **Fidelity — partial (matters).** Prototype omits `org`/`team` precedence (`engine.go:32-43`
   vs real `authority.go:103-109`), drops `Kind`-specific capability union
   (`profile_resolver.go:341-347`), sorts raw `Ref` instead of real `Key()`
   (`profile.go:159-180`), and its lock pass unconditionally pins/subtracts whereas the real
   engine validates authority, rejects fatal overlaps, and permits peer/higher-authority writes
   (`profile_locks.go`, `authority_apply.go`). Any of these can change the frozen family or
   produce errors the prototype never sees.
2. **Tests — real but soft.** Hazard fixture genuine (6 perms run). But determinism is largely
   guaranteed by the impl's own sort + deliberate phase exclusion (somewhat circular); the
   no-collision control is nearly tautological (unresolved family `""` can't match a nonempty
   family selector); the tie test checks hand-picked values, not equivalence with the legacy
   resolver; the cache test varies `Role` while `BuggyKey` omits `Role` — it proves GENERIC
   under-keying, not specifically a missing frozen-family key.
3. **Rule relocates, doesn't fully solve.** No-self-reference is a genuine fix for TOP-LEVEL
   family self-reference, but not a general dependency solution — it does not cover future
   selector dims whose values are written in phase-2, dynamic family-via-`Harness` derivation, or
   the transitive `org→team→repo` extends ordering / dedupe / cycle cases from
   `config-transitive-layering` (the prototype exercises none of these).
4. **Decision too strong.** The one genuine duplication pair + the unvalidated complexity tax
   favor **H_B0 now**. Retain H_B1 as a CONDITIONAL design hypothesis; consider H_B2 only when
   demand crosses the trigger and adapter coherence is acceptable.
5. **Highest-value correction:** rerun the permutation/lock/cache tests against the REAL resolver
   with org/team precedence, capability kinds, and authority collisions; fix the cache test to
   isolate ONLY frozen-family variation.
