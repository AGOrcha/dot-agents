# Loop State

Last updated: 2026-04-11
Iteration: 4

## Current Position

Driving specs:
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — post-MVP workflow automation waves
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — KG subsystem with code-structure layer

Active wave summary (from `.agents/active/*.plan.md`):
- **active-artifact-cleanup**: Completed (2026-04-11) — all items done
- **crg-kg-integration**: Phases A-D complete. E/F/G (Postgres, Go MCP, skill integration) deferred
- **platform-dir-unification**: Blocked — Phases 4+5 need resource-intent-centralization RFC
- **refresh-skill-relink**: Blocked on resource-intent-centralization
- **skill-import-streamline**: Blocked on resource-intent-centralization
- **resource-intent-centralization**: Architectural RFC needed — skip

**All actionable implementation work in this loop is complete.** The only remaining active plans are either done, deferred, or blocked on one architectural decision (resource-intent-centralization). No further waves available without starting that RFC.

Note: Many older plans (kg-phase-1 through 5, wave-3 through 5) show "Completed" in their status header but still have unchecked `- [ ]` items. The status header is authoritative — unchecked boxes on completed plans are stale plan hygiene, not real work.

Analysis prep priority:
- The next loop-worthy improvement is not more summary prose; it is better evidence capture for the later analysis phase.
- Future iterations should leave behind structured trace metadata, scenario coverage, and explicit linkage between commands, commits, retries, and follow-on actions.

## Iteration Log

### Iteration 4 — 2026-04-11
- wave: active-artifact-cleanup
- item: Normalize stale plan state + record cleanup outcome in history + add lesson
- files_changed: 6
- lines_added: 65
- lines_removed: 3
- tests_added: 0
- tests_total_pass: true
- retries: 0
- commit: 7aad9c1
- scope_note: on-target
- summary: Added Status/Depends-on headers to 3 blocked plans; wrote impl-results.2.md; extended archive-completed-active-plans lesson; marked active-artifact-cleanup complete

Self-assessment:
- read_loop_state: yes
- one_item_only: no (grouped normalize + lesson + history as one logical item — the plan listed them as separate items but they're a single coherent action)
- committed_after_tests: yes
- ran_cli_command: yes
- stayed_under_10_files: yes
- no_destructive_commands: yes

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

**No actionable implementation waves remain.** All completed waves have been archived. The 5 remaining active plans are:
- 1 just-completed cleanup plan (will archive next pass)
- 3 blocked on `resource-intent-centralization` RFC
- 1 deferred (crg-kg-integration phases E/F/G)

To unblock: write the `resource-intent-centralization` RFC (architectural session required, not a loop iteration).

Analysis follow-on once implementation resumes:
- Prefer iterations that exercise uncovered commands or new workflow states over repeating the same happy-path checks.
- Capture at least some expected empty-state, warning, retry-recovered, and blocked traces; a later analysis pass will be weak if it only sees `[ok]` outcomes.
- Link each interesting trace to the triggering plan item, commit, and any retry/error entry so later review can reconstruct cause and effect quickly.

## Analysis Readiness

Questions the later analysis phase should be able to answer:
- Which commands are reliable only on happy paths vs across realistic repo states?
- Which workflow states produce the most retries, ambiguity, or operator intervention?
- Which outputs are correct but still create UX friction or weak operator guidance?
- Which agent loops stay tightly scoped vs drift or require cleanup work afterward?

Signals already captured:
- Per-iteration summary, scope, retries, commit, and basic self-assessment
- Exact CLI invocations with short output snapshots
- Command-level coverage tracking
- Freeform CLI observations

Signals still missing or too weak:
- Structured trace metadata: scenario, duration, exit status, expected vs unexpected, follow-on action
- Negative and near-miss traces such as retry-recovered, ok-but-empty, ok-with-warning, blocked, or pre-existing tool bug
- Scenario coverage across repo/workflow states, not just command names
- Linkage between iteration log entries, CLI traces, error log entries, and commits

Minimum capture rules for remaining work:
- Every iteration should declare one or more scenario tags such as `clean-repo`, `dirty-repo`, `no-canonical-plan`, `blocked-plan-set`, `kg-pre-postprocess`, or `deferred-wave`
- Every CLI trace should record whether the result was expected, unexpected, or informative-but-nonblocking
- Every retry or detour should leave an Error Log entry even if the final outcome is green
- Prefer at least one new command or one new scenario per iteration until the matrix below is meaningfully filled

## Scenario Coverage

| Scenario | Covered | Last Iteration | Notes |
|---|---|---|---|
| `clean-repo` | yes | 4 | `workflow status` reported `dirty: 0` |
| `dirty-repo` | yes | 3 | `workflow status` reported `dirty files: 1` |
| `no-canonical-plan` | yes | 3 | `workflow plan` returned expected empty state |
| `blocked-plan-set` | yes | 4 | active set reduced to completed, blocked, or deferred plans only |
| `kg-pre-postprocess` | yes | 2 | `kg flows` empty before `kg postprocess` |
| `kg-postprocess-complete` | no | - | need a run after `kg postprocess` |
| `expected-warning-or-empty` | yes | 2 | empty flow output was expected |
| `retry-recovered` | yes | 2 | naming collision fixed before final commit |
| `pre-existing-tool-bug` | no | - | none isolated yet |
| `write-command-path` | no | - | no safe write-path workflow/KG command exercised yet |

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

### Iteration 4 — 2026-04-11
```
$ go run ./cmd/dot-agents workflow status
Workflow Status — branch: feature/PA-cursor-kg-build-update-commands-1b58, sha: 7aad9c1, dirty: 0, active plans: 6, lessons: 10
```
Classification: [ok]

### Iteration 3 — 2026-04-11
```
$ go run ./cmd/dot-agents workflow plan
No canonical plans found.
```
Classification: [ok] — expected, no PLAN.yaml files have been created yet

```
$ go run ./cmd/dot-agents workflow status
Workflow Status — active plans: 6, dirty files: 1
```
Classification: [ok]

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
| `workflow status` | yes | 4 | ok |
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
