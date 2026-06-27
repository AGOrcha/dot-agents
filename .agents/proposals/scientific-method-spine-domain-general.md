# Proposal: the scientific-method spine — domain-general, first-class in the kg-ideate / ideation-cycle pipeline

- type: project-local design input (markdown per `proposal-routing` — refines repo skills/specs)
- status: folded; #186 merged (2026-06-26 — the spine is now the documented hard-fork path in
  the `ideation-cycle` skill: `classify-forks.md` evidence-type triage + evidence-form table;
  `empirical-pass.md` pre-registration + GATE 1 + power/regime check; `fidelity-gate.md` four
  discrimination levels + null-first-class; `cross-brain.md` two-gate audit + serialized codex;
  `converge.md` narrow/caveated fold + unsettled axes; `SKILL.md` spine index)
- targets:
  - `internal/scaffold/home/starter/skills/global/ideation-cycle/` (the skill: make this protocol the SPINE of the hard-fork path; folds after the #186 edits)
  - `.agents/workflow/specs/ideation-system-composition/design.md` (reference the spine as the idea→spec resolution method)
  - `.agents/workflow/specs/kg-ideate-skill/design.md` (the pipeline owner that dispatches it)
- provenance: distilled from this session's v1→v4 depth experiments + three independent codex audits (evidence sidecar `.agents/workflow/specs/ideation-system-composition/evidence/depth-degradation-dogfood.md`) and `.agents/lessons/prototype-experiment-fidelity-gate/`.

## Goal

Make the **scientific method** the first-class, **domain-general** spine of the ideation
pipeline's hard-fork resolution — not a software-only "build a Go prototype" habit. The
protocol is invariant; only the **evidence form** changes by domain. This is what makes it
"practical and usable across every domain" (owner, 2026-06-26).

## The invariant spine (any hard fork, any domain)

1. **Frame** competing hypotheses + state *what evidence would discriminate* between them.
2. **Pre-register** predictions + power/discrimination criteria BEFORE gathering evidence
   (no post-hoc goalpost-moving). [added in v3]
3. **Cross-brain GATE 1 — design audit (PRE-run).** Before spending on the run, an independent
   brain/method audits the *design*: are the hypotheses falsifiable, the predictions
   pre-committed, the power/regime adequate, the negative control real, the inputs faithful,
   the evidence-form appropriate? Its job is to **invalidate the DESIGN** and send it back
   *before* the spend. This is the cheaper, higher-leverage gate — it catches the *wrong
   experiment* (the expensive error class), not just the wrong conclusion. [the fix for v1/v2/v3
   each running a flawed design and only being caught after the run]
4. **Gather faithful evidence** in the domain-appropriate form, with a **negative control**
   (the broken/falsifying case must actually fail). [fidelity-gate]
5. **Power + regime check** — CAN this evidence discriminate (sub-ceiling baseline), and are
   you measuring WHERE the effect lives? Internal rigor ≠ regime validity. [v3/v4]
6. **Cross-brain GATE 2 — results audit (POST-run).** A different brain/method whose job is to
   **invalidate the CONCLUSION**: does the evidence actually support the claim, what confounds
   remain, where is it over-claimed? Self-audit structurally cannot catch "wrong experiment" or
   "hollow null." [caught us 3×] Both gates are the SAME independent-brain discipline applied at
   the two points where it pays off.
7. **Null is first-class** — a documented "couldn't induce / couldn't reach the regime" is a
   finding, never laundered into a clean answer.
8. **Iterate or extract** — each audit reveals the next flaw; loop until sound, or extract the
   actionable subset and mark the rest explicitly unsettled. **Ratify** with a per-fork
   evidence sidecar (lineage edge). [D7]

## Domain-generality — the evidence-form mapping

The spine dispatches on a fork's **evidence type** (the `classify-forks` triage gains this):

| Fork type | "Empirical evidence" instantiates as | Negative control = |
|---|---|---|
| Algorithmic / software | throwaway prototype + tests | the broken impl must fail the test |
| Agent-behavior | controlled eval across conditions | a condition engineered to fail must fail |
| Research / factual | corpus + KG evidence (`article-extract → ingest → KG`) | a disconfirming source actively sought |
| Product / UX | user data, A/B, instrumented usage | the variant predicted worse must read worse |
| Ops / infra | a controlled measurement / load test | a known-bad config must trip the metric |
| Pure judgment (no empirical handle) | adversarial cross-brain + explicit trade-off analysis | argue the strongest case for the rejected option |

Triage classes: **briefing-decidable** (resolve inline) / **empirically-testable** (run the
spine with the domain-appropriate evidence form) / **judgment-call** (cross-brain + trade-off,
the last table row) / **unfalsifiable-needs-reframe** (no discriminating evidence exists →
reframe the fork until it does, or escalate to the owner).

## Gaps in the current `ideation-cycle` this fills (mapped to instruction files)

- `classify-forks.md` — add the **evidence-type** classification + the unfalsifiable-needs-reframe class.
- `empirical-pass.md` — currently assumes a Go-prototype deterministic PASS/FAIL; generalize to
  the evidence-form table; add **pre-registration** (step 2) and the **power/regime check** (step 5)
  as required sub-steps, not just the negative control. Insert **GATE 1 (design audit) BEFORE the
  run** and **GATE 2 (results audit) AFTER** — the two-gate cross-brain.
- `fidelity-gate.md` — split discrimination into the three levels the session surfaced:
  **instrument** (scorer catches violations) vs **experiment** (effect can occur) vs **regime**
  (testing where the effect lives). Name **null as first-class**.
- `cross-brain.md` — extend from "cross-brain the judgment calls" to the **two-gate independent
  adversarial audit**: GATE 1 invalidates the *design* (pre-run, the cheaper/higher-leverage
  gate — catches the wrong experiment before the spend); GATE 2 invalidates the *conclusion*
  (post-run). Same independent-brain discipline, applied at both points. Judgment-call forks
  (no empirical handle) still get the single cross-brain trade-off pass.
- `converge.md` — ratify with the per-fork evidence sidecar; record unsettled axes explicitly.

## Sequencing — RESOLVED

The `ideation-cycle` skill was mid-edit on #186 (the tiering/relay fold); to avoid conflict
the spine folded in AFTER #186 merged. **Done** (2026-06-26): folded on a branch off the merged
#186 state. The companion methodology **lesson** (`prototype-experiment-fidelity-gate`, the full
v1→v4 arc: power-gate, regime-validity, independent-invalidation, null-first-class) is the
durable principle record this proposal operationalizes; the lesson's "depth-degradation arc"
section is the source the skill edits encode.

## Non-goals

- Not a new standalone skill — it is the SPINE of the existing `ideation-cycle` hard-fork path.
- Not "every fork gets a prototype" — the triage is the point; most forks are briefing-decidable.
- Not software-only — the evidence-form table is the domain-generality contract.
