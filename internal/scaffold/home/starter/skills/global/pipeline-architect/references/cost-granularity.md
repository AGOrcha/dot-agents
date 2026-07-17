# Deep dive — cost mechanics and stage granularity (§5)

Operational depth behind `instructions/cost-tuning.md`. Model- and registry-agnostic: no model,
vendor, published rate, or corpus measurement is named, and no plan-scoped evidence anchor
appears. Source of truth: `docs/full-loop-pipeline-craft.md` §5.

---

## The load-bearing facts

### Cache-read dominance

In long agent context, **re-sent cached context dominates token volume**. Each turn re-sends the
accumulated context, and that re-sent slice grows with the transcript, so cache-read tokens
swamp everything else. The *magnitude* is denominator- and harness-dependent — each harness
reports on its own token schema — so it is **not** a single cross-harness band; never quote one.
Productive work (output plus non-cached input, where reasoning is a subset of output and is never
added again) is a modest slice of the total.

This dominance is **structural to long agent context, not an artifact of the loop or the
sampler**: it shows up in one-off scratchpad and review sessions too. You cannot pipeline your
way out of the *fact* — only manage its *volume* through the context (see §8 of the public
guide: compaction and fewer re-reads).

### Productive-token accounting

Raw total-token counts **overstate cost** because they double-count cached re-reads (and, on
some schemas, reasoning). Compute every token-volume and token-cost axis on the **productive**
figure — output plus non-cached input, with reasoning treated as a subset of output and never
added twice — and report raw alongside cache-adjusted so a reader can see both the sticker price
and the real one.

Dollar attribution is gappy and route-dependent: some harnesses record cost, some are
token-only, some record nothing, and some provider routes bill zero. Any cross-harness dollar
comparison must **reconstruct** a missing-cost harness from `tokens × published-rate`, flag it as
an inference, and never silently mix reconstructed with recorded cost. Treat a zero-dollar
provider route as **suspect** — a real request that bills nothing is a telemetry gap, not free
work.

### Fixed per-request tax

Tool definitions plus the system prompt are large and **volume-fixed** — a floor that is
independent of task complexity. A trivial task can spend most of its context on tool definitions
alone. This floor is model-priced (the tokens re-price with the tier) but volume-fixed (the
count does not shrink for a small task), which is what makes stage granularity a real lever.

---

## Design consequences

- **Context reuse gates volume, not the model.** At a fixed context snapshot, swapping the
  executor model barely moves token volume: the productive fraction is small and the cache
  volume is set by context size, not by which model reads it. To move volume you must change the
  **pipeline** — compaction, fewer re-reads — not the model.
- **Dollar savings are cache-read-rate bound.** Only a minority of total dollars is
  outcome-addressable by swapping tiers; the majority re-prices purely by the swapped tier's
  cache-read rate. A tier swap is a *re-pricing* of the same volume far more than it is a
  *reduction* of volume.
- **Stage granularity trades against the fixed tax.** Cheap-tier fractional savings rise with
  task length; the shortest tasks yield the least because the fixed tool-definition block is
  model-priced but volume-fixed. Don't route a trivial task through a heavy tool-def context —
  the overhead dominates and the savings never materialize.
- **Accuracy risk is localized to the generated-output fraction.** A weaker model degrades
  accuracy only in proportion to a task's model-generated output share. Low-generation stages
  (context-shuffling, review) are near-zero-risk cheap-route targets **even when their uncached
  read volume is high** — read volume is not generation, and accuracy risk follows generation.
- **The review/verify stage is the highest-leverage cheap route.** Review turns generate very
  low output — a classification workload, not a generation workload — so routing review to a
  cheap tier saves at near-zero accuracy risk, **provided the cross-family gate is preserved**
  (a cheap reviewer is still a *different-family* reviewer). Cheap-route order is therefore:
  routine review lenses first, verifiers next, and the **executor last** — and the executor only
  after a live paired contrast shows no accuracy regression.

---

## Frontier discipline

Every effect size above is a **hypothesis** about shape, not a measured frontier. A cost/
efficiency frontier is a **live, paired-contrast artifact** — one stage-model swap per contrast,
measured under CI — never a claim reconstructed from historical rows. Historical rows suggest
where to point a contrast; they never license a frontier assertion on their own.

---

## Cheap-tier ordering (hypothesis-grade until live contrasts validate)

1. **Routine review lenses** — lowest generation share (low output; often cache-cold, so high
   uncached read volume that does *not* imply accuracy risk), gated read-only. Safest first
   target.
2. **Verifiers** — classification/checking workload, low generated output.
3. **Executor** — highest generation share; cheap-route **last**, and only after a live paired
   contrast shows no accuracy regression, with the cross-family gate preserved.

---

## Rules

- Normalize every token-cost/volume axis on productive tokens (output + non-cached input;
  reasoning ⊆ output); report raw plus cache-adjusted. NEVER price a stage on raw total tokens.
- Reconstruct any missing-cost harness from `tokens × published-rate`, flag it as an inference,
  and never mix it silently with recorded cost; treat a zero-dollar provider route as suspect.
- To reduce token volume, change the pipeline (compaction, fewer re-reads), not the executor
  model.
- Prefer coarse stage granularity on short tasks: don't route a trivial task through a full
  tool-def context; cheap-tier savings scale with task length.
- Route cheap tiers at low-generation stages first (review/verify), holding the executor at
  baseline; the executor is the LAST stage to cheap-route. Keep the cross-family gate.
- NEVER assert a cost/efficiency frontier from historical rows; require CI-backed paired live
  contrasts.
