# Template: Self-Assessment Persistence Line

After running iteration-close commands, confirm these in the loop-state self-assessment block:

```
- persisted_via_workflow_commands: yes
- proposal_queued: yes (<id>) | no
```

If you closed a **delegated** slice, mention that merge-back was recorded
(parent still owes `delegation closeout`; it does not also advance the
delegated task), e.g.:

```
- delegation_handoff: merge-back recorded for <task-id>; parent to close out
```

If only verify record was run but checkpoint was skipped (e.g., dry-run mode):
```
- persisted_via_workflow_commands: partial (verify only)
```

If the iteration intentionally skipped persistence (e.g., no net code change):
```
- persisted_via_workflow_commands: n/a (no code change this iteration)
```

## Checkpoint Readback

After running `da workflow checkpoint`, confirm the new state:
```bash
da workflow status
# Expected: "Next action" now reflects the checkpoint message you just wrote
# Not: stale "Status: Completed (2026-04-11)" text from a prior session

da workflow log
# Expected: most recent entry matches this iteration's --message
```

If `workflow status` still shows stale text, note it explicitly:
```
- persisted_via_workflow_commands: yes (checkpoint written; status readback still stale — known issue)
```
