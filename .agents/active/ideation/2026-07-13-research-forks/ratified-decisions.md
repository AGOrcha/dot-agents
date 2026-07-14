# Ratified decisions — research-intake surfaced forks (ideation-cycle, 2026-07-13)

This is the ideation-cycle RETURN artifact: ratified decisions + per-fork evidence sidecar. It
does NOT write spec prose (per `workflow-artifact-model` — spec-scaffold owns that). Owner
ratification pending; the decisions below are the audited recommendations.

Grounding: reused-by-freshness from this session's research-intake pass (no separate kg-brief
re-run). Cross-harness auditor for all gates: codex (codex-cli 0.144.1), a different model family
than the Claude driver. Evidence files under `evidence/`.

---

## Fork B — per-model-family harness override layer → **H_B0 (status quo) NOW; H_B1 is a CONDITIONAL hypothesis**

**Decision (scoped down by GATE 2 from the going-in "design-approved, adopt-deferred"):**
- **Do NOT build a per-model-family override layer now.** Adopt **H_B0 (status quo)**: the
  measured benefit today is **one** genuine duplication pair (`cross-harness-adversarial` ↔
  `cross-harness-adversarial-claude`), and the only non-claude family present is `gpt` (1 of 27
  stage-profile fragments) — far below the adopt trigger.
- **H_B1 (add `model_family` as a `ProfileSelector` dimension, two-phase resolution with a frozen
  phase-1 + no-self-reference rule) is retained as a CONDITIONAL design hypothesis, NOT
  design-approved.** The prototype (all 6 experiments green) established that the two-phase +
  no-self-reference rule is deterministic and hazard-removing *for the pre-registered fixture* —
  but GATE 2 showed that does NOT prove it survives the real engine.
- **H_B2 (post-resolution adapter)** stays a fallback, considered only if demand crosses the
  trigger and the two-mental-model coherence cost is acceptable.

**What the empirical pass actually proved (keep vs over-read):**
- ✅ The hazard is real and specific: a naive single-pass resolver selecting on `model_family` is
  order-dependent (4 distinct outcomes over 6 permutations) *only* when family-as-value and
  family-as-selector coexist.
- ✅ A frozen-phase-1 + no-self-reference rule removes it for the top-level case.
- ❌ NOT proved: that the rule preserves the REAL engine's org/team precedence, `Kind` capability
  union, authority-validated locks, or the transitive `org→team→repo` extends ordering from
  `config-transitive-layering`. The prototype simplified all of these away (GATE 2, fidelity).

**Trigger + gate to revisit (both required):**
1. Demand: `.agentsrc.json` carries **≥2 distinct non-claude `model_family` values across ≥3
   stage-profiles** (today: 1). AND
2. Validation: re-run the permutation / lock / cache experiments **against the real resolver**
   (org/team precedence, capability kinds, authority-collision locks, transitive extends) — not a
   toy — and fix the cache test to isolate frozen-family variation only. Only then can H_B1 move
   from "conditional hypothesis" to "design-approved."

**Routes:** update proposal `~/.agents/proposals/obs-per-model-family-harness-override-layer.md`
to carry this scoped conclusion (do-not-build-now; the two-gate revisit condition; the
real-engine-validation debt). No change to `config-transitive-layering` tasks.

---

## Fork A — meta-loop mechanism-generation → **modified Pos-3 (no standing change; autonomy parked with sharpened criteria)**

**Decision (cross-brain moved this off the going-in Pos-1):**
- **No standing routing change.** Do NOT add a "prefer new mechanism over parameter tweak"
  heuristic to `knowledge-fold-back` now — on n=3/one-benchmark evidence it is slogan-level noise
  and risks biasing triage toward unnecessary abstraction.
- **Keep autonomous mechanism-generation PARKED.** The Bilevel Autoresearch result is cited for
  *direction only* (mechanism-change ≫ parameter-change as a research hypothesis), never as a
  production rule.
- **Correction on record:** `skill-architect` + `da review` is a *validation gate*, NOT a runtime
  safety envelope. Admission criteria to reconsider autonomy (now in the parked fold-back):
  (1) replicated cross-task evidence beyond the paper's n=3; AND (2) a real runtime envelope —
  fail-closed transactional promotion with automatic rollback + provenance of machine-authored
  mechanisms + bounded canary/blast-radius.

**Route:** `bilevel-meta-loop-mechanism-generation` fold-back UPDATED with the above (done). No
new artifact; the lesson `use-skill-architect-for-skill-generation` already carries the
validation-gate (not rollback-envelope) framing.

---

## Fork C — orchestrator-swap + brief-quality axis → **DEFERRED (unchanged)**

Not resolved — frozen-adjacent to the transcript plan's pareto waves. Pre-framed as a
post-freeze experiment in `pre-registration.md` (§Fork C): hypothesis, discrimination criterion,
and a concrete trigger (freeze lifts; run jointly with the K.3 effort-dial axis). No wave
defined, no frozen decision reopened. Already recorded in eval Part L open-Q #2 + plan digest
addendum #11.

---

## Meta-note — the cycle earned its keep

Both empirical/cross-brain gates changed a going-in recommendation, which is the point:
- Fork A: GATE (cross-brain) downgraded Pos-1 → modified Pos-3 and named the missing runtime
  safety control.
- Fork B: GATE 1 caught a design that could have passed while dodging the hazard (hardened the
  fixture + made 5 invariants blocking); GATE 2 caught that the green prototype over-claimed
  (proved the toy, not the real engine) → downgraded "design-approved" → "status quo now, H_B1
  conditional." A same-harness self-review would likely have rubber-stamped the green prototype.
