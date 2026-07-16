# Cost mechanics + stage granularity tuning

Operationalizes **§5** of the public guide `docs/full-loop-pipeline-craft.md`.
Load when deciding stage granularity, where to route a cheap-tier model, or how to account/report
token cost.

**Deep dive:** [`references/cost-granularity.md`](../references/cost-granularity.md) carries the
full accounting model, the design consequences, and the frontier discipline. This file is a
concise loader.

---

## The load-bearing facts

- **Cache-read dominance.** In long agent context, re-sent cached context dominates token volume.
  The magnitude is denominator- and harness-dependent — it is **not** a single cross-harness band;
  never quote one. Productive work (output + non-cached input; reasoning ⊆ output) is a modest
  slice. This is structural to long agent context, not an artifact of the loop.
- **Fixed per-request tax.** Tool definitions + system prompt are large and volume-fixed — a floor
  independent of task complexity. A trivial task can spend most of its context on tool definitions
  alone.

## Architect rules

- **Normalize every token-cost/volume axis on productive tokens** (output + non-cached input;
  reasoning ⊆ output — NEVER double-count it); report raw **and** cache-adjusted. NEVER price a
  stage on raw total tokens.
- **Reconstruct any missing-cost harness from `tokens × published-rate`, flag it as an
  inference,** and never mix it silently with recorded cost; treat a zero-dollar provider route as
  suspect.
- **To reduce token volume, change the pipeline** (compaction, fewer re-reads), not the executor
  model — an executor swap at a fixed snapshot barely moves volume.
- **Prefer coarse stage granularity on short tasks:** don't route a trivial task through a full
  tool-def context; cheap-tier savings scale with task length.
- **Route cheap tiers at low-generation stages first (review / verify), holding the executor at
  baseline,** and keep the cross-family gate. Accuracy risk follows a stage's generation share,
  not its read volume; the executor is the **last** stage to cheap-route.
- **NEVER assert a cost/efficiency frontier from historical rows;** require CI-backed **paired
  live contrasts** (one stage-model swap per contrast). All effect sizes are hypothesis-only until
  a live contrast validates them.

## Where cheap-tier is safe (ordered — hypothesis-grade until live contrasts validate)

1. **Routine review lenses** — lowest generation share (low output; often cache-cold ⇒ high
   uncached read volume that does not imply risk), gated read-only. Safest first target.
2. **Verifiers** — classification/checking workload, low generated output.
3. **Executor** — highest generation share; cheap-route **last** and only after a live paired
   contrast shows no accuracy regression, with the cross-family gate preserved.

## Config/command surface

Cheap-routing a stage is a model + model-family edit on the stage profile, never a code change
(the model family is open-ended). After editing, lint the config, then resolve the stage to
confirm the new route and that the cross-family gate still holds `reviewer.family !=
executor.family`. Validate a candidate route with a live paired contrast, not from history.
