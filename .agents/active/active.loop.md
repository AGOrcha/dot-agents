# Automated Work Looper Prompt — Worker Overlay (dot-agents)

Copy the prompt below into a worker agent as: `/loop 1hr <prompt>` (or paste `<prompt>` directly)

## Project overlay metadata

- **Role:** Worker overlay for the dot-agents repo. Layer 2 of the three-layer model — repo-specific CLI inventory, implementation rules, and safety guardrails. Bundle is your primary task context.
- **Global `loop-worker` profile:** `~/.agents/profiles/loop-worker.md` (use `--delegate-profile loop-worker` with `workflow fanout`).
- **Valid fanout reference:** `--project-overlay .agents/active/active.loop.md` (path must stay inside the repo).
- **Skill routing:** In this project prefer `/orchestrator-session-start` over `/agent-start`. `agent-start` is for one-off tasks in repos without a dot-agents workflow setup.
- **TS port KG (phase-4):** `ports/typescript/src/commands/kg.ts` exposes read-only `runKgHealth` / `runKgQuery`; query is an intentional Go-only stub (no subprocess).
- **Agent repo mirrors:** On refresh/install, Claude `createAgentsLinks` syncs `~/.agents/agents/<project>/` into repo `.agents/agents/` and `.claude/agents/` (idempotent alongside shared-target projection).
- **Resource readback:** `status` summarizes declared hooks/MCP/etc. from `.agentsrc.json`; canonical hook bundles on disk are listed with `hooks list` / `hooks show` (Go CLI: no `hooks add` — author bundles under `~/.agents/hooks/…` then `refresh` / `install`).

---

## Prompt

```
## Startup (3 steps)
1. Read `.agents/active/loop-state.md` → `## Current Position` and the last 2 `## Iteration Log` entries (skip if missing)
2. `go run ./cmd/dot-agents workflow tasks <plan_id from bundle>` — confirm task status and dependencies
3. `git status --short` — if prior dirty state exists, commit it before starting

Do NOT run `workflow orient`, `workflow next`, or `workflow status` at startup — your bundle is the authoritative task scope.

## Implementation (ONE item per iteration)

### Reconciliation iterations
When the iteration is state catch-up only (advancing YAML tasks already implemented, reconciling markdown/YAML drift):
- iteration type: reconciliation
- feedback_goal: `state hygiene: confirm <X> is marked correctly`
- cli_produced_actionable_feedback: n/a
- Do not invent a stretch feedback goal for a reconciliation pass.

4. Implement the task within `write_scope` — keep scope tight, touch minimal files
5. Run focused tests: `go test ./<changed-packages>`
   - **Positive scenarios:** cover intended success paths — default inputs, happy-path behavior, expected outputs
   - **Negative scenarios:** cover failure paths — invalid input, missing prerequisites, expected errors
   - Prefer table-driven or parallel subtests for multiple success/failure combinations
6. Run regression: `go test ./...` — must stay green; do not commit with red tests
7. Run the CLI command nearest to what you changed (one primary evidence chain, 1–3 commands):
   - If unclear which command: `go run ./cmd/dot-agents workflow tasks <plan>` is always valid
   - Classify each result: `[ok]` | `[ok-warning]` | `[impl-bug]` | `[tool-bug]` | `[missing-feature]` | `[blocked]`
   - `[impl-bug]`: fix in this iteration before committing
   - `[tool-bug]`: fold-back immediately (see Iteration End), then continue
8. Commit once with implementation + loop-state/history updates: `<area>: <what changed>`
9. If tests fail, fix before moving on — do not leave red tests uncommitted

## CLI Commands (worker subset)

Read-only (always safe):
- `go run ./cmd/dot-agents workflow tasks <plan>` — show task list and statuses
- `go run ./cmd/dot-agents workflow verify log` — show recorded verification history
- `go run ./cmd/dot-agents workflow health` — workflow health snapshot
- `go run ./cmd/dot-agents status` — project health
- `go run ./cmd/dot-agents kg health` — knowledge graph health
- `go run ./cmd/dot-agents kg query <intent>` — query the KG

Write (not approval-gated — run as part of normal closeout):
- `go run ./cmd/dot-agents workflow verify record --status pass --summary "<test results>"`
- `go run ./cmd/dot-agents workflow checkpoint --message "<summary>" --verification-status pass`
- **Delegated:** `go run ./cmd/dot-agents workflow merge-back --task <id> --summary "..." --verification-status pass`
- **Direct:** `go run ./cmd/dot-agents workflow advance <plan> <task> completed`

Approval-gated (only when the task explicitly requires it):
- `workflow fanout`, `workflow sweep --apply`, `kg setup`, `kg sync`, `review approve/reject`

## Safety Guardrails — HARD RULES
- Do NOT run `dot-agents refresh`, `dot-agents install`, or `dot-agents install --generate` — these can overwrite managed files
- Do NOT modify `.agentsrc.json` manually — only through Go command paths
- Do NOT start architectural redesigns or multi-phase refactors — write an analysis to `.agents/active/<name>.plan.md`, add to skip-list, pick the next item
- Do NOT attempt to fix bugs in the dot-agents tool during implementation waves — fold-back the bug and move on
- Do NOT run commands that write outside the repo without a sandbox (`AGENTS_HOME` + `KG_HOME` pointing at tmp dirs; log `sandbox: ...` in trace)
- Maximum 10 files changed per iteration — if scope grows, split the work and commit what you have

## Iteration End
10. Run `/iteration-close`
    - Full closeout: verify record → checkpoint → merge-back (delegated) or advance (direct) → iteration log → self-assessment.
    - **Loop-state writes (two-author protocol):**
      1. Run `go run ./cmd/dot-agents workflow checkpoint --log-to-iter <N>` — creates `.agents/active/iteration-log/iter-N.yaml` with all CLI-deterministic fields. Prints the file path.
      2. Fill agent fields in `iter-N.yaml`: `item`, `scenario_tags`, `feedback_goal`, `tests_added`, `tests_total_pass`, `retries`, `scope_note`, `summary`, and the full `self_assessment` block.
      3. Update `## Loop Health` and `## Next Iteration Playbook` in `loop-state.md`.
      **Do NOT update `## Current Position`** — that is orchestrator scope.
      **Do NOT append to `## Iteration Log`** — that section no longer exists in loop-state.md.
    - **CLI broken fallback:** if the binary won't build, mark `persisted_via_workflow_commands: paused — <reason>`, fold-back the blocker immediately (`go run ./cmd/dot-agents workflow fold-back create --plan <id> --observation '[tool-bug]: <detail>' --propose`), and continue. Run deferred persist commands at the start of the next iteration.

## What NOT to spend time on
- Workspace hygiene beyond what the current task requires
- Skill transforms, imports, or promotions
- Schema or manifest changes unrelated to the current task
- Reading entire spec documents — use loop-state.md and your bundle as memory
```
