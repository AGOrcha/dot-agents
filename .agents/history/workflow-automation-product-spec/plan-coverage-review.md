# Workflow Automation Plan Coverage Review

This note compares the preliminary plan in `/Users/nikashp/.claude/plans/happy-seeking-iverson.md` against `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`.

## What The Preliminary Plan Already Covers Well

- It identifies the right product gap: `dot-agents` needs a workflow layer, not just more config projection.
- It correctly centers the MVP on orient, persist, and propose behavior.
- It uses the existing canonical hook bundle design instead of inventing a parallel hook system.
- It sequences the work in a practical order:
  - hook bundles first
  - scaffolding and detection second
  - workflow commands third
  - proposal review last
- It includes concrete starter bundles for session orient, session capture, destructive-command guarding, secret scanning, and auto-formatting.
- It keeps the first phase low-risk by avoiding Go changes.

## Gaps The Product Spec Closes

- The preliminary plan is solution-first. The product spec separates:
  - problem and goals
  - product decisions
  - exact artifact contracts
  - implementation phases
- The preliminary plan leaves several MVP decisions implicit. The product spec resolves:
  - repo-local versus user-local artifact boundaries
  - whether workflow commands are primary UX or escape hatches
  - proposal archive behavior after approval or rejection
  - no-MCP-server scope for the MVP
  - single-writer assumption for concurrent agents
  - exact approval gradient boundaries
- The preliminary plan does not define exact canonical schemas. The product spec adds:
  - checkpoint schema
  - session-log entry format
  - proposal schema
  - orient data model and output contract
- The preliminary plan implies verification state matters but does not make it mandatory in the checkpoint schema. The product spec makes verification summary and next action required.
- The preliminary plan names pending proposals but does not fully specify target safety. The product spec requires proposal targets to stay relative to `~/.agents/` and rejects absolute or traversing paths.
- The preliminary plan includes future-oriented concerns from the research indirectly, but it does not explicitly defer them. The product spec marks these as deferred:
  - MCP workflow query surface
  - persisted repo preferences
  - multi-agent coordination
  - fan-out and merge-back workflow commands
  - bash parity

## Required Plan Adjustments Before Implementation

- Treat `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md` as the behavioral source of truth.
- Update checkpoint writing tasks so they include:
  - `schema_version`
  - `verification.status`
  - `verification.summary`
  - `next_action`
  - `blockers`
- Update proposal-review tasks so approved proposals are archived after successful apply and refresh, not left in the pending queue.
- Make it explicit that `dot-agents workflow ...` commands are support and inspection commands, not the main human workflow.
- Keep proposal creation file-based for the MVP. Do not add a new `dot-agents propose` command in this pass.
- Keep runtime MCP integration and multi-agent coordination out of scope for this implementation.

## Net Result

The preliminary plan remains a good implementation skeleton.

The new product spec makes it decision-complete enough that a weaker implementation agent should be able to execute it without inventing missing behavior.
