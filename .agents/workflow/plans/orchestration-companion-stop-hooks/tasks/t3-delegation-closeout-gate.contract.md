# T3 Delegation Closeout Gate Contract

- task: `t3-delegation-closeout-gate`
- external prerequisites: completed upstream P3b starter tree and hook output contract
- dependencies: T0 and T1
- requirements: R4, R6, D2

## Goal

Enforce only the parent closeout promise made by `delegation-lifecycle`,
prevent an invoked closeout whose declared pre-existing prerequisite is
already absent, and preserve pending-closeout context before compaction.
Worker execution and fanout creation already have separate owners.

## Bundle and Wiring

Create:

```text
internal/scaffold/hooks/global/delegation-closeout-gate/HOOK.yaml
internal/scaffold/hooks/global/delegation-closeout-gate/gate.sh
```

The manifest uses terminal `stop`, plus `pre_tool_use` and `pre_compact` only
where upstream P1c verifies those event contracts. Wire the starter skill to
write a `parent_closeout` sentinel after the parent chooses the task and
`accept|reject` decision and immediately before invoking `workflow delegation
closeout`.

Do not write this sentinel when:

- the skill is merely creating a fanout handoff;
- the worker is implementing or writing merge-back;
- a completed merge-back is intentionally being left for parent review in a
  later turn.

## Deterministic Rules

| Rule | Evidence | Outcome |
| --- | --- | --- |
| Declared history archive omits `delegation.yaml`, `merge-back.md`, or `closeout.yaml` | history artifact paths | hard remediation |
| Active delegation, bundle, or merge-back survives declared closeout | declared active paths | hard remediation |
| `accept` fails to yield completed canonical task | `TASKS.yaml` | hard remediation |
| `reject` fails to yield blocked canonical task | `TASKS.yaml` | hard remediation |
| Completed merge-back exists with no parent-closeout sentinel | active state only; intent unknown | advisory only |
| Worker path / no sentinel | sentinel absence or operation mismatch | silent allow |

## Non-Terminal Rules

| Event | Rule | Outcome |
| --- | --- | --- |
| `pre_tool_use` | attempted `workflow delegation closeout` has a sentinel-declared merge-back artifact or decision prerequisite already missing | hard remediation before execution |
| `pre_compact` | a parent-closeout sentinel remains unresolved | advisory continuity context; never block compaction |

## Acceptance

- Scaffold-copy test includes both bundle files.
- Script tests cover valid accept, valid reject, missing archive element,
  stale active artifact, mismatched canonical task status, pre-action
  missing-prerequisite prevention, compaction advice, no-sentinel advisory,
  and worker-path bypass.
- Remediation relies only on declared durable evidence and the upstream
  native-output mechanism.

## Out of Scope

- Duplicating `loop-worker-gate`.
- Closing a delegation automatically from the hook.
- Treating a cross-turn pending parent review as a deterministic breach.
