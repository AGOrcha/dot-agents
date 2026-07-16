---
name: "pipeline-architect"
description: "Design, edit, and maintain full-loop execution pipelines, and onboard new or custom execution profiles/config. Use when designing or altering an execution profile, stage_profiles, a verifier or review chain, or model routing; when binding a cross-family review gate; when onboarding a new app_type or a target platform/harness; or when tuning stage granularity and cheap-tier routing for cost. The skill EDITS canonical config (`.agentsrc.json` stage_profiles + execution_profile) and treats the runtime swarm YAML as a generated build artifact — never a hand-authored source. Every prescription is model- and registry-agnostic craft, stated as checkable invariants."
argument-hint: "[--app-type <type>] [--platform <target-harness>] [--stage <executor|verifier|reviewer|orchestrator>]"
---

# Pipeline Architect

Build, alter, and maintain full-loop execution pipelines from the **Layer-1 profile IR**, and onboard new or custom execution profiles/platforms. The invariants below are transferable craft, not a description of one script: any emitted pipeline on any harness must satisfy them.

**Source of truth.** The public, operational full-loop guide — model- and registry-agnostic, with checkable rules per section — is `docs/full-loop-pipeline-craft.md` (§1–§8). Each instruction file operationalizes one section of that guide, and each loads a **co-located deep dive** under `references/` for the full mechanism:

- `instructions/design-pipeline.md` → `references/design-loop-routing.md` (§1–§3)
- `instructions/verification-review.md` → `references/verification-review.md` (§4)
- `instructions/cost-tuning.md` → `references/cost-granularity.md` (§5)
- `instructions/platform-capabilities.md` → `references/platform-projection.md` (§6)
- `instructions/gotchas.md` → `references/anti-patterns.md` (§7)

Instructions are concise loaders; the co-located `references/` doc carries the specifics. No prescription in this skill names a specific model, vendor, or closed capability registry.

**Two-layer model.** The profile IR (`.agentsrc.json` `stage_profiles` + `execution_profile`) is Layer-1 source. The runtime swarm YAML (`.agents/workflow/runtime/full-loop/*.swarm.yaml`) is a **Layer-2 build artifact**, emitted per-harness by `da workflow pipeline emit --platform <p>`. You edit Layer-1 and regenerate Layer-2; you NEVER hand-edit the emitted YAML (§7).

## Config surface this skill edits

- `.agentsrc.json` → `stage_profiles` (two-level: `stage → slug → StageProfile{label, model, model_family, prompt_files, precondition_policy}`) and `execution_profile` (topology / `by_app_type` verifier+lens sets). Legacy `verifier_profiles` / `reviewer_profiles` / `app_type_verifier_map` fold into `stage_profiles` (new key wins; migrate with `da config migrate`).
- `.agents/workflow/runtime/full-loop/*.swarm.yaml` → **generated**; regenerate via the emitter, never edit by hand.
- Register/refresh a custom skill or profile with `da skills promote <name>` then `da refresh`.

## Workflow

0. **Frame the task.** Decide which change class you are making — new/edited stage or model route, verifier/review chain, cost/granularity tune, or onboarding a new app_type/platform — then load only the matching instruction file(s) below. Read the guide section (and its `references/` deep dive) before editing config.

1. **Design the loop + routing.**
   Load → `instructions/design-pipeline.md` (§1–§3; deep dive `references/design-loop-routing.md`)
   Slot/eligible/fanout skeleton, typed `stage_profiles`, `model` + `model_family` routing (cross-family binding by the **named** adversarial lens, never a numeric index), and lifecycle/recovery contracts.

2. **Wire the verification + review spine.**
   Load → `instructions/verification-review.md` (§4; deep dive `references/verification-review.md`)
   Bounded verifier chain and routine lens sets, falsification-first review contract, structured verdicts bound to the harness's native gate, and evidence gates (anchor + real tool record, never self-report).

3. **Tune cost / granularity.**
   Load → `instructions/cost-tuning.md` (§5; deep dive `references/cost-granularity.md`)
   Cache-read dominance, productive-token accounting, stage granularity vs the fixed per-request tax, and where cheap-tier models are safe (review/routine first, executor last, gated by live contrasts).

4. **Project per platform.**
   Load → `instructions/platform-capabilities.md` (§6; deep dive `references/platform-projection.md`)
   Per-harness archetype (CLI-native / artifact-reader / contract-native / minimal), the telemetry axes each can feed, and what a projection for each archetype must include/omit.

5. **Design against the anti-patterns.**
   Load → `instructions/gotchas.md` (§7; deep dive `references/anti-patterns.md`)
   Each failure mode and the invariant that defeats it. Read this **before** shipping a config edit or emit.

## Verify (dry-run walkthrough)

Run these from the repo root after any Layer-1 edit; all four gate a change before it ships. None of them mutates canonical workflow state.

1. **Lint the config layers** (validates `stage_profiles` schema + fold-in):
   ```
   da config lint            # human output; non-zero exit on any invalid layer
   da config lint --json     # LintReport{ok,...} for scripting
   ```
   If you migrated legacy keys: `da config migrate --dry-run` then `da config verify`.

2. **Resolve topology + lenses** for the touched app_type (confirms the verifier/lens sets and their order):
   ```
   da config relevance --filter topology --app-type <type> --json
   da config relevance --filter lenses   --app-type <type> --json
   ```

3. **Spot-check the projection surface** — resolve-prompt per stage slug; assert non-empty `model` AND `model_family`, and (for the cross-family gate) `reviewer.family != executor.family`:
   ```
   da --json workflow resolve-prompt --kind executor --slug default
   da --json workflow resolve-prompt --kind verifier --slug <slug>
   da --json workflow resolve-prompt --kind reviewer --slug <adversarial-lens-slug>
   ```
   A matched stage with empty `model`/`model_family` is a hard failure — fix Layer-1, do not ship.

4. **Emit + validate the swarm projection** (Layer-2 build artifact; must be byte-identical on re-emit):
   ```
   da workflow pipeline emit --platform <p> --app-type <type> --dry-run   # preview, no write
   da workflow pipeline emit --platform <p> --app-type <type>             # regenerate artifact
   ```
   The emitted swarm DSL is schema-validated by the target harness's swarm parser; the outer driver refuses to run if either YAML is missing. Deterministic driver behavior is pinned by the runtime's driver tests (normal / failure / recovery / fanout-failure).

If you added or changed the skill itself: `da skills promote pipeline-architect && da refresh`.
