# Deep dive — per-harness capability matrix and projection (§6)

Operational depth behind `instructions/platform-capabilities.md`. Model- and registry-agnostic:
harnesses are described by **archetype and capability**, never by product identity, and no
plan-scoped evidence anchor appears. Source of truth: `docs/full-loop-pipeline-craft.md` §6.

---

## Why projection is per-harness

A projection MUST be **per-harness** because the loop mechanism and the telemetry differ
radically across harnesses. Some harnesses drive the workflow CLI directly; others never touch
it and read workflow artifacts as context; still others run the loop-worker/orchestrator
contract natively with no CLI at all. Describe each target by *archetype and capability*, not by
product identity — a product name dates, an archetype does not.

### The archetype matrix

| archetype | drives the workflow CLI? | orchestration primitive | telemetry axes it can feed |
|---|---|---|---|
| CLI-native, full telemetry | yes (heavy) | prescribed-skill-driven | tokens, cost, wallclock, model, tool-result |
| CLI-native, no cost telemetry | yes (heavy) | prescribed-skill-driven | tokens, partial wallclock, model, tool-result; **no cost** |
| artifact-reader / native-orchestration | never (reads workflow artifacts as context) | own primitives (full-auto) | tokens, model, tool-result; **no cost** |
| contract-native, no CLI | runs the loop-worker/orchestrator contract natively, without the CLI | native task spawning | **none** of tokens/cost/wallclock/model; **zero tool-result** |
| minimal / smoke | n/a | — | tokens, credits (not cost), wallclock, model |

The rows are not a ranking; they are a **capability partition**. Two harnesses in the same row
can share a projection shape; two in different rows cannot.

---

## Archetype → projection shape

- **CLI-native (either telemetry variant):** project a driver that calls the workflow engine
  directly for slots/eligible/fanout/resolve-prompt. This is the canonical driver shape; the
  no-cost variant differs only in its telemetry mask, not its control flow.
- **artifact-reader / native-orchestration:** project a **no-CLI, artifact-reading** driver — it
  reads workflow artifacts as context and orchestrates via its own primitives. Do NOT project a
  CLI caller for this archetype; it will never call the engine.
- **contract-native, no CLI:** project the loop-worker/orchestrator **contract** natively with
  no CLI, and carry **no telemetry expectations** — this archetype records none.
- **minimal / smoke:** calibration/exclusion rows only, not workflow delivery.

---

## Consequences the projection layer must encode

- **A single emitted projection cannot serve every archetype.** Because the archetypes differ on
  the most basic axis — *does it drive the CLI at all?* — one swarm shape cannot serve them all.
  File the transformer requirement as a proposal parallel to the platform-handling doc when a
  projector for an archetype does not yet exist.
- **The cost/Pareto cell must carry a telemetry-capability mask.** A cell is
  `model_family × task_class × cache_regime × retry_regime`; **hard-exclude** any harness from an
  axis it cannot record (e.g. a harness that records no tokens/cost/wallclock/model from all
  four). The exclusion is a **format property**, invariant to workflow-vs-advisory use — a
  harness that cannot record an axis cannot record it in any mode. Never score a cell on an axis
  its harness cannot supply; a masked axis is absent, not zero.
- **Scope findings correctly.** Cost and resilience outcomes generalize *past* the workflow
  sample and apply corpus-wide; mechanism/orchestration projection MUST be gated on "is this a
  workflow session?" and never applied to advisory chat. Mixing the two scopes over-claims the
  mechanism findings and under-claims the cost ones.
- **Record bridge origin.** When one harness spawns work on another (a real recurring bridge),
  record the **originating** harness so cross-harness cost roll-ups attribute bridged
  cost/outcome back to the spawning orchestrator and don't double-count it against both.

---

## Onboarding a new app-type or harness

1. **app-type:** add its verifier and lens sets to the execution profile plus any new stage
   profiles; resolve topology/lenses for the app-type; lint the config; spot-check each route
   with the resolve seam; emit.
2. **harness:** pick the archetype above, then emit through the projector interface. When the
   projector for that archetype does not yet exist, file the transformer requirement as a
   proposal parallel to the platform-handling doc. Never hand-write the emitted projection.
3. **Set the capability mask first.** Never score a new harness on a telemetry axis it cannot
   supply — the mask is a precondition of any cost cell that includes it.

---

## Rules

- Emit a distinct per-harness loop projection from one profile IR; NEVER assume one swarm shape
  serves every harness.
- Attach a telemetry-capability mask to every Pareto cell and hard-exclude a harness from any
  axis it cannot record.
- Gate mechanism/orchestration projection on "is this a workflow session?"; apply cost/resilience
  rules broadly.
- For an artifact-reading harness, project a no-CLI, artifact-reading driver (it orchestrates via
  its own primitives), not a CLI caller.
- Record the origin harness on bridged work so cross-harness cost roll-ups don't mis-attribute.
