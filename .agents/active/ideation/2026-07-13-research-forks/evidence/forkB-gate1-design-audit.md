# Fork B — GATE 1 (cross-harness design audit)

**Auditor:** codex (codex-cli 0.144.1) — different harness/model family than the driver.
**Verdict:** DESIGN-FLAWED (fix before running). Findings incorporated into pre-registration v2.

1. **Fixture does not force the hazard.** The prototype must contain a concrete 3-fragment
   permutation fixture, else it can pass with a trivially frozen family and never expose order
   dependence:
   - fragment A (low `Order`): `bundle.model_family = "claude"`
   - fragment B: `selector.model_family = "claude"` with a sentinel override value
   - fragment C (higher `Order`): `bundle.model_family = "gpt"`
   - run ALL input-order permutations; assert whether B matches the FINAL frozen phase-1 value.

2. **Negative control mislabeled.** The `.agentsrc.json` duplication count is a *benefit/demand
   baseline*, not a negative control — it conflates intentional role/stage differences with
   collapsible duplication and says nothing about semantic override reuse. Keep it as a benefit
   baseline AND add a real no-collision control fixture.

3. **Missed failure modes** (must be blocking acceptance criteria): value-locks on `model_family`
   + selector-scoped locks; `PolicyModeReplace` (precedence/permissions reset while locks
   accumulate); `Order`-precedes-`specificity` (a new selector dimension shifts tie behavior);
   `schemas/agentsrc.schema.json` currently permits `model_family` as a bundle value, NOT a
   selector key (schema/migration surface); profile lock/projection/cache invalidation +
   context-sensitive resolution keys.

4. **Decision rule unsound.** "P1 + low duplication" does not establish that locks, replace
   semantics, schema ingestion, persistence, or cache/projection remain correct. The
   model-diversity threshold is unspecified; "H_B2 or H_B0" is not a rule.

**Single most important fix:** pre-register the concrete 3-fragment permutation fixture and make
all lock / policy / schema / persistence / cache invariants BLOCKING acceptance criteria.

(Full transcript: codex run bu1bnueok, 2026-07-13; ~106k tokens.)
