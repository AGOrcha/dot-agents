# Skill ↔ Command Integration Map

Status: Active
Last updated: 2026-07-22
Related:
- `.agents/workflow/specs/scoped-knowledge-graphs/design.md`
- `.agents/workflow/specs/graph-bridge-contract/design.md` (KG bridge)

This document maps how the starter skills consume the `da` command surface and the
knowledge graph. Every skill listed here ships in the starter scaffold
(`internal/scaffold/home/starter/skills/global/`) and lands in a home on `da init`.
Skills consume commands and graph data; commands generate, validate, and reference
skills; the graph serves both.

The code graph is reached through `da kg` (the code-review graph, CRG, embedded in
the KG subsystem). MCP tool parity is available by running `da kg serve` and pointing
a platform's MCP config at it, but the skills below drive the `da kg` CLI directly.

## Integration Model

```
Skills (agent-facing)            Commands (CLI)                 Graph (data)
┌────────────────────┐        ┌────────────────────┐        ┌────────────────┐
│ /build-graph       │──────> │ da kg build/update │──────> │ nodes, edges   │
│ /review-delta      │        │ da kg code-status  │        │ communities    │
│ /review-pr         │──────> │ da kg changes      │──────> │ flows          │
│ /self-review       │        │ da kg impact       │        │ risk_index     │
│ /agent-start       │        │ da kg query        │        │ kg_notes       │
│ /agent-handoff     │──────> │ da kg bridge query │──────> │ note_symbol    │
│                    │        │ da workflow orient │        │   _links       │
│ orchestration set  │──────> │ da workflow …      │──────> │ (workflow      │
│ (isp, delegation-  │        │  eligible/next/    │        │  state, not    │
│  lifecycle, …)     │        │  fanout/merge-back │        │  the code KG)  │
└────────────────────┘        └────────────────────┘        └────────────────┘
        │                              │
        └─────────── hooks ────────────┘
      (session-orient, session-capture, graph-orient,
       graph-update, graph-precommit, and the stage gates)
```

## Skills

Each entry is what the skill does today, the `da` commands it calls, and the graph
tables it touches.

### Code-review-graph skills

These drive `da kg` so reviews carry structural context instead of grep output.

#### build-graph

Builds or incrementally updates the code graph — the foundational skill every other
graph-consuming skill assumes has run.

- **Commands:** `da kg code-status` (is the graph present and current?), then
  `da kg build` (first time) or `da kg update` (incremental).
- **Graph:** `nodes`, `edges`, `metadata`, `communities`, `flows`, `flow_memberships`.

#### review-delta

Token-efficient review of only what changed since the last commit, with automatic
blast-radius detection.

- **Commands:** `da kg update` (freshen), `da kg changes` (risk-scored change
  analysis), `da kg impact <changed-file>` (blast radius), `da kg bridge query
  --intent tests_for <fn>` (test coverage), `da kg bridge query --intent
  symbol_decisions <fn>` (the decisions behind the code, not just the diff).
- **Graph:** `nodes`, `edges`, `risk_index`, `note_symbol_links`, `kg_notes`.

#### review-pr

Full PR / branch review with blast-radius analysis — the most graph-intensive skill,
since PRs span more code and cross module boundaries.

- **Commands:** the `review-delta` set, plus `da kg changes --base <ref>` (scope to
  the PR diff) and `da kg query --intent related_notes <keyword>` (related
  notes/symbols). Community analysis flags PRs that cross module boundaries.
- **Graph:** as `review-delta`, plus `communities`.

#### self-review

Pre-commit quality review; graph awareness is lightweight so it stays fast.

- **Commands:** `da kg changes --brief` (risk scores + test gaps for changed
  symbols), `da kg bridge query --intent tests_for <fn>` (structural test coverage).
- **Graph:** `risk_index`, `note_symbol_links` — alerts when a decision-documented
  function changed.

### Session skills

#### agent-start

Session initialization: gather current state and context before working.

- **Commands:** `da workflow orient` (active plan, checkpoints, handoffs, proposals,
  git and graph-bridge health), `da kg code-status` (code-graph stats), `da kg bridge
  query --intent decision_lookup <topic>` (prior decisions relevant to the task).

#### agent-handoff

Package session context for the next agent, and recover verified state on resume via
the session-handoff journal (modes: `recover`, `list`, `update`, `view`).

- **Commands:** `da workflow journal` (append-only mutation log + replay-and-recover),
  `da workflow checkpoint` (file + symbol-level changes), `da kg changes` (which
  symbols moved this session). Records any `note_symbol_links` created.

### Workflow-orchestration skills

These drive the `da workflow` surface — the CLI resolves, gates, and records the
topology; the skills compose the verifier/review steps on top.

#### orchestrator-session-start

The orchestrator turn: pre-flight, eligibility, task pick, KG readback, then the
fanout-or-direct decision. Does not implement the delegated slice.

- **Commands:** `da workflow eligible`, `da workflow orient`, `da workflow next`,
  `da workflow task update`, `da workflow fanout`.

#### isp

Interactive staged pipeline runtime over pre-gathered orchestrator output:
impl → verifier → review → parent gate.

- **Commands:** `da workflow fanout`, `da workflow bundle stages`,
  `da workflow delegation gate`.

#### delegation-lifecycle

Delegate a bounded write-scope task to a sub-agent, track its lifecycle, and merge
the result back into the canonical plan.

- **Commands:** `da workflow fanout`, `da workflow merge-back`,
  `da workflow delegation closeout`, `da workflow delegation gate`.

#### iteration-close

Persist a loop iteration's workflow state (fixes the `persisted_via_workflow_commands:
no` anti-pattern).

- **Commands:** `da workflow verify record`, `da workflow checkpoint`, then
  `da workflow advance` (direct work) or `da workflow merge-back` (delegated work).

#### loop-worker

Legacy / full-slice bounded implementation worker: reads a delegation bundle,
implements its `write_scope`, runs `iteration-close`, and returns a merge-back. (Typed
ISP stages use `isp`, not this.)

- **Commands:** `da workflow bundle stages`, plus the `iteration-close` chain.

#### plan-wave-picker

Choose the next wave/phase across active plans without re-reading each plan. (Run
only when `orchestrator-session-start` has not already run this session.)

- **Commands:** `da workflow plan`, `da workflow eligible`, `da workflow next`.

#### provider-consumer-pair

Sequence two waves that must ship together — one defines a contract, the other
consumes it — without circular blocking.

- **Commands:** `da workflow plan`, `da workflow tasks`, `da workflow fanout`.

### Ideation and authoring skills

#### kg-ideate (with kg-brief and staged-execution-handoff)

KG-grounded front end to the artifact pipeline: idea/proposal → spec → plan →
concurrent staged execution. Phase 1 (`kg-brief`) queries the knowledge graph, the
research corpus, and the lessons index to produce a shared briefing block; Phase 4
(`staged-execution-handoff`) makes the direct-vs-fanout call and hands the spec + plan
into `orchestrator-session-start` / `isp`.

- **Commands:** `da kg query` (briefing), then the `da workflow` plan/fanout surface
  via the orchestration skills.
- **Graph:** read-only `kg_notes`, `note_symbol_links` at briefing time.

#### spec-scaffold, plan-scaffold

The middle phases of `kg-ideate`: turn a briefing into a stabilized spec, then a spec
into a plan with bounded tasks, `depends_on` ordering, and impact-radius-grounded
write-scopes.

- **Commands:** `da workflow plan`, `da workflow tasks`.

#### ideation-cycle

Fork-resolution loop: turn a HARD/OPEN design fork into a ratified, fidelity-audited
decision via prototype → cross-harness audit → cross-brain review. Composes the
delegation surface for its prototype/audit workers.

### Config and release skills

#### pipeline-architect

Design and maintain full-loop execution pipelines and onboard execution profiles:
`stage_profiles`, verifier/review chains, model routing, cross-family review gates,
and per-`app_type` stage granularity.

- **Commands / config:** edits the canonical `.agentsrc.json` `execution_profile` and
  `stage_profiles`; reads them back with `da config relevance`. No code-graph
  integration.

#### release-cut

Cut a tagged release after docs are reconciled: pin-check the signing toolchain, push
the version bump / tag, monitor the release workflow, and classify known
sign/timestamp failures. Runs after the docs refresh. No code-graph integration.

#### skill-architect

Design, create, transform, audit, eval, improve, optimize, and package skills. The
`eval`/`improve`/`optimize` modes call an LLM; by default (`claude-cli`) they drive
whichever of the five platform CLIs is present (`claude`/`cursor`/`codex`/`opencode`/
`copilot`), auto-detected with no API key. Pin with `SKILL_ARCHITECT_PLATFORM` or swap
the provider with `SKILL_ARCHITECT_PROVIDER`. No code-graph integration today.

## Command → Skill: reverse direction

Commands generate, register, and render skills.

### da init / da add / da refresh

- `da init` scaffolds the starter skills (including the graph and workflow skills
  above) into `~/.agents/`, and can register the `da kg serve` MCP server for detected
  platforms.
- `da add` registers agent and MCP-server configs for a platform.
- `da refresh` re-fetches skill definitions from their declared sources and relinks
  them for the active platform.

### da workflow orient

Renders the session context — active plan, last checkpoint, verification state,
handoffs, proposals, git state, and graph-bridge health — that `agent-start` consumes.

### da review

Applies a queued rule/skill/config proposal (`da review approve`) and drives the
admin surface (`da review` list/label), backed by the SHA-256-chained audit log.

### da config explain / sync / lint / verify / relevance

The `da config` subtree introspects and resolves the layered config.
`da config explain <field-path>` prints the effective value plus full layer
provenance; `da config sync` re-fetches every declared layer and rewrites the config
section of `.agentsrc.lock` (the one mutating subcommand); `da config lint` validates
the manifest and each `extends` layer against the AgentsRC layer schema;
`da config verify` runs offline repo setup-contract checks; `da config relevance`
resolves a task's execution profile (units, topology, lenses) by `app_type` — the
readback `pipeline-architect` uses. These are operator-facing introspection commands,
not skill-invoked.

## Hooks

Hooks bridge skills and commands by firing on agent events. The starter ships these
under `~/.agents/hooks/global/`:

| Hook | Event | What it does |
|------|-------|--------------|
| session-orient | session_start | Calls `da workflow orient` (feeds `agent-start`) |
| session-capture | stop | Calls `da workflow checkpoint` (file + symbol changes) |
| graph-orient | session_start | `da kg health` — code-graph readiness at session start |
| graph-update | post_tool_use (Edit/Write/Bash) | `da kg update --skip-flows` — keeps the graph current as code changes |
| graph-precommit | pre_tool_use (git-commit Bash) | Runs `graph-precommit.sh` (`da kg` change brief) — surfaces risk before a commit (Claude has no native pre-commit event) |
| session-handoff-snapshot | pre_compact | Snapshots live workflow/KG state to the journal |
| session-handoff-recover | session_start | Replays the journal to recover verified state |
| iteration-close-gate / isp-gate / loop-worker-gate | stop | Enforce per-skill stop-gate context for the workflow stages |
| auto-format | post_tool_use (Write/Edit) | Runs formatters |
| guard-commands | pre_tool_use (Bash) | Blocks dangerous commands |
| secret-scan | post_tool_use (Write/Edit) | Warns on credential writes |

---

*Skills referenced in earlier drafts of this map but not part of the starter scaffold
or `.agentsrc.json` — `create-subagent`, `split-reviewable-commits`, `gh-fix-ci` — are
omitted here; this map covers only what ships.*
