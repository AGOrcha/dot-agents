Task: Review `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md` using the `Self-Review` and `review-pr` skills.

Scope:
- `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`
- corroborating implementation files for workflow, review, and hook behavior

Findings:
1. High: the proposal schema is single-file only, but the MVP explicitly routes multi-file hook bundles and skills through that queue. This leaves bundle creation and update semantics undefined.
2. High: proposal targets are only bounded to `~/.agents/`, which allows reviewed proposals to overwrite operational state such as workflow context or proposal internals instead of only shared behavior artifacts.
3. Medium: project context paths are keyed only by `<project>`, which is derived from `.agentsrc.json.project` or the repo basename. Different repos can collide and overwrite checkpoints or logs.
4. Medium: the `guard-commands` contract claims exact destructive matching, but the blocklist and current implementation rely on substring-style behavior and include an over-broad `truncate` token.
5. Medium: the product spec hardcodes `CLAUDE_PROJECT_DIR` in a cross-platform workflow contract and omits the canonical repo-local lesson file layout while still treating lessons as managed artifacts.
6. Low: the documented `workflow checkpoint` flags drift from the current command surface.

Artifacts reviewed:
- `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`
- `commands/workflow.go`
- `commands/review.go`
- `internal/config/proposals.go`
- `internal/scaffold/hooks/global/guard-commands/guard.sh`
- `internal/scaffold/hooks/global/session-orient/orient.sh`
- `internal/scaffold/hooks/global/session-capture/capture.sh`

Verification:
- No tests run. This was a review-only task.

Verdict:
- NEEDS FIXES before using the spec as the sole implementation contract for a weaker agent.
