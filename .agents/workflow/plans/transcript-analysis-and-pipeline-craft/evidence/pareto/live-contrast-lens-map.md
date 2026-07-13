# Live-contrast route map — executor swap × cross-family adversarial gate

Deterministic per-contrast routing for the `pareto-live-waves` task. Resolves the RULE-7
self-collision (`falsification-review-rubric.md:23-32`, `pareto-measurement-rubric.md`
"Cross-family adversarial gate — validity stage"). Contrast definitions:
`historical-hypotheses.md` §2 table.

## The rule (one sentence)

The cross-family adversarial gate's `model_family` is a **dependent variable pinned opposite
the executor family** — never a constant, never an independently-swept axis. The code gate
(`internal/platform/pipeline_projection.go:409`, `internal/platform/cc_pipeline.go:277`) stays
unweakened; it hard-refuses `reviewer.family == executor.family`, and opposite-pinning is what
keeps every contrast on the legal side of it.

## Registered adversarial lenses (`.agentsrc.json` `reviewers`)

| slug | model | model_family | use when executor family is |
|---|---|---|---|
| `cross-harness-adversarial`        | `gpt-5.4`         | `gpt`    | `claude` |
| `cross-harness-adversarial-claude` | `claude-opus-4-8` | `claude` | `gpt`    |

Both compose the same (model-agnostic) prompt files
(`reviewers/reviewer.base.md`, `reviewers/cross-harness-adversarial.md`,
`reviewers/cross-harness-adversarial.project.md`); the lens routes to a different *harness* at
runtime regardless, so the declared `model_family` is purely the static gate proxy.

**Operational note (2026-07-13 audit).** The "flip" is MANUAL, by construction: no code path
selects `cross-harness-adversarial-claude` automatically (`resolveCrossFamily` /
`profile_prompt.go` take an explicit slug; verified — zero non-config references to the
`-claude` key). A gpt-family executor arm whose lens_set still names plain
`cross-harness-adversarial` hits the family-equality refusal and blocks (fail-safe, but the
arm dies). Therefore every C3/C4/C5-gpt-leg/C6-gpt-gate arm config MUST pin
`cross-harness-adversarial-claude` in its lens_set at wave prep. Auto family-swap wiring is
tracked as an open fold-back (`cross-family-lens-autoswap`), not assumed.

## Per-contrast map

Baseline executor = `claude-opus-4-8` (`claude`). "Swap" = the single measured-stage change.

| id | swapped stage | executor route (family) | adversarial gate lens / model / family | gate legal? | executable as-preregistered |
|---|---|---|---|---|---|
| **C1** | executor | `claude-sonnet-5` (claude) | `cross-harness-adversarial` / `gpt-5.4` / gpt | gpt ≠ claude ✓ | **YES** |
| **C2** | executor | `claude-haiku-4-5` (claude) | `cross-harness-adversarial` / `gpt-5.4` / gpt | gpt ≠ claude ✓ | **YES** |
| **C3** | executor | `gpt-5.6-terra` (gpt) | `cross-harness-adversarial-claude` / `claude-opus-4-8` / claude | claude ≠ gpt ✓ | needs family flip |
| **C4** | executor | `gpt-5.6-sol` (gpt) | `cross-harness-adversarial-claude` / `claude-opus-4-8` / claude | claude ≠ gpt ✓ | needs family flip |
| **C5** | executor (stratified) | PRIMARY `sonnet-5`(claude)/`terra`(gpt) vs SECONDARY `haiku-4-5`(claude)/`sol`(gpt) | per leg: claude leg → `cross-harness-adversarial` (gpt); gpt leg → `cross-harness-adversarial-claude` (claude) | ✓ per leg | claude legs **YES**; gpt legs need flip |
| **C6** | review (cheapen), executor held `opus-4-8` (claude) | `claude-opus-4-8` (claude), fixed | if the cheapened stage IS the adversarial gate → opposite-family cheap model `gpt-5.6-sol` (gpt); the `haiku-4-5` leg is valid ONLY for a **routine** verifier/lens slot (no family constraint), never the gate | ✓ | `gpt-5.6-sol`-gate leg **YES**; `haiku-4-5` only on a routine slot |

| **C0** | none — A/A null (2026-07-12 amendment; rubric step 4a) | `claude-opus-4-8` (claude), both arms | `cross-harness-adversarial` / `gpt-5.4` / gpt | gpt ≠ claude ✓ | **YES — runs FIRST, gates C1-C6** |

**Executable now, no flip:** **C0 (first, gating)**, C1, C2, C5-claude-legs (`sonnet-5`/`haiku-4-5`), C6-`gpt-5.6-sol`-gate-leg.
**Require the opposite-family flip (use `cross-harness-adversarial-claude`):** C3, C4,
C5-gpt-legs (`terra`/`sol`), C6-`haiku-4-5`-gate-leg (illegal on the gate — reroute `haiku-4-5`
to a routine verifier slot instead).

## Measurement rules carried from the rubric

1. The adversarial gate's own stage-run (tokens/$/wall-clock) is **excluded** from the frontier
   cost/accuracy cell — reported separately as review-validity overhead.
2. The cell is attributed to the **first-pass** executor + verifier stage-runs.
   Gate-REJECT-induced re-work is a separate stage-run, reported per-contrast, NOT summed into
   the executor cell.
3. **Every contrast MUST report** the gate's verdict distribution (accept/reject/block counts)
   and induced re-work iteration count next to the cell, so the per-contrast gate-strictness
   asymmetry (gpt gate on claude-exec contrasts vs claude gate on gpt-exec contrasts) is visible.
   A frontier claim is invalid if the executor-swap delta cannot be separated from the
   gate-strictness delta at the reported block rates.
