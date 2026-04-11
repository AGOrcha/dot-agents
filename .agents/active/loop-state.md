# Loop State

Last updated: 2026-04-11
Iteration: 2

## Current Position

Driving specs:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — post-MVP workflow automation waves
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — KG subsystem with code-structure layer

Active wave summary (from `.agents/active/*.plan.md`):
- **crg-kg-integration**: Phases A + B + C + D all complete. Remaining phases (E Postgres, F Go MCP, G skill integration) are lower priority and not yet started.
- **kg-phase-6-shared-memory-research**: research steps complete, optional prototype remaining — next candidate
- **wave-7-cross-repo-sweep-drift**: status says Completed but has unchecked items — verify before picking
- **resource-intent-centralization**: plan written, architectural — skip

Note: Many older plans (kg-phase-1 through 5, wave-3 through 5) show "Completed" in their status header but still have unchecked `- [ ]` items. The status header is authoritative — unchecked boxes on completed plans are stale plan hygiene, not real work.

## Last Completed

**Iteration 2 — 2026-04-11**
Wave: `crg-kg-integration` Phase C
- Extended `internal/graphstore/crg.go` with `runPyQuery()` helper (executes Python via .venv interpreter, returns JSON).
- Added `GetImpactRadius()`, `ListFlows()`, `ListCommunities()`, `Postprocess()` methods to CRGBridge.
- Added `dot-agents kg impact`, `kg flows`, `kg communities`, `kg postprocess` subcommands.
- Fixed `CRGImpactResult` naming collision with existing `ImpactResult` in `store.go`.
- All tests pass. Commands verified live: `kg impact commands/kg.go` shows 112 changed nodes + 2 impacted files; `kg communities --min-size 5` shows 41 communities; `kg flows` shows 0 (flows need postprocess first).

**Iteration 1 — 2026-04-11**
Wave: `crg-kg-integration` Phase B
- Committed skill-architect transforms (delegation-lifecycle, plan-wave-picker, provider-consumer-pair) that were left uncommitted from prior session.
- Implemented `internal/graphstore/crg.go`: CRGBridge type that delegates build/update/status/change-detection to the Python code-review-graph CLI installed in `.venv`. Avoids a full Go tree-sitter port (~3000 lines) by using subprocess bridge.
- Added `dot-agents kg build`, `kg update`, `kg code-status`, `kg changes` subcommands to `commands/kg.go`.
- Wrote unit tests in `internal/graphstore/crg_test.go`.
- All tests pass (`go test ./...`). CLI commands verified live against this repo.

(Prior seed from manual Codex sessions `019d7a6d` and `019d7a9d`):
- Managed resource cleanup, AGENTS.md, skill transforms, AgentsRC schema, resource-intent-centralization plan.

## What's Next

- `kg-phase-6-shared-memory-research` — check unchecked items, implement any remaining prototype steps.
- `wave-7-cross-repo-sweep-drift` — verify whether unchecked items are truly stale or real work.

## Skip List

Plans to skip (blocked, requires architectural work, completed, or out of scope for loop):
- `resource-intent-centralization` — architectural redesign, needs focused RFC session
- `refresh-skill-relink` — blocked on resource-intent-centralization
- `skill-import-streamline` — blocked on resource-intent-centralization
- `platform-dir-unification` — blocked on resource-intent-centralization
- `agentsrc-local-schema` — completed
- `workflow-automation-product-spec-review` — review artifact, not implementation
- `wave-6-delegation-merge-back` — all items checked, completed
- `kg-phase-1-graph-core` — status: Completed
- `kg-phase-2-basic-ingest` — status: Completed
- `kg-phase-3-deterministic-query` — status: Completed
- `kg-phase-4-lint-maintenance` — status: Completed
- `kg-phase-5-bridge-readiness` — status: Completed
- `wave-3-structured-query-health-surface` — status: Completed
- `wave-4-shared-preferences` — status: Completed
- `wave-5-knowledge-graph-bridge` — status: Completed

## Blockers

- Shared `.agents/skills/*` projection bug: repo-local skill directories are not converted to managed symlinks after import. Documented in `skill-import-streamline.plan.md`. Blocked on resource-intent-centralization.
- `plan-wave-picker` SKILL.md at `~/.agents/skills/dot-agents/plan-wave-picker/SKILL.md` has invalid frontmatter (missing `---` delimiters). Codex warns on load.

## CLI Traces

### Iteration 2 — 2026-04-11
```
$ go run ./cmd/dot-agents kg impact commands/kg.go --limit 10
Impact Radius — 112 changed nodes, 10 impacted within 2 hops, 2 impacted files, 122 total
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg communities --min-size 5
Code Communities — Found 41 communities (top: commands-workflow size=139, commands-graph size=112)
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg flows
Execution Flows — Found 0 (no flows without postprocess run)
```
Classification: [ok] — flows require `kg postprocess` first, which is expected.

### Iteration 1 — 2026-04-11
```
$ go run ./cmd/dot-agents kg code-status
Code Graph Status — Nodes: 923, Edges: 6281, Files: 50, Languages: go ruby, Last updated: 2026-04-11T00:49:52
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg changes --brief
Change Impact — Analyzed 15 changed file(s): 5 changed functions, 0 flows, 5 test gaps, risk 0.30
```
Classification: [ok]

```
$ go run ./cmd/dot-agents kg changes
Change Impact — structured output with changed symbols, test gaps, review priorities
```
Classification: [ok]

## CLI Observations

- `kg changes` returns qualified names with absolute file paths (e.g. `/Users/.../kg.go::runKGWarm`) — CRG uses absolute paths internally. This is slightly noisy in output; could be made relative to repo root in a future iteration.
- `--brief` flag sends human-readable text from CRG, not JSON. The bridge handles both modes correctly now.
- `kg build` and `kg update` stream output directly; no structured return — good for interactive use.
- `kg impact` output includes File-level nodes in "Changed nodes" section — these add noise; filtered out in the render pass (only non-File nodes shown). Good UX decision.
- `kg flows` is empty without first running `kg postprocess` — worth noting in help text or adding a tip in the empty-state message (already done: "Run 'dot-agents kg postprocess' to detect flows").
- `runPyQuery` pattern works cleanly for calling CRG Python tool functions without needing a full MCP server — useful pattern for adding more CRG capabilities later.
