---
name: "release-cut"
description: "Use when cutting a tagged release after docs are reconciled: preflight pin-check the signing toolchain, push the version bump / tag, monitor auto-release.yml, and on an infra-class failure clean the stale tag + workflow_dispatch re-drive and classify the known sign/timestamp failures. Runs AFTER release-docs-refresh. Not for everyday tagging or for doc edits."
argument-hint: "[--version <x.y.z>] [--dispatch-only] [--watch <run-id>]"
---

# Release Cut

Drive a tagged release through `auto-release.yml` to a verified, signed publish — and recover cleanly when the pipeline fails on infra (not on a real regression).

This is the successor step to the **`release-docs-refresh`** skill: that one reconciles scope/spec/user-facing docs against the code *before* the version bump; this one performs the cut itself and owns the run-to-publish loop. Run `release-docs-refresh` first — `release-cut` assumes docs are already reconciled and only the VERSION/CHANGELOG bump remains.

## Workflow

0. **Review failure points**
   Load → `instructions/gotchas.md`
   Read the stale-tag, kernel32, DLSequence, and timestamp-flake pitfalls **before** touching the tag. Recovery is much cheaper when the known signatures are already in context.

1. **Run the cut**
   Load → `instructions/workflow.md`
   Preflight pin-check the signing toolchain → confirm/push the version bump → monitor `auto-release.yml` → on infra failure clean the stale tag and `workflow_dispatch` re-drive → classify the run outcome.

2. **Classify a failure (only if the run is red)**
   Load → `references/known-failures.md` and `references/auto-release-stepmap.md`
   Map the failed step + log signature to a class (toolchain re-pin / re-drive vs. real regression) and route accordingly. Do not re-diagnose from scratch — match the signature.

3. **Gate before declaring done**
   Load → `eval/checklist.md`
   A release is high-stakes; run the pass/fail gate (release exists, Cosign verify-blob passed, no stale tag left behind) before reporting success.

## Why this is a skill

The multi-step branching judgment — *preflight pin-check → cut → monitor → on infra-class failure clean the stale tag + re-drive → classify known signatures* — is exactly the flow the `pin-release-toolchain-and-make-releases-retryable` lesson exists to make routine. See that lesson for the *why*; this skill is the *how*.
