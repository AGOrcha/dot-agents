# Orchestration Companion Lifecycle Hooks Plan

- plan-id: `orchestration-companion-stop-hooks`
- spec: [`../../specs/orchestration-companion-stop-hooks/design.md`](../../specs/orchestration-companion-stop-hooks/design.md)
- upstream prerequisite: `loop-discipline-stop-hooks`
- telemetry integration: `r1-5-hook-enforcement-telemetry`

## Summary

This follow-up covers the companion orchestration skills excluded from the
primary loop discipline gate set. It adds terminal enforcement only where a
skill declares a durable, branch-specific outcome, and bounded non-terminal
behavior only where an action can be prevented or context preserved without
claiming completion:

- `orchestrator-session-start`: a delegated handoff must materialize a
  consistent delegation contract and bundle.
- `delegation-lifecycle`: an expressly selected parent closeout must archive
  its evidence, clean active artifacts, and reconcile task state.
- `plan-wave-picker`: ship and modernize the skill, but do not attach a hook
  until a persisted selection receipt exists.

For the first two skills, verified `PreToolUse` may prevent invocation when a
declared pre-existing prerequisite is already missing, and `PreCompact` may
advise on an unresolved sentinel without blocking compaction. Post-tool
success/failure observation is evaluated through R1.5 rather than implemented
as a private companion mechanism.

## Grounded Coverage Decision

| Skill | Current durable evidence | Upstream overlap | v1 result |
| --- | --- | --- | --- |
| `orchestrator-session-start` | fanout creates delegation contract and bundle; sidecar/context may be included | `isp-gate` begins after handoff; no gate for the handoff itself | terminal handoff gate plus bounded prerequisite prevention/compaction advice |
| `delegation-lifecycle` | merge-back plus parent closeout archive and state cleanup | `loop-worker-gate` covers worker path; P3b scaffolds skill | terminal parent-closeout gate plus bounded prerequisite prevention/compaction advice |
| `plan-wave-picker` | `workflow eligible` is readable but accepted recommendation is not persisted | none | scaffold and modernize; no v1 hook |

Existing `session-orient` and `session-capture` hooks remain complementary
readback/checkpoint behavior. They do not prove any of these invariants.

## Companion Contracts

| Task | Contract | Outcome |
| --- | --- | --- |
| `t0-upstream-readback-and-starter-assets` | [`tasks/t0-upstream-readback-and-starter-assets.contract.md`](tasks/t0-upstream-readback-and-starter-assets.contract.md) | starter promotion and canonical wave-picker instructions |
| `t1-companion-sentinel-operations` | [`tasks/t1-companion-sentinel-operations.contract.md`](tasks/t1-companion-sentinel-operations.contract.md) | upstream sentinel extension only |
| `t2-orchestrator-handoff-gate` | [`tasks/t2-orchestrator-handoff-gate.contract.md`](tasks/t2-orchestrator-handoff-gate.contract.md) | fanout/existing-bundle handoff gate |
| `t3-delegation-closeout-gate` | [`tasks/t3-delegation-closeout-gate.contract.md`](tasks/t3-delegation-closeout-gate.contract.md) | parent-closeout gate |
| `t4-wave-picker-no-hook-assessment` | [`tasks/t4-wave-picker-no-hook-assessment.contract.md`](tasks/t4-wave-picker-no-hook-assessment.contract.md) | explicit no-hook decision and receipt options |
| `t5-integration-and-outcome-compatibility` | [`tasks/t5-integration-and-outcome-compatibility.contract.md`](tasks/t5-integration-and-outcome-compatibility.contract.md) | portable gate tests and telemetry compatibility |

## Sequence

### T0 - Starter assets and wave-picker alignment

After the primary plan's P3b has landed `delegation-lifecycle`, copy the
complete `orchestrator-session-start` and `plan-wave-picker` trees into the
starter. Bring `plan-wave-picker` guidance into alignment with the canonical
workflow plan and `workflow eligible` model; its legacy markdown workflow is
compatibility context only.

### T1 - Sentinel operations

After the primary sentinel CLI exists, add the minimum accepted skill and
operation context needed for `fanout_handoff`, `existing_bundle_handoff`, and
`parent_closeout`. Completed companion records use the same history archive
contract as the primary plan.

### T2 - Orchestrator handoff gate

Create `orchestrator-handoff-gate` under the global hook scaffold and wire the
starter skill to open a sentinel only on a delegated handoff branch. Its
terminal path validates bundle/contract/scope/required-context consistency;
its verified pre-action path may prevent fanout only for a missing declared
pre-existing prerequisite, and its pre-compaction path advises only. It is
not an ISP gate and it is not a worker gate.

### T3 - Delegation closeout gate

Create `delegation-closeout-gate` and wire the starter delegation skill only
for the parent closeout branch. Its terminal path validates archive creation,
active cleanup, and canonical task state after a declared `accept` or
`reject`; its verified pre-action path may prevent closeout when the declared
merge-back or decision prerequisite is absent, and its pre-compaction path
advises only.

### T4 - Wave-picker assessment

Persist a short historical decision record showing why no gate is justified:
the skill has no durable accepted-selection artifact. Describe the two viable
future receipt locations but implement neither.

### T5 - Integration and telemetry compatibility

Add a flat shell smoke test for both gates, following existing test layout.
When R1.5 capture is available, verify companion outcomes are ingested through
the common contract and hand off post-tool success/failure observation
fixtures for its T1b evaluation. Otherwise the task proves the result
vocabulary and leaves observation/capture ownership with R1.5.

## Dependencies

```text
loop-discipline-stop-hooks P3b --> t0-upstream-readback-and-starter-assets --> t2-orchestrator-handoff-gate --+
loop-discipline-stop-hooks P0  --> t1-companion-sentinel-operations -------> t2-orchestrator-handoff-gate --+--> t5-integration-and-outcome-compatibility
loop-discipline-stop-hooks P3b --> t0-upstream-readback-and-starter-assets --> t3-delegation-closeout-gate --+
loop-discipline-stop-hooks P0  --> t1-companion-sentinel-operations -------> t3-delegation-closeout-gate --+
t0-upstream-readback-and-starter-assets --> t4-wave-picker-no-hook-assessment ------------------------------+
r1-5-hook-enforcement-telemetry capture (when shipped) -----------------------------------------------------> t5
```

The canonical task ledger records only intra-plan dependencies. The named
upstream and telemetry dependencies are external scheduling gates.

## Verification Checkpoints

- After T0: starter-copy tests show the complete orchestrator and wave-picker
  skill trees materialize and upstream `delegation-lifecycle` remains intact.
- After T1: sentinel unit tests cover new allowed skills, operations, invalid
  contexts, and history archival.
- After T2: bundle missing/mismatch/sidecar tests prove deterministic
  orchestrator terminal remediation, verified pre-action prerequisite
  prevention, and non-blocking compaction advice; direct execution creates no
  gate obligation.
- After T3: accept/reject archive and cleanup tests prove deterministic parent
  closeout remediation, verified pre-action prerequisite prevention, and
  non-blocking compaction advice; worker path is bypassed.
- After T4: the decision artifact states why no `plan-wave-picker` hook exists
  and identifies the future receipt design decision.
- After T5: `tests/test-orchestration-companion-stop-hooks.sh` exercises hard,
  advisory, and bypass paths through upstream native output handling, with
  R1.5 compatibility checked when available.

## Non-Goals

- Modifying either parent plan during this follow-up drafting pass.
- Making hook code choose a task, fan out work, or close out delegation.
- Parsing transcripts to establish a hard violation.
- Attaching a stop hook to a selection skill that has no persisted selected
  outcome.
- Recording or scoring post-tool feedback outside the R1.5 decision path.
