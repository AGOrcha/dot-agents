# ISP Step 3: Direct Work vs Fanout

Work **directly** only when the task is research, planning, architecture, or interactive user collaboration with no bounded `write_scope`.

Otherwise create or reuse bounded delegations:
- One canonical delegated contract per task — do not split impl, verifier, and review into separate top-level tasks.
- In parallel fanout mode, one orchestrator pass may create multiple non-overlapping bundles.
- In scoped completion mode, stay serialized to one task per pass.

## Decision tree

```
Does a bundle already exist for the selected task?
├── YES → Reuse it. Go to instructions/fanout.md (staged runtime).
└── NO  → Can the task be bounded by write_scope AND no active delegation overlaps it?
          ├── YES → workflow fanout (see instructions/fanout.md)
          └── NO  → Direct execution in-session
```

If write_scope_declared is false for the selected task, surface a caution before fanning out. Consider running `workflow derive-scope` or flagging for manual scope review.
