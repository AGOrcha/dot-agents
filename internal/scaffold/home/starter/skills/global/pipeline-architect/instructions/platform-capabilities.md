# Per-harness capability matrix

Operationalizes **§6** of the public guide `docs/full-loop-pipeline-craft.md`.
Load when onboarding a new platform/harness or app-type, or deciding what a projection must
include/omit. A projection MUST be **per-harness** because the loop mechanism and telemetry
differ radically.

**Deep dive:** [`references/platform-projection.md`](../references/platform-projection.md) carries
the full archetype matrix, projection shapes, and the telemetry-mask/scope rules. This file is a
concise loader.

---

## The matrix (by archetype, never product identity)

| archetype | drives the workflow CLI? | orchestration primitive | telemetry axes it can feed |
|---|---|---|---|
| CLI-native, full telemetry | yes (heavy) | prescribed-skill-driven | tokens, cost, wallclock, model, tool-result |
| CLI-native, no cost telemetry | yes (heavy) | prescribed-skill-driven | tokens, partial wallclock, model, tool-result; **no cost** |
| artifact-reader / native-orchestration | never (reads workflow artifacts as context) | own primitives (full-auto) | tokens, model, tool-result; **no cost** |
| contract-native, no CLI | runs the loop-worker/orchestrator contract natively, no CLI | native task spawning | **none** of tokens/cost/wallclock/model; **zero tool-result** |
| minimal / smoke | n/a | — | tokens, credits (not cost), wallclock, model |

## Archetype → projection shape

- **CLI-native:** project a driver that calls the workflow engine directly
  (slots/eligible/fanout/resolve-prompt). The no-cost variant differs only in its telemetry mask.
- **artifact-reader:** project a **no-CLI, artifact-reading** driver — it reads workflow artifacts
  as context and orchestrates via its own primitives. Do NOT project a CLI caller.
- **contract-native:** project the loop-worker/orchestrator **contract** natively with no CLI, and
  carry no telemetry expectations.
- **minimal:** smoke only; calibration/exclusion rows, not workflow delivery.

## Architect rules

- **Emit a distinct per-harness loop projection from one profile IR;** NEVER assume one swarm
  shape serves every harness.
- **Attach a telemetry-capability mask to every Pareto cell and hard-exclude a harness from any
  axis it cannot record** (the exclusion is a format property, invariant to workflow-vs-advisory
  use). A cell is `model_family × task_class × cache_regime × retry_regime`.
- **Gate mechanism/orchestration projection on "is this a workflow session?";** apply
  cost/resilience rules broadly. Never apply mechanism/orchestration projection to advisory chat.
- **For an artifact-reading harness, project a no-CLI, artifact-reading driver** (it orchestrates
  via its own primitives), not a CLI caller.
- **Record the origin harness on bridged work** so cross-harness cost roll-ups don't
  mis-attribute.

## Onboarding a new app-type or platform

1. **app-type:** add its verifier + lens sets to the execution profile and any new stage profiles;
   resolve topology/lenses per app-type; lint; spot-check with the resolve seam; emit.
2. **platform/harness:** pick the archetype above, then emit through the projector interface. File
   the transformer requirement as a proposal parallel to the platform-handling doc when the
   projector does not yet exist. Never hand-write the emitted YAML.
3. **Never score a new harness on a telemetry axis it cannot supply** — set its capability mask
   first.
