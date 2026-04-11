# Skill ↔ Command Integration Map

Status: Active
Last updated: 2026-04-10
Related:
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` (Wave 5)
- `.agents/active/crg-kg-integration.plan.md`

## Purpose

This document maps the bidirectional integration between dot-agents skills (agent-facing prompts), dot-agents commands (CLI surface), and the knowledge graph (data layer). Skills consume commands and graph data. Commands generate, validate, and reference skills. The graph serves both.

## Integration Model

```
Skills (agent-facing)                Commands (CLI)               Graph (data)
┌─────────────────┐              ┌──────────────────┐         ┌──────────────┐
│  /review-delta  │──calls──────>│ dot-agents kg    │──reads──>│ nodes, edges │
│  /review-pr     │              │   changes        │         │ flows, risk  │
│  /build-graph   │──calls──────>│ dot-agents kg    │──writes─>│ communities  │
│  /agent-start   │              │   build/update   │         │ kg_notes     │
│  /self-review   │              │                  │         │ note_symbol  │
│  /agent-handoff │              │ dot-agents       │         │   _links     │
│  /split-commits │              │   workflow orient│──reads──>│              │
│  /gh-fix-ci     │              │   review approve │         └──────────────┘
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
- Step 1: `dot-agents kg status` — check if graph exists and is current
- Step 2: `dot-agents kg build` (first time) or `dot-agents kg update` (incremental)
- Step 3: `dot-agents kg status` — report results

**Graph tables touched**: `nodes`, `edges`, `metadata`, `communities`, `flows`, `flow_memberships`

**Why this matters**: This is the foundational skill. Every other graph-consuming skill assumes build-graph has run.

### review-delta

**Purpose**: Token-efficient delta review using impact analysis. Reviews only what changed since last commit.

**Current state**: Calls CRG MCP tools (`build_or_update_graph_tool`, `get_review_context_tool`, `get_impact_radius_tool`, `query_graph_tool`).

**Integrated state**:
- Step 1: `dot-agents kg update` — ensure graph reflects current state
- Step 2: `dot-agents kg changes` — get risk-scored change analysis (replaces `get_review_context_tool`)
- Step 3: `dot-agents kg impact <high-risk-symbol>` — blast radius for flagged symbols
- Step 4: `dot-agents kg bridge query --intent tests_for <changed-fn>` — check test coverage
- Step 5: `dot-agents kg bridge query --intent symbol_decisions <changed-fn>` — surface linked decisions (NEW: traceability)

**Graph tables touched**: `nodes`, `edges`, `risk_index`, `note_symbol_links`, `kg_notes`

**Why this matters**: review-delta is the most graph-intensive skill. It demonstrates the full value of having structural context vs grep. The addition of `symbol_decisions` is new — it gives reviewers the *why* behind code, not just the *what*.

### review-pr

**Purpose**: Full PR review with blast-radius analysis.

**Current state**: Same CRG MCP tools as review-delta, plus `semantic_search_nodes_tool`.

**Integrated state**: Same as review-delta, plus:
- Step 2a: `dot-agents kg changes --base main` — scope to PR diff
- Step 6: `dot-agents kg search <keyword>` — find related symbols (replaces `semantic_search_nodes_tool`)

**Additional graph value**: PR reviews span more code than delta reviews, making community analysis more relevant. `dot-agents kg` communities can identify when a PR crosses module boundaries (higher risk).

### self-review

**Purpose**: Pre-commit quality review.

**Current state**: Runs git diff, applies code quality/security/performance rules. No graph integration.

**Integrated state** (additions only — existing steps unchanged):
- After step 1 (gather diff): `dot-agents kg changes --brief` — get risk scores and test gaps for changed symbols
- After step 4 (test coverage): `dot-agents kg bridge query --intent tests_for <changed-fn>` — structural test coverage check
- New step: surface `note_symbol_links` for changed code — alert if a decision-documented function was modified

**Why this matters**: self-review is the most-run review skill. Adding lightweight graph awareness (just `--brief`) keeps it fast while catching impact that git diff alone misses.

### agent-start

**Purpose**: Session initialization — gather context before coding.

**Current state**: Prefers graph/MCP tooling over manual scans when available. References code-review-graph as optional.

**Integrated state**:
- Context gathering step: `dot-agents workflow orient` (already includes graph health via bridge when available)
- New: `dot-agents kg status` — show code graph stats (files, nodes, edges, staleness)
- New: `dot-agents kg bridge query --intent decision_lookup <current-task-topic>` — surface prior decisions relevant to the task

**Why this matters**: agent-start sets the context window for the entire session. Getting graph context early means fewer grep fallbacks later.

### agent-handoff

**Purpose**: Package session context for the next agent.

**Current state**: Gathers git state, plans, progress, creates handoff document.

**Integrated state** (additions only):
- New: include `dot-agents kg changes` summary in handoff — what symbols were modified this session
- New: include any `note_symbol_links` created or decisions referenced
- New: `dot-agents workflow checkpoint` already captures file modifications; extend to include symbol-level changes

**Why this matters**: Handoffs lose structural context. Recording which symbols changed (not just files) helps the next agent understand scope faster.

### split-reviewable-commits

**Purpose**: Rewrite branch into semantic commit sequence.

**Current state**: No graph integration. Splits are based on file-level heuristics.

**Integrated state**:
- New: `dot-agents kg` community analysis — suggest commit boundaries aligned with code communities
- When two files are in different communities, they are candidates for separate commits
- When files are in the same community, they should stay in the same commit
- Fallback: existing file-level heuristics when graph is unavailable

**Why this matters**: Community-aware splits produce more reviewable commits because each commit touches one logical module.

### gh-fix-ci

**Purpose**: Debug failing GitHub Actions checks.

**Current state**: No graph integration. Inspects check logs, identifies failures, plans fix.

**Integrated state**:
- New: `dot-agents kg changes --base <failing-commit>` — scope investigation to symbols changed since the last green build
- New: `dot-agents kg bridge query --intent impact_radius <changed-fn>` — understand what the change might have broken
- New: `dot-agents kg bridge query --intent tests_for <changed-fn>` — which tests should have caught this

**Why this matters**: CI failures are often caused by impact that crosses module boundaries. The graph shows the blast radius directly instead of requiring the agent to grep through test files.

### skill-architect

**Purpose**: Design and evaluate skills.

**Current state**: No graph integration.

**Integrated state** (future):
- `audit` mode could query the graph to verify skill instructions reference valid commands and tool names
- `eval` mode could measure skill effectiveness by tracking graph metrics before/after skill runs
- `optimize` mode could use graph community data to suggest which modules a skill should focus on

### create-subagent

**Purpose**: Create custom subagents for specialized tasks.

**Current state**: No graph integration.

**Integrated state** (future):
- New: agent descriptions could reference graph communities for scope — e.g., "this agent owns the `auth` community"
- New: `dot-agents kg` community list could suggest natural subagent boundaries

## Command → Skill: Reverse Direction

Commands can reference, inject, or trigger skills:

### dot-agents init

**What it does**: Initialize a project with dot-agents config.

**Skill integration**:
- Generates skill files from templates (currently from `src/share/templates/standard/skills/`)
- Should include graph-aware skills when CRG is available
- Should register `kg serve` MCP server config for detected platforms

### dot-agents add

**What it does**: Add agent configurations to a project.

**Skill integration**:
- Registers MCP server configs for platforms
- Should offer to register `dot-agents kg serve` alongside other MCP servers

### dot-agents refresh

**What it does**: Update configs from sources.

**Skill integration**:
- Pulls latest skill definitions from git sources
- Should update graph skill templates when source skills change

### dot-agents review approve

**What it does**: Approve and apply a workflow proposal.

**Skill integration**:
- Could call `self-review` as a pre-check before applying
- Could call `dot-agents kg changes` to validate proposal impact

### dot-agents workflow orient

**What it does**: Render session context.

**Skill integration**:
- Already renders plans, checkpoints, handoffs, proposals, git state
- Should include graph bridge health
- Should include `dot-agents kg status` (code graph stats)
- Feeds context to `agent-start` skill

### dot-agents doctor

**What it does**: Health check for config state.

**Skill integration**:
- Should include `dot-agents kg health` in diagnostics
- Should verify graph hooks are installed if graph DB exists

## Hooks As The Glue

Hooks bridge skills and commands by firing on agent events:

### Existing hooks (in ~/.agents/hooks/global/)

| Hook | Event | What it does | Graph connection |
|------|-------|-------------|-----------------|
| session-orient | session_start | Calls `dot-agents workflow orient` | Should include graph status |
| session-capture | stop | Calls `dot-agents workflow checkpoint` | Should capture symbol-level changes |
| auto-format | post_tool_use (Write/Edit) | Runs formatters | None |
| guard-commands | pre_tool_use (Bash) | Blocks dangerous commands | None |
| secret-scan | post_tool_use (Write/Edit) | Warns on credential writes | None |

### New hooks for graph integration

| Hook | Event | Command | Purpose |
|------|-------|---------|---------|
| graph-update | post_tool_use (Edit/Write/Bash) | `dot-agents kg update --skip-flows` | Keep graph current as code changes |
| graph-precommit | pre_commit | `dot-agents kg changes --brief` | Surface risk before committing |

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
8. **gh-fix-ci** — impact scoping is valuable but less frequent
9. **skill-architect + create-subagent** — future, depends on graph maturity
