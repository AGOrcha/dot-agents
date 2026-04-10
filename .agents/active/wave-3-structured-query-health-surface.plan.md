# Wave 3: Structured Query And Health Surface

Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 3
Status: Completed (2026-04-10)
Depends on: Wave 2 (canonical plan/task artifacts)

## Goal

Provide a stable machine-readable surface over workflow state so agents can retrieve current workflow state with one query instead of reconstructing it from multiple files. Track verification and tool-health data without bloating checkpoints.

## Artifacts Introduced

| Path | Purpose |
|------|---------|
| `~/.agents/context/<project>/verification-log.jsonl` | Append-only verification run history |
| `~/.agents/context/<project>/health.json` | Current environment and workflow health snapshot |

## Implementation Steps

### Step 1: Verification log infrastructure

- [ ] `VerificationRecord` struct — schema_version, timestamp, kind (test/lint/build/format/custom), status (pass/fail/partial/unknown), command, scope (file/package/repo/custom), summary, artifacts[], recorded_by. JSON tags only (JSONL format).
- [ ] `isValidVerificationKind()`, `isValidVerificationScope()` validation helpers
- [ ] `appendVerificationLog(project string, record VerificationRecord) error` — append JSON line to `verification-log.jsonl` in `config.ProjectContextDir(project)`
- [ ] `readVerificationLog(project string, limit int) ([]VerificationRecord, error)` — read last N entries. Graceful if file absent.
- [ ] Tests: append round-trip, read with limit, missing file returns empty

### Step 2: Health snapshot infrastructure

- [ ] `HealthSnapshot` struct:
  - schema_version, timestamp
  - git: inside_repo, branch, dirty_file_count
  - workflow: has_active_plan, has_checkpoint, pending_proposals, canonical_plan_count (from Wave 2)
  - tooling: mcp, auth, formatter (all string: "available"/"unavailable"/"unknown")
  - status: "healthy"/"warn"/"error"
  - warnings[]
- [ ] `writeHealthSnapshot(project string, snapshot HealthSnapshot) error` — write to `health.json`
- [ ] `readHealthSnapshot(project string) (*HealthSnapshot, error)` — graceful if absent
- [ ] `computeHealthSnapshot(state *workflowOrientState) HealthSnapshot` — derive from existing workflow state collection
- [ ] Tests: write/read round-trip, compute from state with various conditions (no plans, dirty files, pending proposals trigger warnings)

### Step 3: `workflow status --json` enhancement

- [ ] Add `--json` flag to existing `statusCmd`
- [ ] When `--json` or `Flags.JSON`: output `workflowOrientState` as JSON (already done in orient, replicate pattern)
- [ ] Also write `health.json` as side effect of status query
- [ ] Tests: verify JSON output shape

### Step 4: `workflow health` subcommand

- [ ] `healthCmd` (Use: "health") with `runWorkflowHealth()`:
  1. Collect workflow state
  2. Compute health snapshot
  3. Write health.json
  4. Display: status badge, git state, workflow state, tooling state, warnings
  5. `--json` flag for machine-readable output
- [ ] Tests: health output with healthy/warn states

### Step 5: `workflow verify` subcommands

- [ ] `verifyCmd` (Use: "verify") parent command
- [ ] `verifyRecordCmd` (Use: "record") with `runWorkflowVerifyRecord()`:
  - Required flags: `--kind`, `--status`, `--summary`
  - Optional flags: `--command`, `--scope` (default "repo")
  - Appends to verification-log.jsonl
  - `ui.Success()` confirmation
- [ ] `verifyLogCmd` (Use: "log") with `runWorkflowVerifyLog()`:
  - Shows last 10 entries by default
  - `--all` flag for full history
  - `--json` flag for machine output
- [ ] Register: `verifyCmd.AddCommand(verifyRecordCmd, verifyLogCmd)`
- [ ] Tests: record and retrieve, log limits, JSON output

### Step 6: Integration with orient

- [ ] Add `Health *HealthSnapshot` field to `workflowOrientState`
- [ ] Update `collectWorkflowState()` to compute and include health
- [ ] Update `renderWorkflowOrientMarkdown()` to add "# Health" section showing status and warnings
- [ ] Update orient JSON output to include health
- [ ] Tests: orient output includes health section

## Files Modified

- `commands/workflow.go`
- `commands/workflow_test.go`

## Acceptance Criteria

An agent can retrieve the repo's current workflow state with one machine-readable query (`workflow status --json` or `workflow health --json`) instead of reconstructing it from multiple files.

## Verification

```bash
go test ./commands -run 'Health|Verify|VerificationLog'
go test ./commands
go test ./...
```
