---
name: "release-cut"
description: "Use when cutting a tagged release after docs are reconciled: preflight pin-check the signing toolchain, push the version bump / tag, monitor the release workflow, and on an infra-class failure clean the stale tag + re-drive and classify known sign/timestamp failures. Runs AFTER release-docs-refresh. Not for everyday tagging or for doc edits."
argument-hint: "[--version <x.y.z>] [--dispatch-only] [--watch <run-id>]"
---

# Release Cut

Drive a tagged release through the project's release workflow to a verified, signed
publish — and recover cleanly when the pipeline fails on infra (not on a real
regression).

This is the successor step to the **`release-docs-refresh`** skill: that one
reconciles scope/spec/user-facing docs against the code *before* the version bump;
this one performs the cut itself and owns the run-to-publish loop. Run
`release-docs-refresh` first — `release-cut` assumes docs are already reconciled and
only the VERSION/CHANGELOG bump remains.

## Resolve the project's release specifics first

This skill describes the **generic** release-cut pattern. The concrete values it
needs are a **project overlay**, not baked into the skill — resolve them up front
from `da config relevance` (the per-`app_type` execution profile) and the project's
release docs / CI config before you start:

- **the release workflow file** (the workflow that triggers on the version bump and
  drives build → sign → publish; e.g. an `auto-release`-style workflow) and its
  trigger contract (push-on-`VERSION`-change vs. `workflow_dispatch` re-drive).
- **the signing/timestamp toolchain** the project uses (the signing tools, how they
  are pinned, and the done-signal — e.g. a signature-verification step).
- **the known-failure signatures** for this project's toolchain (the project's own
  failure classifier — see the project-example reference below).

If the project ships a concrete release-workflow reference (a step map + a
known-failure classifier), `references/` here is an **illustrative example** of that
shape — read the project's own reference as authoritative, and treat the bundled
example as a template.

## Workflow

0. **Review failure points**
   Load → `instructions/gotchas.md`
   Read the generic pitfalls (stale-tag-on-retry, a non-runnable signing tool, an
   unpinned-toolchain regression, transient signing/timestamp flake) **before**
   touching the tag, then map them to the project's concrete signatures. Recovery is
   much cheaper when the known signatures are already in context.

1. **Run the cut**
   Load → `instructions/workflow.md`
   Preflight pin-check the signing toolchain → confirm/push the version bump →
   monitor the release workflow → on infra failure clean the stale tag and re-drive
   → classify the run outcome.

2. **Classify a failure (only if the run is red)**
   Load → `references/known-failures.md` and `references/auto-release-stepmap.md`
   (the project-example references; prefer the project's own classifier + step map).
   Map the failed step + log signature to a class (toolchain re-pin / re-drive vs.
   real regression) and route accordingly. Do not re-diagnose from scratch — match
   the signature.

3. **Gate before declaring done**
   Load → `eval/checklist.md`
   A release is high-stakes; run the pass/fail gate (release exists, signature
   verification passed, no stale tag left behind) before reporting success.

## Why this is a skill

The multi-step branching judgment — *preflight pin-check → cut → monitor → on
infra-class failure clean the stale tag + re-drive → classify known signatures* — is
exactly the flow the `pin-release-toolchain-and-make-releases-retryable` lesson exists
to make routine. See that lesson for the *why*; this skill is the *how*.
