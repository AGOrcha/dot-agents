# Local transcript analysis + pipeline craft extraction

## Goal

Run a structured, rubric-gated evidence pass over this machine's actual session transcripts,
synthesize it with the two prior analyses (payout/dot-agents `analysis/top-level-basic-from-coordination-state`,
provadm/roos `feature/analysis-doc`), and convert the full-loop OMP exercise's craft into three
reusable assets: a pipeline-architect skill, a config→materialized-pipeline transformer layer,
and a 4-axis Pareto experiment harness for stage-level model routing.

## Methodology gate (task `methodology-rubrics`)

All analysis is gated on the rubrics under `methodology/`:
- `evidence-rubric.md` — inventory fields, anchor discipline, class taxonomy, redaction gate
  (R1–R5: no raw transcripts committed; secret-scan; minimal excerpts; digest-auditable anchors;
  sensitivity triage), confidence grading.
- `falsification-review-rubric.md` — pre-registered refutation hypotheses, executed not argued,
  null results first-class, cross-family blocking gate; zero-refutation review = not performed.
- `pareto-measurement-rubric.md` — stage-run unit, explicit dominance directions, wall-clock
  decomposition (model latency vs tool vs queue; critical-path), historical = hypothesis-only,
  live = paired snapshot-identical repeats with CIs.

## Architecture (three-layer stack; from inventory)

1. **Layer 1 — canonical IR** (`internal/config/profile_*.go`, `execution_profile.go`,
   `internal/platform/resource_{intent,plan}.go`): profiles + stage routing + resource intents.
   The pipeline-architect skill operates HERE.
2. **Layer 2 — projections**: `.agents/workflow/runtime/full-loop/*.swarm.yaml` (today
   hand-written = the gap; becomes EMITTED), `profile_prompt.go` / `config relevance` envelopes,
   `.claude/workflows/*.mjs` (ultracode-wave-engine already consumes the relevance envelope —
   proven consumer). OMP platform proposal (`omp-platform-handling.md`) is the transformer-layer
   beachhead, structurally parallel to hook projection per platform.
3. **Layer 3 — drift + verification**: `profile_drift.go`, falsification-first review,
   acceptance tests on emitted projections.

## Task graph

`methodology-rubrics` gates everything. Analysis track:
`corpus-inventory → evidence-extraction → synthesis` (+ `prior-analysis-sync` parallel).
Pareto track: `pareto-historical → pareto-live-waves` (candidates corrected 2026-07-12:
PRIMARY `claude-sonnet-5` + `gpt-5.6-terra` — both resolvable in the `omp models` registry;
SECONDARY cheap tier `claude-haiku-4-5`/`gpt-5.6-sol`, user-confirmed; baseline
`claude-opus-4-8`). Craft track: `craft-extraction → {pipeline-architect-skill,
platform-projection-layer → cc-workflows-consumer}`. `falsification-review` closes all
substantive slices.

## Constraints

- Pre-existing queue debt (unreviewed proposals in both `~/.agents/` and `./.agents/`,
  ~10 unrouted fold-backs, 7 stale-looking delegation bundles) is acknowledged; this plan was
  user-directed over the drain-first rule. Synthesis output routes INTO that proposal queue
  rather than around it.
- Transcript dirs are read-only; evidence artifacts live under this plan's `evidence/`.
- No AI-attribution trailers anywhere (propagates to all delegated workers).
