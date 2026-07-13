# Gotchas: Orchestrator Session Start

## Do Not Turn The Orchestrator Into A Worker

- The orchestrator should choose and bound work, not become the place where large implementation happens.
- If the same agent starts selecting work, implementing it, and reconciling every observation, the focused loop discipline collapses.
- If a worker refuses a task because the scope isn't bounded (e.g. `loop-worker` rejecting a multi-tier PR-shipping prompt without a bundle), the correct response is to spawn a `general-purpose` worker instead — NOT to brief the `loop-worker` into acting like an orchestrator. The profile enforcement is intentional.

## Canonical State Wins

- Prefer `workflow next`, `workflow tasks`, and delegation contracts over stale checkpoint prose.
- If markdown plan notes disagree with canonical task state, treat that as drift to reconcile, not as permission to ignore the canonical layer.
- After fanout, prefer the delegation **bundle** (`.agents/active/delegation-bundles/<delegation_id>.yaml`) over improvised prompts when briefing a worker.
- Canonical plans live at `.agents/workflow/plans/<plan-id>/` — NOT `.agents/active/`. If a script or instruction references `.agents/active/*.plan.md`, treat it as a stale path and fix it.

## Validate Task State Against Reality Before Fanout

- The full pre-fanout gate is canonical in **`delegation-lifecycle` → `instructions/workflow.md` § 0** (status-vs-PRs, write_scope-on-HEAD, caller walk, coverage-delta forecast, no-overlap). Clear that gate before every fanout — it is a hard MUST, not advice. Lessons: `[[validate-bundle-against-head]]`, `[[bundle-scope-via-code-graph]]`, `[[stale-local-master-ref]]`.

## Graph-First Does Not Mean Graph-Only

- Use KG and CRG first for symbol, blast-radius, and decision-linked questions.
- If the graph is stale, incomplete, or missing the exact edge you need, fall back to targeted file reads — and use `grep -rln '<symbol>\b'` with word-boundary anchoring for unexported Go symbols (the code graph's `callers_of` underreports for cobra `RunE` lambdas and test files calling unexported symbols via type aliases).

## Do Not Re-Fanout an Active Bundle

- If a delegation bundle already exists for the chosen task, the orchestrator turn is brief: confirm the bundle is valid, update TASKS.yaml notes if needed, and hand off to delegation-lifecycle.
- Creating a second bundle for the same task produces a conflict the closeout commands cannot resolve cleanly.
- Pre-flight (`instructions/preflight.md`) surfaces active bundles before you reach the fanout decision — run it first every session.

## Closeout Path Depends on Direct vs Delegated

- **Delegated work:** `workflow delegation closeout --decision accept` advances status AND archives the contract/bundle/merge-back. Do NOT also call `workflow advance`.
- **Direct (orchestrator) work:** `workflow advance` is correct, and consider `workflow contract create --direct` upfront to keep the audit trail consistent.
- Calling both for delegated work double-advances and confuses subsequent eligibility checks.

## Verifier Boundary

- If the project registers verifier_profiles (e.g. `pr-ci`) in `.agentsrc.json`, the impl worker exits at merge-back and the verifier owns the PR-CI-watch loop. The orchestrator should NOT poll CI in parallel — wait for the verifier's terminal `READY` signal in `.agents/active/verification/<task-id>/<profile>.result.yaml`.
- The lens reviewer chain (architecture-standards / acceptance-invariants / adversarial) runs on the same task-local evidence; use it for non-trivial slices before closeout.

## Keep Hooks Lightweight

- Hooks may warn about stale delegations, missing fold-back, or pending verifier output, but they should not decide the next task or spawn workers.
- Choosing work belongs in the command and skill layer so the reasoning stays inspectable.

## Worktree Discipline

- Sessions touching multiple worktrees should always use `git -C /abs/path <cmd>` — never `cd` to a worktree. A single `cd` persists `pwd` across subsequent Bash calls and silently lands commits, branches, and pushes in the wrong worktree.
- For build/test commands that need cwd inside a worktree, use a subshell: `(cd "$WT" && go test ./...)` — parentheses prevent pwd leak.
