# Workflow: Delegation Lifecycle

Use this skill when delegating a task to a sub-agent with a bounded write scope and integrating the result back into the canonical plan.

## Commands

### 1. Fanout

Run from the parent agent to create the delegation contract.

```bash
go run ./cmd/dot-agents workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --write-scope "commands/,internal/config/" \
  --owner "sub-agent-name"
```

Expected effects:
- creates `.agents/active/delegation/<task-id>.yaml`
- validates that the plan and task exist
- validates that the task is `pending` or `in_progress`
- rejects overlapping active write scopes
- advances the task to `in_progress`

### 2. Merge-back

Run after the delegated work is complete.

```bash
go run ./cmd/dot-agents workflow merge-back \
  --task <task-id> \
  --summary "Implemented X by doing Y" \
  --verification-status pass \
  --integration-notes "No conflicts, parent should advance task to completed"
```

Expected effects:
- creates `.agents/active/merge-back/<task-id>.md`
- records git-diffed changed files
- marks the delegation as completed

### 3. Orient

Run from the parent agent to inspect delegation state.

```bash
go run ./cmd/dot-agents workflow orient
# or
go run ./cmd/dot-agents workflow status
```

Expected output:
- `# Delegations` section
- active delegation count
- pending intents count
- merge-back count

## Coordination Intents

If a sub-agent needs to signal something back to the parent, set `pending_intent` on the delegation contract. Valid values:

- `status_request`
- `review_request`
- `escalation_notice`
- `ack`
