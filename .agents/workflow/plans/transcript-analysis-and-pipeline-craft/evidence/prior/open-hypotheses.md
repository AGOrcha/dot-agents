# Open hypotheses from prior analyses (falsification candidates)

Claims the prior analyses *raised but did not test* — asserted from observation, correlation, or a
single case, without a counterfactual, controlled comparison, or executed refutation. Each is a
candidate for the plan's `falsification-review` pass (and, where it is a routing/frontier claim, the
`pareto-live-waves` + `pareto-live-review` pass). Anchors use the `prior-findings-index.md` legend
(`DA`/`EFF`/`BEH`/`PLAN`). Each entry names why it is untested and a concrete test to run.

Grouping: A) efficiency/quantitative, B) causal front-loading, C) correction attribution,
D) classification/attribution edge cases, E) coverage/measurement bias.

---

## A. Efficiency / quantitative claims (untested — no timing data)

- **OH-A1 — "delivery became materially more efficient once work moved into the inner-loop/meta-loop model."**
  Source: `EFF-BL` (`EFF#L687-706`), `EFF-X0` (`EFF#L11-25`). Untested: the report itself states there is "no defensible single percentage" (`EFF-LC1`, `EFF#L674-676`) and that raw-throughput/reduced-rework gains are only medium-confidence/qualitative (`EFF-MG2`, `EFF#L663-671`). No paired or time-normalized comparison exists.
  Test: instrument per-plan lead time + per-task elapsed (the report's own recommendation `EFF-RI1`, `EFF#L710`) on matched task classes; or run our `pareto-live-waves` paired snapshot-identical contrasts (`pareto-measurement-rubric.md:46-53`) so an efficiency delta carries a CI. Refuted if the wall-clock/token delta's CI includes zero on comparable task classes.

- **OH-A2 — context-rehydration proxy "15-31 mixed files → 3 canonical files + 1-2 task artifacts" measures a real re-entry-cost gain.**
  Source: `EFF-W1`/`EFF-BL` (`EFF#L163`, `EFF#L703-704`), `BEH-M1` (`BEH#L343-356`). Untested: it is a file *count* proxy, never a measured time-to-context or token-to-context; no old vs new case was actually re-entered under measurement.
  Test: measure tokens/tool-calls to reconstruct task state from each surface on ≥3 old and ≥3 new cases (our stage-run/anchor tooling can count this). Refuted if canonical-file re-entry does not reduce tokens-to-context, or if the "3 files" omit context that forces reading the same 15-31 artifacts anyway (cf. `EFF-B2`: context lives in prose, not the payload, `EFF#L347-363`).

- **OH-A3 — cheap-tier stage swaps sit on/near the delivery frontier (implied routing hypothesis).**
  Source: our plan derives this from the priors' "efficiency gain" framing; the priors offer only coarse bands (`score-workflow-evidence.py:23-60`) and no model-routing measurement. Untested: no historical row establishes a frontier (`pareto-measurement-rubric.md:42-45` forbids it).
  Test: `pareto-live-waves` — one stage-model swap per contrast, ≥3 repeats, bootstrap CIs, dominance check (`pareto-measurement-rubric.md:46-53`). Refuted if a cheap-tier swap is CI-dominated on accuracy by the baseline executor.

## B. Causal front-loading claims (correlational only)

- **OH-B1 — "correctness converges fastest when the workflow front-loads the right constraints" (not merely when it creates more artifacts).**
  Source: `EFF-E3`/`EFF#L141`, `EFF-B1` (`EFF#L333-345`), `BEH-M3` (`BEH#L372-389`). Untested: inferred from which cases needed later corrections; no case had front-loading held constant while outcome varied. `APPDEV-10376` (strong front-loading) is confounded with being a mature multi-repo effort (`BEH-C3`).
  Test: adversarial re-query — find a strongly front-loaded case that still incurred heavy correction loops, and a thinly front-loaded case that converged fast. If either exists, the causal claim is weakened. In live waves, vary bundle richness on paired disposable tasks and measure correction count.

- **OH-B2 — "the delegated worker payload was thinner than the plan/spec, and that thinness caused the correction loops."**
  Source: `EFF-B2` (`EFF#L347-363`), `PLAN-G2` (`PLAN#L45-52`). Untested: the missing-fields observation is real, but the *causal* link to correction loops is asserted, not shown; corrections may trace to the prose plan the worker also read.
  Test: for each corrected 13778 slice, check whether the corrected information was present in the plan/spec but absent from the archived `delegation.yaml`, vs absent from both. Refuted (as a bundle-thinness cause) if corrections concern facts absent from the plan/spec too, i.e., a planning gap not a projection gap.

- **OH-B3 — companion surfaces (auth/ingress/automation/integration-lib) are a *systematic* planning blind spot requiring a reusable checklist.**
  Source: `BEH-M4` (`BEH#L391-402`), `EFF-CT3` (`EFF#L453-463`). Untested: generalized from 13778's `p5/p6/p7` fold-backs (`BEH-C4`); n≈1 feature.
  Test: audit companion-surface completeness across the other workflow-era cases (10376, credentialing, perf-parity). Refuted if companion omissions do not recur outside 13778.

## C. Correction-attribution claims (small-n pattern assertions)

- **OH-C1 — "human-found issues are mostly ownership/proof/product; agent-found issues are mostly implementation/query/lifecycle/companion."**
  Source: `EFF-E3` (`EFF#L132-133`), `BEH-CI-I3`/`I4` (`BEH#L527-528`), `BEH-CI` table (`BEH#L510-521`). Untested: hand-coded from a 6-case table, several rows transcript-poor (the table itself flags uneven sourcing, `BEH#L512`); no inter-rater check, no anchors per cell.
  Test: re-derive the who-found-what split from anchored transcript turns (rubric E1/E3, `evidence-rubric.md:30-37`), coding each issue independently; refuted if the split does not hold once each cell is anchored, or if transcript-poor rows can't be coded and the pattern rests only on transcript-rich cases.

- **OH-C2 — "the worst direction changes came from unknown-unknowns, not missing details."**
  Source: `BEH-CI-I5` (`BEH#L529-532`), `EFF` correction taxonomy (`EFF#L426-488`). Untested: "unknown-unknown" is defined post hoc; the same items (missing 2.0 parity object, hidden milestone boundary) are elsewhere framed as front-loadable *known* gaps (`EFF-B4`, `EFF#L387-409`) — internal tension.
  Test: classify each direction-changing item as (a) discoverable from available contracts at plan time vs (b) genuinely emergent. Refuted if most "unknown-unknowns" were derivable from the OpenAPI/schema/spec already in hand.

## D. Classification / attribution edge cases

- **OH-D1 — credentialing-ui-hardening is "workflow-era with degraded evidence path *caused by* Windows `da` friction," not weaker process intent.**
  Source: `BEH-E3`/`BEH-EC` (`BEH#L469-486,534-549`), `EFF-E2`/`EFF-LC3` (`EFF#L124,681-683`). Untested: the causal attribution to Windows/code-signing/policy friction is asserted from transcript mentions of `da` failures (`BEH#L297-304`); the counterfactual (same case on a working `da`) was never run.
  Test: reproduce the blocked `da workflow`/`da config explain` operations on a current environment; if they now succeed, the friction attribution is consistent — but check whether the *delegated-archive omission* specifically was tool-blocked vs simply not attempted. Refuted if the archive gap persists where `da` is known-good.

- **OH-D2 — payout/dot-agents "influenced Provider Admin process design earlier and more directly than the artifact-only pass could prove."**
  Source: `DA-T5` (`DA#L90-94`), `DA-T1..T3` (`DA#L78-86`). Untested: inferred from hidden-transcript titles/references (e.g., 2026-05-12 "Create payout plan for provider admin"); title co-occurrence ≠ causal influence on design.
  Test: trace whether specific ProvAdm design decisions cite/mirror payout artifacts by content (not just chronology). Refuted if the ProvAdm decisions predate or diverge from the payout patterns they supposedly derive from.

- **OH-D3 — "environment mode (devcontainer/WSL vs Windows-first) materially affects agent behavior over time."**
  Source: `DA-T4`/`DA-S2` (`DA#L28-40,87-94`). Untested: raised as a caveat about comparison validity; never isolated as a variable.
  Test: hold task class fixed and compare behavior/outcome across environment modes where both exist for the same repo. Refuted if outcome/behavior metrics are indistinguishable across modes.

## E. Coverage / measurement bias (raised as caveats, never quantified)

- **OH-E1 — recency/retention bias does not distort the era comparison.**
  Source (implicit): `EFF-S2` (`.copilot` coverage "strongest for Jun-Jul 2026," no hits for the older 10376/9175/7817, `EFF#L44`) and `EFF-S3`/`EFF-S4` (older cases recoverable only via deeper hidden-store mining, `EFF#L48-65`). Untested: the reports lean on richer recent transcripts for the workflow-era cases and thinner recovered evidence for the raw/intermediary cases — a survivorship pattern that could inflate the workflow-era verdict; never controlled.
  Test: restrict the era comparison to cases with *comparable* transcript density, or down-weight transcript-only signals; refuted (bias present) if the "workflow-era is better" conclusion weakens once evidence density is equalized. Directly relevant to our confidence grading (`evidence-rubric.md:64-67`) and rubric-version matching (E4, `:37-40`).

- **OH-E2 — mtime/ctime/git-commit fallback anchors are accurate enough to time-order the case set.**
  Source: `EFF-S7`/`EFF-A1..A8` (`EFF#L81-101`), `BEH-A1..A8` (`BEH#L60-69`) — older cases anchored on `file mtime`/`ctime fallback`. Untested: fallback anchors were used without validation against any recorded timestamp; our rubric bans mtime precisely because it is unreliable (`evidence-rubric.md:16`).
  Test: for any case with both a recorded timestamp and an mtime anchor, compare; refuted if mtime diverges enough to reorder adjacent cases. Any ordering-dependent claim built on a fallback anchor inherits this risk.

- **OH-E3 — the coarse evidence score is a safe *prioritizer* (its self-declared role) and does not mis-rank cases.**
  Source: `score-workflow-evidence.py:57-60` ("coarse first-pass… not replace it"). Untested: substring probes (`"result"`, `"verification"`, `"merge-back"`) decide verification/bundle ratings (`score-workflow-evidence.py:35-43`) — trivially gameable by incidental text and blind to whether the evidence is real.
  Test: construct a case with the trigger substrings but no real verification evidence; if it scores `high`, the ranking is unsafe. This is the concrete counter-example the falsification rubric wants (`falsification-review-rubric.md:15-18`).

---

## How these route into the plan

- `falsification-review` (cross-family blocking gate, `TASKS.yaml`): OH-B*, OH-C*, OH-D*, OH-E* are re-query/counter-example candidates — each has an executable test above (rubric `falsification-review-rubric.md:9-25`).
- `pareto-historical` → `pareto-live-waves` → `pareto-live-review`: OH-A1, OH-A3 (and OH-A2 as an instrumented measurement) are the frontier/efficiency claims that only a CI-backed paired live contrast can settle (`pareto-measurement-rubric.md:42-53`).
- `synthesis`: convergence of these open hypotheses with our locally-extracted evidence items (rubric E2 defers convergence to synthesis, `evidence-rubric.md:33-35`); any hypothesis that survives refutation with ≥medium confidence becomes an actionable outcome, the rest stay as gaps/review-debt (`templates/case-study.md:17-22`).
