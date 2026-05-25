# T4 Wave-Picker Assessment Contract

- task: `t4-wave-picker-no-hook-assessment`
- dependency: T0
- requirements: R5, D3

## Goal

Preserve the negative design decision: `plan-wave-picker` is worthy of
starter promotion and canonical-state correction, but it does not yet have a
truthful Stop-hook contract.

## Assessment Artifact

Write:

```text
.agents/history/orchestration-companion-stop-hooks/plan-wave-picker-assessment.md
```

The record must cover:

- its current input surfaces: canonical plan/task ledgers, `workflow eligible`
  JSON, scope sidecars, and compatibility markdown;
- the lack of a durable artifact recording which recommendation was accepted;
- why transcript observation would provide advice at most, not a mandatory
  persisted decision;
- why generic `session-capture` does not fill this gap.

## Future Options to Evaluate, Not Implement

| Option | Possible evidence | Concern |
| --- | --- | --- |
| New `workflow selection record` atom | explicit eligible snapshot ID plus selected task/batch | adds a new user-visible write obligation |
| Extend `workflow start-task` | selected task already becomes plan focus and scope evidence is derived | does not represent recommendation-only or rejected batch decisions |

Any future hook remains advisory until a separately approved contract requires
the receipt.

## Acceptance

- The assessment names the recommended no-hook v1 result unambiguously.
- No `plan-wave-picker` hook bundle or sentinel operation is added by any
  task in this plan.

## Out of Scope

- Implementing either receipt option.
- Blocking a stop based on conversational selection text.
