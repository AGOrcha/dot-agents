# Skill ↔ Command Integration Map

Status: Active
Last updated: 2026-06-23
Related:
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` (Wave 5)
- legacy precursor: `.agents/active/crg-kg-integration.plan.md`

New workflow planning work should use `da workflow` with canonical bundles under `.agents/workflow/plans/` and supporting design docs under `.agents/workflow/specs/`; `.agents/active/` references here are historical lineage only.

## Purpose

This document maps the bidirectional integration between shared skills (agent-facing prompts), da commands (CLI surface), and the knowledge graph (data layer). Skills consume commands and graph data. Commands generate, validate, and reference skills. The graph serves both.

## Integration Model

```
Skills (agent-facing)                Commands (CLI)               Graph (data)
┌─────────────────┐              ┌──────────────────┐         ┌──────────────┐
│  /review-delta  │──calls──────>│ da kg changes    │──reads──>│ nodes, edges │
│  /review-pr     │              │ da kg impact     │         │ flows, risk  │
│  /build-graph   │──calls──────>│ da kg            │──writes─>│ communities  │
│  /agent-start   │              │   build/update   │         │ kg_notes     │
│  /self-review   │              │ da kg query      │         │ note_symbol  │
│  /agent-handoff │              │   --intent <n>   │         │   _links     │
│                 │              │ da workflow orient│──reads──>│              │
│                 │              │ da review approve │         └──────────────┘
└─────────────────┘              └──────────────────┘
        │                                │
        └──────── hooks ─────────────────┘
         (session-orient, graph-update,
          graph-precommit, session-capture)
```

## Skill → Command → Graph: Detailed Map

### build-graph

**Purpose**: Build or incrementally update the code knowledge graph.

**Current state**: Calls CRG MCP tools (`list_graph_stats_tool`, `build_or_update_graph_tool`) directly.

**Integrated state**:
- Step 1: `da kg code-status` — check if the code graph exists and is current
- Step 2: `da kg build` (first time) or `da kg update` (incremental)
- Step 3: `da kg code-status` — report results

**Graph tables touched**: `nodes`, `edges`, `metadata`, `communities`, `flows`, `flow_memberships`

**Why this matters**: This is the foundational skill. Every other graph-consuming skill assumes build-graph has run.

### review-delta

**Purpose**: Token-efficient delta review using impact analysis. Reviews only what changed since last commit.

**Current state**: Calls CRG MCP tools (`build_or_update_graph_tool`, `get_review_context_tool`, `get_impact_radius_tool`, `query_graph_tool`).

**Integrated state**:
- Step 1: `da kg update` — ensure graph reflects current state
- Step 2: `da kg changes` — get risk-scored change analysis (replaces `get_review_context_tool`)
- Step 3: `da kg impact <changed-file>` — blast radius for flagged files
- Step 4: `da kg bridge query --intent tests_for <changed-fn>` — check test coverage
- Step 5: `da kg bridge query --intent symbol_decisions <changed-fn>` — surface linked decisions (NEW: traceability)

**Graph tables touched**: `nodes`, `edges`, `risk_index`, `note_symbol_links`, `kg_notes`

**Why this matters**: review-delta is the most graph-intensive skill. It demonstrates the full value of having structural context vs grep. The addition of `symbol_decisions` is new — it gives reviewers the *why* behind code, not just the *what*.

### review-pr

**Purpose**: Full PR review with blast-radius analysis.

**Current state**: Same CRG MCP tools as review-delta, plus `semantic_search_nodes_tool`.

**Integrated state**: Same as review-delta, plus:
- Step 2a: `da kg changes --base main` — scope to PR diff
- Step 6: `da kg query --intent related_notes <keyword>` — find related notes/symbols (replaces `semantic_search_nodes_tool`)

**Additional graph value**: PR reviews span more code than delta reviews, making community analysis more relevant. `da kg` communities can identify when a PR crosses module boundaries (higher risk).

### self-review

**Purpose**: Pre-commit quality review.

**Current state**: Runs git diff, applies code quality/security/performance rules. No graph integration.

**Integrated state** (additions only — existing steps unchanged):
- After step 1 (gather diff): `da kg changes --brief` — get risk scores and test gaps for changed symbols
- After step 4 (test coverage): `da kg bridge query --intent tests_for <changed-fn>` — structural test coverage check
- New step: surface `note_symbol_links` for changed code — alert if a decision-documented function was modified

**Why this matters**: self-review is the most-run review skill. Adding lightweight graph awareness (just `--brief`) keeps it fast while catching impact that git diff alone misses.

### agent-start

**Purpose**: Session initialization — gather context before coding.

**Current state**: Prefers graph/MCP tooling over manual scans when available. References code-review-graph as optional.

**Integrated state**:
- Context gathering step: `da workflow orient` (already includes graph health via bridge when available)
- New: `da kg code-status` — show code graph stats (files, nodes, edges, staleness)
- New: `da kg bridge query --intent decision_lookup <current-task-topic>` — surface prior decisions relevant to the task

**Why this matters**: agent-start sets the context window for the entire session. Getting graph context early means fewer grep fallbacks later.

### agent-handoff

**Purpose**: Package session context for the next agent.

**Current state**: Gathers git state, plans, progress, creates handoff document.

**Integrated state** (additions only):
- New: include `da kg changes` summary in handoff — what symbols were modified this session
- New: include any `note_symbol_links` created or decisions referenced
- New: `da workflow checkpoint` already captures file modifications; extend to include symbol-level changes

**Why this matters**: Handoffs lose structural context. Recording which symbols changed (not just files) helps the next agent understand scope faster.

### split-reviewable-commits (not yet shipped)

**Purpose**: Rewrite branch into semantic commit sequence.

**Status**: Not yet shipped — not registered in `.agentsrc.json` and not part of
the starter scaffold. The notes below are forward-looking.

**Potential graph integration** (future):
- `da kg` community analysis — suggest commit boundaries aligned with code communities
- When two files are in different communities, they are candidates for separate commits
- When files are in the same community, they should stay in the same commit
- Fallback: existing file-level heuristics when graph is unavailable

**Why this matters**: Community-aware splits produce more reviewable commits because each commit touches one logical module.

### gh-fix-ci (not yet shipped)

**Purpose**: Debug failing GitHub Actions checks.

**Status**: Not yet shipped — not registered in `.agentsrc.json` and not part of
the starter scaffold. The notes below are forward-looking.

**Potential graph integration** (future):
- `da kg changes --base <failing-commit>` — scope investigation to symbols changed since the last green build
- New: `da kg bridge query --intent impact_radius <changed-fn>` — understand what the change might have broken
- New: `da kg bridge query --intent tests_for <changed-fn>` — which tests should have caught this

**Why this matters**: CI failures are often caused by impact that crosses module boundaries. The graph shows the blast radius directly instead of requiring the agent to grep through test files.

### skill-architect

**Purpose**: Design, create, transform, audit, eval, improve, optimize, and package skills.

**Current state**: Shipped. `skill-architect` is a starter skill
(`internal/scaffold/home/starter/skills/global/skill-architect/`, registered in
`.agentsrc.json`), so it lands in every initialized home. It runs seven modes —
`new`, `transform`, `audit`, `eval`, `improve`, `optimize`, `package` — and is
provider-pluggable: the `eval`/`improve`/`optimize` modes call an LLM under the
hood. By default (`claude-cli`) it drives the local CLI of whichever of the five
dot-agents platforms is present, auto-detecting `claude` / `cursor` / `codex` /
`opencode` / `copilot` with zero config and no API key, reusing the host
session's auth. Pin a platform with `SKILL_ARCHITECT_PLATFORM`, or swap in a
different provider entirely (Anthropic API, an OpenAI-compatible endpoint, or any
CLI) via `SKILL_ARCHITECT_PROVIDER`.

**Graph integration**: None today. Potential future additions: `audit` could
query the graph to verify skill instructions reference valid commands and tool
names; `eval` could track graph metrics before/after skill runs; `optimize`
could use community data to suggest which modules a skill should focus on.

### Workflow-orchestration skills (shipped)

The following seven skills are shipped — each is a starter skill under
`internal/scaffold/home/starter/skills/global/` and registered in `.agentsrc.json`,
so they land in every initialized home. They drive the `da workflow` /
`da kg` command surface rather than the code-review-graph review surface.

- **delegation-lifecycle** — delegate a bounded write-scope task to a sub-agent,
  track its lifecycle, and merge the result back. Commands: `da workflow fanout`,
  `da workflow merge-back`, `da workflow delegation closeout`, `da workflow delegation gate`.
- **iteration-close** — persist a loop iteration's workflow state. Commands:
  `da workflow verify record`, `da workflow checkpoint`, then `da workflow advance`
  (direct work) or `da workflow merge-back` (delegated work).
- **loop-worker** — bounded implementation worker that reads a delegation bundle,
  implements its write scope, and runs `iteration-close`. Commands:
  `da workflow bundle stages`, plus the `iteration-close` chain.
- **orchestrator-session-start** — orchestrator turn: pre-flight, eligibility,
  task pick, fanout-or-direct decision. Commands: `da workflow eligible`,
  `da workflow orient`, `da workflow next`, `da workflow task update`, `da workflow fanout`.
- **plan-wave-picker** — choose the next wave/phase across active plans without
  re-reading every plan. Commands: `da workflow plan`, `da workflow eligible`,
  `da workflow next`.
- **provider-consumer-pair** — sequence two waves that must ship together (one
  defines a contract, the other consumes it) without circular blocking. Commands:
  `da workflow plan`, `da workflow tasks`, `da workflow fanout`.
- **isp** — interactive staged pipeline orchestrator (impl → verifier → review →
  parent gate) over pre-gathered orchestrator output. Commands:
  `da workflow fanout`, `da workflow bundle stages`, `da workflow delegation gate`.

**Graph integration**: these consume `da workflow` state and the `da kg` readback
at orient time; deeper graph integration (scope derivation, impact-aware fanout)
is tracked separately.

### create-subagent (not yet shipped)

**Purpose**: Create custom subagents for specialized tasks.

**Status**: Not yet shipped — not registered in `.agentsrc.json` and not part of
the starter scaffold. The notes below are forward-looking.

**Potential graph integration** (future):
- Agent descriptions could reference graph communities for scope — e.g., "this agent owns the `auth` community"
- `da kg communities` could suggest natural subagent boundaries

## Command → Skill: Reverse Direction

Commands can reference, inject, or trigger skills:

### da init

**What it does**: Initialize a project with dot-agents config.

**Skill integration**:
- Generates skill files from the embedded scaffold templates under `internal/scaffold/templates/`
- Should include graph-aware skills when CRG is available
- Should register `kg serve` MCP server config for detected platforms

### da add

**What it does**: Add agent configurations to a project.

**Skill integration**:
- Registers MCP server configs for platforms
- Should offer to register `da kg serve` alongside other MCP servers

### da refresh

**What it does**: Update configs from sources.

**Skill integration**:
- Pulls latest skill definitions from git sources
- Should update graph skill templates when source skills change

### da review approve

**What it does**: Approve and apply a workflow proposal.

**Skill integration**:
- Could call `self-review` as a pre-check before applying
- Could call `da kg changes` to validate proposal impact

### da workflow orient

**What it does**: Render session context.

**Skill integration**:
- Already renders plans, checkpoints, handoffs, proposals, git state
- Should include graph bridge health
- Should include `da kg code-status` (code graph stats)
- Feeds context to `agent-start` skill

### da doctor

**What it does**: Health check for config state.

**Skill integration**:
- Should include `da kg health` in diagnostics
- Should verify graph hooks are installed if graph DB exists

### da config explain / sync / lint / verify / relevance

**What they do**: The `da config` subtree introspects and resolves the layered
config. `da config explain <field-path>` (`commands/config/explain.go`) prints
the effective value of a single field plus full layer provenance.
`da config sync` (`commands/config/sync.go`) re-fetches every declared layer
regardless of TTL, re-resolves, and rewrites the config section of
`.agentsrc.lock` (the one mutating subcommand). `da config lint`
(`commands/config/lint.go`) validates the repo-local manifest and each
`extends` layer against the AgentsRC layer schema. `da config verify`
(`commands/config/verify.go`) runs offline repo setup-contract checks without
re-fetching layers. `da config relevance` (`commands/config/relevance.go`)
resolves a task's execution profile (units, topology, lenses) by `app_type`.

**Skill integration**: Not skill-invoked today. These are operator-facing
introspection commands; a future setup or doctor skill could call them to
confirm the resolved config and contract state before acting.

## Hooks As The Glue

Hooks bridge skills and commands by firing on agent events:

### Existing hooks (in ~/.agents/hooks/global/)

| Hook | Event | What it does | Graph connection |
|------|-------|-------------|-----------------|
| session-orient | session_start | Calls `da workflow orient` | Should include graph status |
| session-capture | stop | Calls `da workflow checkpoint` | Should capture symbol-level changes |
| auto-format | post_tool_use (Write/Edit) | Runs formatters | None |
| guard-commands | pre_tool_use (Bash) | Blocks dangerous commands | None |
| secret-scan | post_tool_use (Write/Edit) | Warns on credential writes | None |

### New hooks for graph integration

| Hook | Event | Command | Purpose |
|------|-------|---------|---------|
| graph-update | post_tool_use (Edit/Write/Bash) | `da kg update --skip-flows` | Keep graph current as code changes |
| graph-precommit | pre_commit | `da kg changes --brief` | Surface risk before committing |

These replace the hooks that `code-review-graph install` currently writes to `.claude/settings.json`.

## Measurement: How To Know Integration Works

### Graph adoption metrics

Track per session (via session-capture hook):
- MCP tool calls to graph vs grep/glob fallbacks
- Bridge query count and hit rate
- Whether agent used `kg search` or fell back to `Grep`

### Skill effectiveness via graph

- **review-delta/review-pr**: Did risk scores decrease after review? Were test gaps closed?
- **self-review**: Did it surface impact that git diff alone would miss?
- **agent-start**: Did the agent ask fewer "what does X do" questions after graph context loading?
- **split-reviewable-commits**: Were commit boundaries aligned with communities?

### Graph quality over time

- `risk_index.risk_score` trend per community
- `note_symbol_links` coverage: what percentage of active decisions link to code?
- Stale note count: are notes being maintained or rotting?

## Implementation Priority

1. **review-delta + review-pr** — highest impact, most graph-intensive, proves the value
2. **build-graph** — foundational, everything depends on it
3. **self-review** — most frequently run, lightweight graph addition
4. **agent-start** — sets session context, compounds over time
5. **graph hooks** — automates graph freshness
6. **agent-handoff** — preserves structural context across sessions
7. **split-reviewable-commits** — community-aware splits are a clear upgrade
8. **gh-fix-ci** (not yet shipped) — impact scoping is valuable but less frequent
9. **skill-architect** (shipped) **+ create-subagent** (not yet shipped) — graph integration is future, depends on graph maturity
