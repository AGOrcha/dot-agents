# Pre-execution evidence validation — 2026-07-12

Decision record for the validity check the user requested on the re-executor output and the
broader evidence collection, run BEFORE any post-approval execution (`pareto-live-waves`).
Committed regeneration: `24c8bdf1` (branch `feat/transcript-analysis-pipeline-craft`).

## Verdict: VALID at the data level — no re-collection

Four independent Claude-family audits (`agent://ExecutionEvidenceAudit-2`,
`CollectionValidityAudit-2`, `ParetoValidityAudit-2`, `ResearchValidityAudit-2`) each recomputed
digests / counts / arithmetic / token-ratios from source. Every count, digest, aggregate, and
arithmetic claim independently reproduces. Main's raw findings: **100% confirmed, 0 refuted.** No
corpus corruption, no PII leak, no R5 violation, no fabricated data. The re-executor's post-fix
blocks (S2-H3/S4-H3/H4/H6/H7/S5-*) all reproduce as reported.

> The first reviewer-family spawn failed — all 4 hit Codex `usage_limit_reached` (the reviewer
> agent routes to GPT/Codex, currently rate-limited). Re-run on the `task`/Claude family since
> these are mechanical recompute checks, NOT the blocking cross-family adversarial gate.

Independent re-verification this pass: iter-log **69/69** digests recompute exact; inventory
**806** rows intact; frozen-snapshot ↔ live-cost-row handoff matches end-to-end; codex **120/120**
full-64 digests.

## What was regenerated (bounded — data byte-preserved)

| # | Defect (validated as bounded) | Fix | Slice |
|---|---|---|---|
| 1 | 2 live OMP sessions drift (append-only): `019f4eea` (running session, 3122→4322), `019f4eda` | frozen immutable point-in-time snapshots; cost items annotated | LiveSessionFreeze |
| 2 | rows.jsonl R4: 120 codex 12-hex-trunc + 69 iter-log null digests, 197/198 no excerpt, 7 rows on mutable `#L1` | full 64-hex digests, excerpts/R5 flags, re-anchored onto immutable cost records / frozen snapshots | RowsProvenanceFix |
| 3 | synthesis predates erratum #1 (reasoning double-count, ~50× overstate, 89-99%-every-harness) | erratum-synced lines; stale theme count 10→14 | SynthesisErratumSync + Main |
| 4 | research prose: 400→50 CC files, imprecise 2.4×, StampHog gate figure, Fable URL | entry-ratio/per-field split, README-authoritative gate, canonical URL | ResearchProseFix |
| 5 | review-artifact hygiene | canonicalized executed tests, S4-H7 landed-contract, S4-H2 cite, 2 real S5 tests, S3-H3 count, commit-hash freeze | ReviewArtifactHygiene |

Two previously-UNTESTED S5 unknowns are now settled with real runs: **39-rules-1:1 HOLDS**;
**0 genuine unanchored prescriptions** (all 26 S5-H1 hits are false positives).

## Blocking pre-execution DESIGN issue — RESOLVED

**RULE-7 self-collision.** `model_family` is a free-form declared config string, and the code gate
(`pipeline_projection.go:409`, `cc_pipeline.go:277`) is pure string equality
`CrossFamily.ModelFamily == Executor.ModelFamily` (the `omp` registry `provider` is irrelevant).
So a gpt-family executor (C3/C4/C5-gpt/C6-haiku) with the historically-fixed gpt-family adversarial
lens hard-fails the blocking gate.

**Fix (design/config/docs; code gate unweakened):** the cross-family adversarial gate is a
review-VALIDITY stage whose `model_family` is **pinned opposite the executor family per contrast**
(registered `cross-harness-adversarial-claude` = `claude-opus-4-8`/`claude` for gpt executors) —
this SATISFIES RULE-7, it is not an exception. The gate's stage-run is **excluded from the measured
frontier cell**; the cell is attributed to first-pass executor+verifier stage-runs; gate-induced
re-work and the gate's verdict/block-rate are reported per-contrast so the reviewer-family
strictness confound is visible, not silently folded into the executor delta. Deterministic route
map: `evidence/pareto/live-contrast-lens-map.md`. Rubric amendments:
`pareto-measurement-rubric.md` ("Cross-family adversarial gate — validity stage"),
`falsification-review-rubric.md` §5. Config verify passes; new lens resolves.

Executable as-preregistered without the flip: C1, C2, C5-claude-legs, C6-`gpt-5.6-sol`-gate-leg.
Requiring the opposite-family flip: C3, C4, C5-gpt-legs, C6-`haiku-4-5`-gate-leg.

## Still outstanding (separate, not this validation)

- **True cross-family adversarial gate** (falsification-review-rubric §5, GPT-family reviewer):
  PREPARED + ARMED, not handed off. Re-probed 2026-07-12T16:30Z — GPT/Codex quota still capped on
  every cheaply-reachable path (completion API ~213 min; codex Plus account "try again ~3:08 PM";
  Business account unreachable non-interactively and its usage display is untrustworthy). A blind
  **two-phase** harness lives in `reviews/cross-family-gate/` (phase 1 = blind pre-registration in
  `/tmp`, no repo; phase 2 = `-s read-only` execution of the FROZEN hypotheses on the repo), so the
  gate honors pre-registration-before-inspection. `run.sh` parses the verdict FIRST and only treats
  a cap phrase as BLOCKED on a parse miss (the reviewer cats artifacts containing
  `resource_exhausted`/`usage_limit_reached`, so a pre-parse cap check would false-discard a valid
  verdict). A background timer auto-runs it after the ~3.5h reset (retries ≤4×); the verdict lands
  at `reviews/cross-family-review.json`. Run manually anytime:
  `bash reviews/cross-family-gate/run.sh`. This mechanical Claude-family validation is NOT a
  substitute for that GPT-family gate.
- **`pareto-live-waves`**: pre-execution DESIGN gate is cleared, but the live A/B runs remain
  **user-gated** (cost). The paired-run harness is ready; live execution awaits explicit go.
- **Filed observation** (`.agents/proposals/obs-config-verify-staleness-message-misroutes.md`):
  `config verify` staleness message prints equal digests and misroutes the remedy to
  `da config sync`; captured, not fixed here.
