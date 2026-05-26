# T5 Integration and Outcome Compatibility Contract

- task: `t5-integration-and-outcome-compatibility`
- dependencies: T2, T3, T4
- external coordination: `r1-5-hook-enforcement-telemetry`
- requirements: R6, DC6

## Goal

Demonstrate that the two new gates enforce only their declared portable
evidence and can join the common enforcement-outcome pipeline without
forking telemetry.

## Shell Integration Test

Create `tests/test-orchestration-companion-stop-hooks.sh` following the
existing flat shell-test convention. At minimum, exercise:

| Scenario | Expected result |
| --- | --- |
| valid orchestrator delegated handoff | allow and archive sentinel |
| missing or inconsistent orchestrator bundle | hard remediation through native adapter |
| valid delegation parent `accept` closeout | allow and archive sentinel |
| incomplete closeout archive or stale active artifact | hard remediation through native adapter |
| completed merge-back without declared closeout intent | advisory, not hard remediation |
| direct orchestration or worker lifecycle branch | bypass companion gates |
| missing declared handoff or closeout prerequisite at verified `PreToolUse` | preventive hard remediation before workflow command |
| unresolved companion sentinel at `PreCompact` | continuity advice without blocking compaction |

Use fixture repository artifacts rather than transcript content for hard
cases.

## R1.5 Coordination

- If R1.5 capture has shipped, add integration assertions that companion gate
  results persist using the same `allow`, `advise`, or `remediate` contract
  and archive linkage as primary gates.
- If R1.5 capture has not shipped, assert only the common result vocabulary
  accepted by that planned contract and document the missing external
  scheduling gate; do not create a private companion telemetry document.
- In either case, provide bounded `PostToolUse` / `PostToolUseFailure`
  workflow-command fixtures for R1.5 T1b evaluation; do not persist or score
  them in companion code.

## Acceptance

- Both companion bundles retain the upstream hook timeout and platform-native
  output behavior.
- A negative test proves no `plan-wave-picker` bundle is emitted.
- The test does not require a readable platform transcript to establish any
  hard outcome.

## Out of Scope

- Changing R1.5 plan files.
- Adding transcript persistence or scoring behavior directly in this plan.
