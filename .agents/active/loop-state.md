# Loop State

Last updated: 2026-04-11
Iteration: 2

## Current Position

Driving specs:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — post-MVP workflow automation waves
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — KG subsystem with code-structure layer

Active wave summary (from `.agents/active/*.plan.md`):
- **active-artifact-cleanup**: In progress — "Normalize stale plan state + add lesson" items remain
- **crg-kg-integration**: Phases A-D complete. E/F/G (Postgres, Go MCP, skill integration) deferred
- **platform-dir-unification**: Mixed status (2 completed, 1 in-progress, 2 not started) — blocked on resource-intent-centralization
- **refresh-skill-relink**: Blocked on resource-intent-centralization
- **skill-import-streamline**: Blocked on resource-intent-centralization
- **resource-intent-centralization**: Architectural RFC needed — skip

Note: Many older plans (kg-phase-1 through 5, wave-3 through 5) show "Completed" in their status header but still have unchecked `- [ ]` items. The status header is authoritative — unchecked boxes on completed plans are stale plan hygiene, not real work.

## Iteration Log

### Iteration 3 — 2026-04-11 18:00
- wave: active-artifact-cleanup
- item: Archive completed plan files out of `.agents/active/` when the task is done or already has matching history coverage
- files_changed: 12
- lines_added: 0
- lines_removed: 11
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: 87bce37
- scope_note: on-target
- summary: Moved 12 completed plan files from .agents/active/ to matching history/ folders; active set reduced from 18 to 6 plans

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: no (12 files — all plan renames, no code changes; scope is correct)
- no_destructive_commands: yes

### Iteration 2 — 2026-04-11
- wave: crg-kg-integration
- item: Phase C — CRG advanced query bridge (impact, flows, communities, postprocess)
- files_changed: 3
- lines_added: ~350
- lines_removed: ~10
- tests_added: 0 (verified via live CLI)
- tests_total_pass: true
- retries: 1 (CRGImpactResult naming collision with existing ImpactResult in store.go)
- commit: (not recorded — backfilled)
- scope_note: on-target
- summary: Added runPyQuery helper, 4 CRGBridge methods, 4 kg subcommands (impact, flows, communities, postprocess)

Self-assessment:
- read_loop_state: yes
- one_item_only: yes
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 1 — 2026-04-11
- wave: crg-kg-integration
- item: Phase B — CRG subprocess bridge and basic code-graph CLI commands
- files_changed: 6
- lines_added: ~500
- lines_removed: ~5
- tests_added: ~10 (internal/graphstore/crg_test.go)
- tests_total_pass: true
- retries: 0
- commit: (not recorded — backfilled)
- scope_note: expanded: also committed prior-session skill-architect transforms that were uncommitted
- summary: CRGBridge subprocess bridge to Python CRG, kg build/update/code-status/changes subcommands

Self-assessment:
- read_loop_state: yes
- one_item_only: no (also committed prior-session leftovers)
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

### Iteration 0 — 2026-04-11 (seed)
- wave: n/a (manual sessions)
- item: n/a
- files_changed: ~30
- lines_added: ~2000
- lines_removed: ~200
- tests_added: ~37 (internal/graphstore/sqlite_test.go)
- tests_total_pass: true
- retries: n/a
- commit: (multiple, not tracked)
- scope_note: n/a (seed from Codex sessions 019d7a6d and 019d7a9d)
- summary: GraphStore interface + SQLite backend, managed resource cleanup, AGENTS.md, skill transforms, AgentsRC schema, resource-intent-centralization plan

## What's Next

- `active-artifact-cleanup` — "Normalize stale plan state" and "Add lesson for keeping .agents/active/ focused" are the two remaining items
- After cleanup complete: all remaining active plans are either architectural (skip) or deferred (crg-kg phases E/F/G). May be at a natural stopping point for this loop.

## Skip List

Plans to skip (blocked, requires architectural work, completed, or out of scope for loop):
- `resource-intent-centralization` — architectural redesign, needs focused RFC session
- `refresh-skill-relink` — blocked on resource-intent-centralization
- `skill-import-streamline` — blocked on resource-intent-centralization
- `platform-dir-unification` — blocked on resource-intent-centralization
- `crg-kg-integration` phases E/F/G — deferred, lower priority than other work

(Completed plans archived to .agents/history/ on 2026-04-11:)
- kg-phase-1 through kg-phase-6, wave-3 through wave-7, workflow-automation-product-spec-review, agentsrc-local-schema, resource-sync-architecture-analysis

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

## Command Coverage

| Command | Tested | Last Iteration | Status |
|---|---|---|---|
| `status` | no | - | - |
| `doctor` | no | - | - |
| `workflow status` | no | - | - |
| `workflow orient` | no | - | - |
| `workflow checkpoint` | no | - | - |
| `workflow log` | no | - | - |
| `workflow plan` | no | - | - |
| `workflow tasks` | no | - | - |
| `workflow advance` | no | - | - |
| `workflow health` | no | - | - |
| `workflow verify` | no | - | - |
| `workflow prefs` | no | - | - |
| `workflow graph` | no | - | - |
| `workflow fanout` | no | - | - |
| `workflow merge-back` | no | - | - |
| `workflow drift` | no | - | - |
| `workflow sweep` | no | - | - |
| `kg setup` | no | - | - |
| `kg health` | no | - | - |
| `kg ingest` | no | - | - |
| `kg queue` | no | - | - |
| `kg query` | no | - | - |
| `kg lint` | no | - | - |
| `kg maintain` | no | - | - |
| `kg bridge` | no | - | - |
| `kg sync` | no | - | - |
| `kg warm` | no | - | - |
| `kg warm stats` | no | - | - |
| `kg link add` | no | - | - |
| `kg link list` | no | - | - |
| `kg link remove` | no | - | - |
| `kg build` | no | - | - |
| `kg update` | no | - | - |
| `kg code-status` | yes | 1 | ok |
| `kg changes` | yes | 1 | ok |
| `kg changes --brief` | yes | 1 | ok |
| `kg impact` | yes | 2 | ok |
| `kg communities` | yes | 2 | ok |
| `kg flows` | yes | 2 | ok |
| `kg postprocess` | no | - | - |

## Error Log

(No errors recorded yet)

<!--
Format:
### Iteration N
- type: test-failure | compile-error | cli-error
- detail: <what failed>
- resolution: <what fixed it>
- retries: N
-->

## CLI Observations

- `kg changes` returns qualified names with absolute file paths (e.g. `/Users/.../kg.go::runKGWarm`) — CRG uses absolute paths internally. This is slightly noisy in output; could be made relative to repo root in a future iteration.
- `--brief` flag sends human-readable text from CRG, not JSON. The bridge handles both modes correctly now.
- `kg build` and `kg update` stream output directly; no structured return — good for interactive use.
- `kg impact` output includes File-level nodes in "Changed nodes" section — these add noise; filtered out in the render pass (only non-File nodes shown). Good UX decision.
- `kg flows` is empty without first running `kg postprocess` — worth noting in help text or adding a tip in the empty-state message (already done: "Run 'dot-agents kg postprocess' to detect flows").
- `runPyQuery` pattern works cleanly for calling CRG Python tool functions without needing a full MCP server — useful pattern for adding more CRG capabilities later.
