# Merge-back: Phase G embedded CRG skills + orchestrator Phase 5 closeout

Date: 2026-04-12

## Worker status

Prior **Task subagents** (orchestrator fan-out) were one-shot runs: they **finished**; nothing is still executing in the background.

## Integrated results

### Loop orchestrator — Phase 5 (`phase-5-kg-first-understanding`)

- **Code:** `commands/workflow.go`, `commands/workflow_test.go`, `docs/LOOP_ORCHESTRATION_SPEC.md` — forward code-structure `workflow graph query` intents to `dot-agents kg bridge query` subprocess.
- **Canonical:** `loop-orchestrator-layer` TASKS — `phase-5-kg-first-understanding` → **completed**. PLAN `current_focus_task` → **Phase 6** (fold-back).

### CRG+KG — Phase G (`phase-g-skill-integration`) — partial

- **Embedded canonical skills (tracked):** under `src/share/templates/standard/skills/global/`
  - `build-graph/` — `kg code-status`, `kg build` / `kg update` (CLI-first; MCP noted as optional parity).
  - `review-delta/` — `kg update`, `kg changes --brief`, `kg impact`, `kg bridge query`.
  - `review-pr/` — same family + note that semantic/docs helpers remain on `kg serve` MCP when needed.
- **Template updates:** `self-review` (optional `kg changes --brief`), `agent-start` (optional `workflow orient` / `kg health`).
- **Canonical:** `crg-kg-integration` TASKS — `phase-g-skill-integration` → **in_progress** (hooks under `~/.agents/hooks/global/` and full closeout still open).

## Follow-ups

- Document or add hook samples in-repo if Phase G requires tracked HOOK.yaml artifacts.
- Promote/sync `~/.agents/skills/global` from embedded templates where your install flow expects it (`skills promote` / refresh), without violating guardrails on destructive `refresh` in automation loops.
