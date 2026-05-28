# Gotchas: Orchestrator Session Start

## Do Not Turn The Orchestrator Into A Worker

- The orchestrator should choose and bound work, not become the place where large implementation happens.
- If the same agent starts selecting work, implementing it, and reconciling every observation, the focused loop discipline collapses.

## Canonical State Wins

- Prefer `workflow next`, `workflow tasks`, and delegation contracts over stale checkpoint prose.
- If markdown plan notes disagree with canonical task state, treat that as drift to reconcile, not as permission to ignore the canonical layer.
- After fanout, prefer the delegation **bundle** (`.agents/active/delegation-bundles/<delegation_id>.yaml`) over improvised prompts when briefing a worker.

## Graph-First Does Not Mean Graph-Only

- Use KG and CRG first for symbol, blast-radius, and decision-linked questions.
- If the graph is stale, incomplete, or missing the exact edge you need, fall back to targeted file reads instead of forcing a wrong graph interpretation.

## Do Not Re-Fanout an Active Bundle

- If a delegation bundle already exists for the chosen task, the orchestrator turn is brief: confirm the bundle is valid, update TASKS.yaml notes if needed, and hand off to delegation-lifecycle.
- Creating a second bundle for the same task produces a conflict the closeout commands cannot resolve cleanly.
- Pre-flight (`instructions/preflight.md`) surfaces active bundles before you reach the fanout decision — run it first every session.

## Keep Hooks Lightweight

- Hooks may warn about stale delegations or missing fold-back, but they should not decide the next task or spawn workers.
- Choosing work belongs in the command and skill layer so the reasoning stays inspectable.
