# Wave 6: Delegation And Merge-Back

Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 6
Status: Directional backlog (requires RFC before coding)
Depends on: Wave 5 (knowledge-graph bridge), stable single-agent workflow model

## Goal

Make delegated multi-agent work explicit and bounded. Require ownership of write scope. Produce merge-back artifacts that reduce integration guesswork for parent agents.

## Pre-Implementation Requirements

This wave requires a focused RFC before coding due to:
- Write-scope overlap is a correctness problem, not just UX
- Naive append-only coordination creates drift
- Transport-specific agent protocols should not be baked into canonical storage prematurely

The RFC should resolve: concurrency model (lock-based vs reservation-based), conflict detection strategy, and how delegation interacts with canonical plan/task artifacts from Wave 2.

## Artifacts Introduced

| Path | Purpose |
|------|---------|
| `.agents/active/delegation/<task-id>.yaml` | Delegated task contract |
| `.agents/active/merge-back/<task-id>.md` | Subagent return summary for parent integration |

## Implementation Steps

### Step 1: Delegation contract types

- [ ] `DelegationContract` struct:
  - schema_version, id, parent_plan_id, parent_task_id
  - title, summary
  - write_scope ([]string — file/directory patterns this delegate may touch)
  - success_criteria (string)
  - verification_expectations (string)
  - may_mutate_workflow_state (bool)
  - owner (string — delegate agent identity)
  - status: pending/active/completed/failed/cancelled
  - created_at, updated_at
- [ ] `isValidDelegationStatus()` validation
- [ ] `loadDelegationContract(projectPath, taskID) (*DelegationContract, error)`
- [ ] `saveDelegationContract(projectPath string, contract *DelegationContract) error`
- [ ] `listDelegationContracts(projectPath string) ([]string, error)`
- [ ] Tests: round-trip, list, validation

### Step 2: Write-scope validation

- [ ] `validateWriteScope(contracts []DelegationContract) []string` — detect overlapping write scopes across active delegations. Returns list of conflict descriptions.
- [ ] Uses `filepath.Match` or glob-based overlap detection
- [ ] Overlap check runs on delegation creation and on advance to active
- [ ] Tests: non-overlapping passes, overlapping detected, nested patterns handled

### Step 3: Merge-back artifact types

- [ ] `MergeBackSummary` struct:
  - schema_version, task_id, parent_plan_id
  - title, summary
  - files_changed ([]string)
  - verification_result: status, summary
  - integration_notes (string — guidance for parent agent)
  - blockers_encountered ([]string)
  - created_at
- [ ] `saveMergeBack(projectPath string, summary *MergeBackSummary) error` — writes markdown to `.agents/active/merge-back/<task-id>.md` with YAML frontmatter
- [ ] `loadMergeBack(projectPath, taskID string) (*MergeBackSummary, error)`
- [ ] Tests: write/read round-trip

### Step 4: Coordination intent types

Transport-neutral coordination semantics (not raw chat syntax):

- [ ] `CoordinationIntent` enum type: `status_request`, `review_request`, `escalation_notice`, `ack`
- [ ] Stored as fields in delegation contract, not as marker strings
- [ ] `DelegationContract` gets `pending_intent` field (optional CoordinationIntent)
- [ ] Parent/delegate can set/clear intents via advance command
- [ ] Tests: intent lifecycle

### Step 5: `workflow fanout` subcommand

- [ ] `fanoutCmd` (Use: "fanout") with `runWorkflowFanout()`:
  - Required flags: `--plan <plan-id>`, `--task <task-id>`
  - Optional flags: `--owner`, `--write-scope` (comma-separated)
  1. Load canonical plan and task from Wave 2
  2. Validate task exists and is not already delegated
  3. Check write-scope overlaps against existing active delegations
  4. Create delegation contract under `.agents/active/delegation/`
  5. Update task status to `in_progress` with delegation marker
  6. `ui.Success()` with contract path
- [ ] Tests: create delegation, overlap rejected, missing plan/task errors

### Step 6: `workflow merge-back` subcommand

- [ ] `mergeBackCmd` (Use: "merge-back") with `runWorkflowMergeBack()`:
  - Required flag: `--task <task-id>`
  - Optional flags: `--summary`, `--verification-status`, `--integration-notes`
  1. Load delegation contract
  2. Collect changed files (git diff against delegation start point)
  3. Create merge-back summary
  4. Update delegation status to completed
  5. `ui.Success()` with merge-back path
- [ ] Tests: merge-back created, delegation status updated

### Step 7: Integration with orient/status

- [ ] Add `ActiveDelegations` to `workflowOrientState` — count, any with pending intents
- [ ] Add `PendingMergeBacks` — count of unprocessed merge-back artifacts
- [ ] Update `renderWorkflowOrientMarkdown()` — "# Delegations" section
- [ ] Update `runWorkflowStatus()` — delegation count
- [ ] Tests: orient reflects delegation state

## Files Modified

- `commands/workflow.go`
- `commands/workflow_test.go`

## Blocking Risks

- Write-scope overlap is a correctness problem — needs thorough testing
- Naive append-only coordination will create drift — coordination intents must be explicit
- Transport-specific protocols (chat markers, @mentions) must NOT enter canonical storage

## Acceptance Criteria

This wave should only start after the single-agent workflow model is stable and verified in real use. Complete when:
- Delegated work has explicit, bounded contracts
- Write scope is validated to prevent overlap
- Parent agents can consume merge-back summaries for integration decisions

## Verification

```bash
go test ./commands -run 'Delegation|Fanout|MergeBack|WriteScope'
go test ./commands
go test ./...
```
