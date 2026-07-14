# Fork A — cross-brain judgment audit (meta-loop mechanism-generation)

**Auditor:** codex (codex-cli 0.144.1) — different harness/model family. Adversarial pass on the
driver's recommended default (Pos-1). Run 2026-07-13, ~37k tokens.

**Going-in default (Pos-1):** (i) add a "prefer new mechanism over parameter tweak" routing
heuristic to `knowledge-fold-back`; (ii) name `skill-architect` + `da review` as the safety
envelope; (iii) park autonomy with a trigger.

**Verdict: Pos-1 is TOO STRONG → modified Pos-3.** Findings:
1. (i) is additive (the routing tree does NOT already encode it) but WEAK — without an operational
   test it is slogan-level noise and risks biasing triage toward unnecessary abstraction.
2. The paper's evidence (n=3, one benchmark, one bilevel step, high variance, no tested
   skill/prompt carrier) supports a research hypothesis, NOT a standing production-routing rule.
   Cite for "shape and direction," not as a rule.
3. `skill-architect` + `da review` = structural + human validation, NOT runtime safety. The most
   important MISSING control is **fail-closed, transactional promotion with automatic rollback**
   (isolate the candidate, preserve the prior mechanism, ensure failed validation cannot silently
   mutate active state). Provenance of machine-authored mechanisms + bounded canary/blast-radius
   also required.
4. Pos-3 steelman: this is corroboration of an already-human-gated philosophy, not evidence of
   transfer; a mechanism-preference rule on preliminary evidence may create false confidence in
   the named "envelope."

**Highest-value action (ratified):** make NO standing routing change; keep autonomy parked; AMEND
the parked decision record (`bilevel-meta-loop-mechanism-generation`) with explicit admission
criteria — replicated cross-task evidence AND a real runtime envelope (fail-closed transactional
promotion + automatic rollback + provenance + bounded canary/blast-radius) before reconsideration.

## Ratified fork A decision

- **Do NOT** add the mechanism>parameter heuristic to `knowledge-fold-back` now (premature
  abstraction on preliminary evidence).
- **Do NOT** represent `skill-architect` + `da review` as a sufficient safety envelope — it is
  necessary but NOT runtime-safe (the `use-skill-architect-for-skill-generation` lesson update
  from this batch should be read as "validation gate," not "rollback envelope").
- **KEEP** autonomy parked; sharpen the fold-back's trigger to the admission criteria above.
- The paper stays cited for *direction* (mechanism-change ≫ parameter-change as a research
  hypothesis), never as a production rule.
