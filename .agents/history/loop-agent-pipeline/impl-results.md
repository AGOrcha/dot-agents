## 1. p10a — CLI Schema Field Parity Fix (2026-04-19)

**Task:** `loop-agent-pipeline / p10a-cli-schema-field-parity`
**Status:** Completed

### What changed

`commands/workflow/cmd.go` and `commands/workflow/plan_task.go` — three CLI surfaces now expose fields that existed in structs but had no flag wiring:

| Command | New flag(s) |
|---|---|
| `workflow plan create` | `--success-criteria`, `--verification-strategy` |
| `workflow plan update` | `--success-criteria`, `--verification-strategy` |
| `workflow task add` | `--app-type` |

### Root cause

`CanonicalPlan` already had `SuccessCriteria` and `VerificationStrategy` fields; `CanonicalTask` already had `AppType`. The run functions simply never received or set these values. Every new plan created via the CLI required manual YAML edits to add them.

### Verification

`go test ./commands/workflow/...` — green. CLI smoke: `workflow plan create` and `workflow task add` help text shows new flags.

### Fold-back origin

`cli-schema-field-drift` — recorded against p10 (`p10-workflow-command-decomposition`), auto-approved as defect, new task p10a created and completed same session.

---

## 2. Session — Spec Synthesis and Plan Authoring (2026-04-19)

### New spec: config-distribution-model

Created `.agents/workflow/specs/config-distribution-model/design.md` as the canonical interface spec between `org-config-resolution` and `external-agent-sources`. Covers:
- Two-tier model (config layers vs executable packages)
- `sources` / `extends` / `packages` field surface and `source-id:path@ref` reference syntax
- Source type constraints (tier 1 = git/http/local; tier 2 = oci/http)
- Two-pass resolution engine
- `.agentsrc.lock` format with separate config and packages sections
- Per-tier caching semantics
- Audit event taxonomy additions
- `da config explain` command contract
- `da config` and `da packages` command subtree with disposition of `install`, `refresh`, `import`, `sync`, `explain`

Updated `org-config-resolution/design.md` and `external-agent-sources/design.md` to reference config-distribution-model as the canonical field surface spec and fixed inconsistent syntax examples (`type: registry` → typed sources with `id`; bare extends → `source-id:path` refs).

### New plan: kg-command-surface-readiness

Created `.agents/workflow/plans/kg-command-surface-readiness/` (7 tasks). Extends graph-bridge-command-readiness to the full `kg` command surface across 4 slices:
- Slice 1 (`kg-freshness-audit` / `kg-freshness-impl`): code-graph freshness and provenance — entry point, unblocks planner-evidence plan
- Slice 2 (`kg-change-impact-audit` / `kg-change-impact-impl`): change and impact trustworthiness
- Slice 3 (`kg-advanced-surfaces-audit`): flows/communities/postprocess readiness decisions
- Slice 4 (`kg-mcp-transport-audit` / `kg-mcp-transport-impl`): MCP parity

### New plan: planner-evidence-backed-write-scope

Created `.agents/workflow/plans/planner-evidence-backed-write-scope/` (6 tasks). Introduces the `.scope.yaml` sidecar model for evidence-backed write_scope:
- `sidecar-schema`: schema + Go types (unblocked now)
- `sidecar-manual-experiment`: hand-author 2 real sidecars to validate shape
- `derive-scope-command`: `workflow plan derive-scope` command (gated on kg-freshness-impl)
- `check-scope-command`: `workflow plan check-scope` command
- `skill-upgrades`: orchestrator-session-start, agent-start, plan-wave-picker
- `fanout-evidence-integration`: fanout warnings for missing evidence
