# Wave 6: Delegation And Merge-Back

Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 6
Status: Completed (2026-04-10) — all 7 steps implemented and tested.
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

- [x] `DelegationContract` struct:
  - schema_version, id, parent_plan_id, parent_task_id
  - title, summary
  - write_scope ([]string — file/directory patterns this delegate may touch)
  - success_criteria (string)
  - verification_expectations (string)
  - may_mutate_workflow_state (bool)
  - owner (string — delegate agent identity)
  - status: pending/active/completed/failed/cancelled
  - created_at, updated_at
- [x] `isValidDelegationStatus()` validation
- [x] `loadDelegationContract(projectPath, taskID) (*DelegationContract, error)`
- [x] `saveDelegationContract(projectPath string, contract *DelegationContract) error`
- [x] `listDelegationContracts(projectPath string) ([]string, error)`
- [x] Tests: round-trip, list, validation

### Step 2: Write-scope validation

- [x] `writeScopeOverlaps(existing, newScope, excludeTaskID)` — detect overlapping write scopes across active delegations. Returns list of conflict descriptions.
- [x] Uses prefix containment for non-wildcard patterns (90%+ of real cases per RFC)
- [x] Overlap check runs on delegation creation
- [x] Tests: non-overlapping passes, overlapping detected, nested patterns handled, completed delegation skipped

### Step 3: Merge-back artifact types

- [x] `MergeBackSummary` struct with all required fields
- [x] `saveMergeBack` — writes markdown with YAML frontmatter to `.agents/active/merge-back/<task-id>.md`
- [x] `loadMergeBack` — parses YAML frontmatter from merge-back file
- [x] Tests: write/read round-trip

### Step 4: Coordination intent types

Transport-neutral coordination semantics (not raw chat syntax):

- [x] `CoordinationIntent` enum type: `status_request`, `review_request`, `escalation_notice`, `ack`
- [x] Stored as `pending_intent` field in `DelegationContract`
- [x] No chat syntax or @mentions anywhere in storage

### Step 5: `workflow fanout` subcommand

- [x] `fanoutCmd` (Use: "fanout") with `runWorkflowFanout()`:
  - Required flags: `--plan <plan-id>`, `--task <task-id>`
  - Optional flags: `--owner`, `--write-scope` (comma-separated)
  1. Loads and validates plan + task exist
  2. Validates task not already delegated
  3. Checks write-scope overlaps against active delegations
  4. Creates `.agents/active/delegation/<task-id>.yaml`
  5. Advances task status to `in_progress`

### Step 6: `workflow merge-back` subcommand

- [x] `mergeBackCmd` (Use: "merge-back") with `runWorkflowMergeBack()`:
  - Required flags: `--task <task-id>`, `--summary`
  - Optional flags: `--verification-status`, `--integration-notes`
  1. Loads delegation contract
  2. Collects changed files via git diff
  3. Creates merge-back summary at `.agents/active/merge-back/<task-id>.md`
  4. Updates delegation status to completed

### Step 7: Integration with orient/status

- [x] Add `ActiveDelegations` (`workflowDelegationSummary`) to `workflowOrientState`
- [x] Add `PendingMergeBacks int` to `workflowOrientState`
- [x] `collectDelegationSummary` — counts active delegations and pending intents
- [x] `renderWorkflowOrientMarkdown()` — "# Delegations" section (active count, pending intents, merge-back count)
- [x] `runWorkflowStatus()` — prints active delegations and pending merge-backs
- [x] Tests: empty project, active with intent, completed not counted

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
