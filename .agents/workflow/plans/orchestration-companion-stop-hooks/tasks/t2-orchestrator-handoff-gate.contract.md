# T2 Orchestrator Handoff Gate Contract

- task: `t2-orchestrator-handoff-gate`
- external prerequisite: completed upstream hook/native-output implementation
- dependencies: T0 and T1
- requirements: R3, R6, D1

## Goal

Enforce the observable delegated handoff produced by
`orchestrator-session-start` before ISP or a worker consumes it, prevent only
a verified missing-prerequisite invocation before fanout, and preserve
unresolved handoff context before compaction. This is not an end-to-end
orchestration gate.

## Bundle and Wiring

Create:

```text
internal/scaffold/hooks/global/orchestrator-handoff-gate/HOOK.yaml
internal/scaffold/hooks/global/orchestrator-handoff-gate/gate.sh
```

The manifest uses terminal `stop`, plus `pre_tool_use` and `pre_compact` only
where upstream P1c establishes their platform input/output behavior. It uses
the same enabled/required platform policy and native remediation handling
established by the upstream lifecycle gates.

Update the starter `orchestrator-session-start` skill so:

- no sentinel is written for direct execution;
- after choosing `fanout_handoff`, it writes a sentinel before invoking
  `workflow fanout`;
- after choosing `existing_bundle_handoff`, it writes a sentinel before
  briefing or chaining the worker;
- the skill clears/archives the sentinel only after the handoff gate passes
  through the upstream mechanism.

## Deterministic Rules

| Rule | Evidence | Outcome |
| --- | --- | --- |
| Expected delegation contract is absent | declared path does not exist | hard remediation |
| Expected bundle is absent | declared path does not exist | hard remediation |
| Contract/bundle/sentinel disagree on plan, task, delegation ID, or write scope | parseable durable artifacts | hard remediation |
| Required medium/high-confidence scope sidecar is not included as bundle context | sentinel-declared sidecar and bundle `context.required_files` | hard remediation |
| Delegated bundle has no `selection.reason` | bundle field | advisory until unresolved decision 1 is settled |
| Direct path or no sentinel | sentinel absence or operation mismatch | silent allow |

## Non-Terminal Rules

| Event | Rule | Outcome |
| --- | --- | --- |
| `pre_tool_use` | attempted `workflow fanout` has an active handoff sentinel whose declared pre-existing required sidecar is missing | hard remediation before execution |
| `pre_tool_use` | bundle output is not yet created by `workflow fanout` | never evaluate as a precondition |
| `pre_compact` | a delegated-handoff sentinel remains unresolved | advisory continuity context; never block compaction |

## Non-Duplication Boundary

Do not inspect:

- ISP stage completion, parent gate, or bundle execution results;
- worker changes, verification, checkpoint, or merge-back;
- parent closeout.

Those are upstream `isp-gate`, upstream `loop-worker-gate`, or T3 ownership.

## Acceptance

- Scaffold-copy test includes `HOOK.yaml` and `gate.sh`.
- Script tests cover fanout pass, existing-bundle pass, missing artifact,
  scope mismatch, omitted required sidecar, pre-action missing-sidecar
  prevention, compaction advice, selection-reason advisory, and
  direct/no-sentinel bypass.
- Hard results use the upstream native-output mechanism for every supported
  platform; the script does not embed an assumed universal JSON payload.

## Out of Scope

- Transcript-backed hard rules.
- Making a selection or running `workflow fanout` from the hook.
