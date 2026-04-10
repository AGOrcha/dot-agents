# Workflow Automation Product Spec

Status: Proposed
Last updated: 2026-04-09

This document turns the workflow automation research and the preliminary plan in `/Users/nikashp/.claude/plans/happy-seeking-iverson.md` into an implementation-ready MVP contract for `dot-agents`.

This spec is normative for product decisions. Implementation plans may change sequencing, but they should not re-decide behavior defined here.

## Purpose

`dot-agents` already distributes agent configuration well. The next product layer is workflow automation for agent sessions: orient at session start, persist useful state during and at the end of work, and queue higher-risk changes for human review.

The intended operating model is:

- Agents operate the workflow system.
- Humans steer and review higher-risk changes.
- Canonical workflow state is explicit, portable, and inspectable.

The goal of this spec is to remove product ambiguity so a weaker implementation agent can build the MVP without inventing behavior.

## Problem Statement

Current workflow state is real but fragmented:

- Plans, handoffs, and lessons are spread across repo-local conventions and platform-local storage.
- Session re-entry repeatedly requires reconstructing active work from scratch.
- Verification state is rediscovered instead of persisted.
- Humans still end up acting as workflow operators instead of reviewers.

The product gap is not another config family. The product gap is a shared workflow layer that supports the existing agent loop:

`orient -> work -> persist -> propose`

## Product Principles

- Agent-first: the primary operator is the session agent, not the human.
- Human-steered: changes that affect shared instructions or behavior require explicit review.
- Canonical artifacts: workflow state must have stable paths and schemas.
- Platform-native delivery: hooks render into each platform's native config format from canonical hook bundles.
- Minimal user burden: the MVP should not require humans to learn a new operational workflow.
- Explicit unknowns: when data is unavailable, store `unknown` or an empty collection instead of inferring.

## MVP Goals

- Reduce session re-entry cost by surfacing active plans, checkpoint state, lessons, handoffs, and git state automatically.
- Persist enough structured state that the next agent can continue work without guessing.
- Introduce a proposal queue for rule, skill, hook, and workflow-config changes that need human approval.
- Keep the human-facing interaction minimal: review, approve, reject, and inspect.
- Reuse the existing canonical hook-bundle design instead of inventing a second runtime mechanism.

## Non-Goals

- No new MCP server in the MVP.
- No multi-agent locking or conflict-resolution protocol in the MVP.
- No new canonical workflow family beyond the artifacts explicitly listed below.
- No attempt to replace git history, PR review, or platform-native session history.
- No bash parity requirement for the first implementation pass.
- No automatic rule or skill mutation outside the proposal queue.

## Resolved MVP Decisions

These decisions are fixed for the MVP and should not be reopened during implementation:

- Storage split:
  - Repo-local workflow artifacts stay in the repo under `.agents/`.
  - User-local operational state lives in `~/.agents/`.
- Hooks:
  - Canonical hook bundles use `HOOK.yaml` plus bundle-local scripts/assets.
  - Platforms consume rendered native hook outputs from those bundles.
  - Do not build a generic unified hook runner for the MVP.
- Human interface:
  - `dot-agents review` is the primary human-facing new command.
  - `dot-agents workflow ...` commands are escape hatches and debugging tools, not the main human workflow.
- Proposal queue:
  - One YAML file per proposal.
  - Pending proposals live in `~/.agents/proposals/`.
  - Reviewed proposals move to `~/.agents/proposals/archived/`.
- Concurrency:
  - MVP assumes one active agent writer per repo at a time.
  - If multiple agents write concurrently, last write wins for `checkpoint.yaml`; append-only logs remain append-only.
- Verification state:
  - Verification is stored as structured summary data in checkpoints.
  - Detailed raw test output stays in existing tool logs and terminal history.
- Scope:
  - The MVP ships orient, persist, safety/quality hooks, workflow status/orient/checkpoint/log commands, and the proposal-review queue.
  - Preferences, tool-health automation, delegation orchestration, and multi-agent merge-back are follow-on work.

## Roles

| Role | Responsibility |
|------|----------------|
| Session agent | Performs work, reads orient output, writes checkpoints, authors proposals |
| Human reviewer | Reviews and approves or rejects higher-risk proposals |
| `dot-agents` CLI and hook layer | Produces canonical context, persists structured state, renders platform-native hooks, applies approved proposals |

## Canonical Artifact Layout

### Repo-local artifacts

These are committed or project-scoped artifacts owned by the repo:

| Path | Purpose |
|------|---------|
| `.agents/active/*.plan.md` | Active work plans |
| `.agents/active/handoffs/*.md` | Pending handoff docs |
| `.agents/lessons/index.md` or `.agents/lessons.md` | Human-readable lesson index |

### User-local artifacts

These are local operational artifacts owned by `~/.agents`:

| Path | Purpose |
|------|---------|
| `~/.agents/context/<project>/checkpoint.yaml` | Latest structured checkpoint for a project |
| `~/.agents/context/<project>/session-log.md` | Append-only session history derived from checkpoints |
| `~/.agents/proposals/<id>.yaml` | Pending proposals awaiting human review |
| `~/.agents/proposals/archived/<id>.yaml` | Approved or rejected proposals after review |
| `~/.agents/hooks/global/<hook-name>/...` | Canonical global hook bundles |

### Path rules

- `<project>` is derived from `.agentsrc.json.project` when present.
- If `.agentsrc.json.project` is absent, `<project>` is the repo directory basename.
- Phase 1 hook scripts must support `CLAUDE_PROJECT_DIR` and otherwise fall back to the current working directory.
- Phase 3 CLI commands use the current working directory unless a future command explicitly adds a path flag.

## Workflow Contract

The product contract is organized around three primitives:

1. Orient
2. Persist
3. Propose

Supporting safety hooks are part of the MVP, but they exist to reinforce the same workflow loop rather than create a separate product.

## Orient

### Trigger points

- Required: session-start hook
- Required: `dot-agents workflow orient`
- Required: `dot-agents workflow status` uses the same underlying data sources

### Canonical orient data model

The canonical orient model contains:

- `project`
  - `name`
  - `path`
- `git`
  - `branch`
  - `sha`
  - `dirty_file_count`
  - `recent_commits` as up to 5 one-line commit summaries
- `active_plans`
  - list of `path`, `title`, and up to 3 pending checklist items or leading summary lines
- `checkpoint`
  - latest checkpoint fields from `checkpoint.yaml`, or `null` if absent
- `handoffs`
  - list of `path` and title
- `lessons`
  - up to 10 recent entries from the first existing path in:
    1. `.agents/lessons/index.md`
    2. `.agents/lessons.md`
- `proposals`
  - `pending_count`
- `next_action`
  - preferred source order:
    1. `checkpoint.next_action` when non-empty
    2. first pending checklist item from the first active plan
    3. `"Review active plan"`
- `warnings`
  - non-fatal issues such as missing lessons index, missing git repo, or unreadable checkpoint

### Orient output formats

- Hook output:
  - human-readable Markdown to stdout
  - must include sections for Project, Active Plans, Last Checkpoint, Pending Handoffs, Recent Lessons, Pending Proposals, and Next Action
- CLI output:
  - `dot-agents workflow orient` prints the same Markdown view by default
  - `dot-agents workflow orient --json` emits the same canonical data model in JSON

### Orient behavior rules

- Missing optional artifacts are reported as empty sections, not errors.
- Orient must never block session start.
- The MVP does not persist a separate `orient.yaml` artifact.

## Persist

### Trigger points

- Required: session-end hook
- Required: `dot-agents workflow checkpoint`
- Optional later: additional natural-breakpoint triggers such as post-test hooks

### Checkpoint schema

`~/.agents/context/<project>/checkpoint.yaml` uses this schema:

```yaml
schema_version: 1
timestamp: "2026-04-09T23:30:00Z"
project:
  name: "dot-agents"
  path: "/Users/nikashp/Documents/dot-agents"
git:
  branch: "feature/workflow-automation"
  sha: "abc1234"
  dirty_file_count: 2
files:
  modified:
    - "internal/platform/hooks.go"
    - "internal/platform/hooks_test.go"
message: "phase 3 complete"
verification:
  status: "pass"
  summary: "go test ./... passed"
next_action: "Implement proposal review command"
blockers: []
```

### Checkpoint field rules

- `schema_version` is required and set to `1`.
- `timestamp` is required and uses UTC RFC3339 format.
- `project.name` and `project.path` are required.
- `git.branch`, `git.sha`, and `git.dirty_file_count` are required when git data is available; otherwise use `unknown` and `0`.
- `files.modified` is required and may be empty.
- `message` is optional and defaults to an empty string.
- `verification.status` is required and must be one of `pass`, `fail`, `partial`, or `unknown`.
- `verification.summary` is required and may be an empty string.
- `next_action` is required and should be concrete.
- `blockers` is required and may be empty.

### Session log format

Each checkpoint append writes one Markdown entry to `~/.agents/context/<project>/session-log.md`:

```md
## 2026-04-09T23:30:00Z
branch: feature/workflow-automation
sha: abc1234
files: 2
verification: pass
message: phase 3 complete
next_action: Implement proposal review command
```

### Persist behavior rules

- Session capture must never block session end.
- If checkpoint writing fails, the hook prints a warning and exits successfully.
- `dot-agents workflow checkpoint` may expose flags for `--message` and `--verification`, but the stored schema must remain exactly as defined above.

## Propose

### Scope

The proposal queue is the only MVP path for agent-authored changes to shared rules, skills, hooks, or workflow config that require human review.

### Proposal schema

Pending proposals are YAML files at `~/.agents/proposals/<id>.yaml`:

```yaml
schema_version: 1
id: "2026-04-09-add-go-format-rule"
status: "pending"
type: "rule"
action: "add"
target: "rules/global/go-conventions.mdc"
rationale: "Agent observed repeated gofmt corrections across multiple sessions."
content: |
  # Go Conventions
  - Always use gofmt
  - Prefer table-driven tests
created_at: "2026-04-09T23:00:00Z"
created_by: "claude-session-abc123"
reviewed_at: ""
review_reason: ""
```

### Proposal field rules

- `schema_version` is required and set to `1`.
- `id` is required, unique, and stable.
- `status` is required and must be `pending`, `approved`, or `rejected`.
- `type` is required and must be `rule`, `skill`, `hook`, or `setting`.
- `action` is required and must be `add`, `modify`, or `remove`.
- `target` is required and must be a path relative to `~/.agents/`.
- Absolute paths and parent-directory traversal in `target` are invalid.
- `rationale` is required.
- `content` is required for `add` and `modify`, and must be empty for `remove`.
- `created_at` and `created_by` are required.
- `reviewed_at` and `review_reason` remain empty until review.

### Review command contract

Required commands:

- `dot-agents review`
  - lists pending proposals only
- `dot-agents review show <id>`
  - shows full proposal content and metadata
- `dot-agents review approve <id>`
  - validates the target
  - applies the change under `~/.agents/`
  - runs `dot-agents refresh`
  - updates `status` to `approved`
  - sets `reviewed_at`
  - moves the proposal to `~/.agents/proposals/archived/<id>.yaml`
- `dot-agents review reject <id> [--reason "..."]`
  - updates `status` to `rejected`
  - sets `reviewed_at`
  - sets `review_reason` when provided
  - moves the proposal to `~/.agents/proposals/archived/<id>.yaml`

### Review behavior rules

- Approve is transactional: if apply or refresh fails, the proposal remains pending.
- Review commands are the only MVP path that may apply proposal content to `~/.agents/`.
- Proposal creation itself is file-based; no separate `dot-agents propose` CLI is required in the MVP.

## Safety And Quality Hook Bundles

These canonical hook bundles are required in the MVP:

| Hook | Event | Behavior | Blocking policy |
|------|-------|----------|-----------------|
| `session-orient` | `session_start` | Emits orient Markdown | never blocks |
| `session-capture` | `stop` | Writes checkpoint and appends session log | never blocks |
| `guard-commands` | `pre_tool_use` on shell commands | Blocks exact destructive patterns | may block |
| `secret-scan` | `post_tool_use` on file edits | Warns on likely secrets | never blocks |
| `auto-format` | `post_tool_use` on file edits | Runs best-effort formatter by extension | never blocks |

### Guard-commands exact initial blocklist

The initial MVP blocklist is:

- `rm -rf /`
- `rm -rf ~`
- `git push --force origin main`
- `git push --force origin master`
- `DROP DATABASE`
- `DROP TABLE`
- `truncate`
- `:(){ :|:& };:`

### Secret-scan exact initial detectors

The initial MVP detector set includes:

- Anthropic keys matching `sk-ant-api`
- AWS access keys matching `AKIA[0-9A-Z]{16}`
- GitHub tokens matching `ghp_`, `gho_`, or `ghs_`
- Stripe keys matching `sk_live_` or `sk_test_`
- OpenAI keys matching `sk-[a-zA-Z0-9]{20,}`

The initial placeholder allowlist includes:

- `YOUR_KEY`
- `REPLACE_ME`
- `example`
- `xxxx`
- `test_`

### Auto-format routing

The initial formatter routing is:

- `.go` -> `gofmt -w`
- `.py` -> `ruff format --quiet`, fallback `black --quiet`
- `.ts`, `.tsx`, `.js`, `.jsx`, `.css`, `.scss`, `.json`, `.yaml`, `.yml` -> `npx prettier --write`
- `.rs` -> `rustfmt`

Formatter availability is best effort. Missing formatters must not fail the hook.

## Approval Gradient

The MVP approval gradient is:

- Auto-apply:
  - checkpoints
  - session logs
  - plan progress updates
  - repo-local handoffs
  - repo-local lessons
- Propose-and-review:
  - new or changed rules
  - new or changed skills
  - new or changed hook bundles
  - workflow config changes under `~/.agents/`
- Escalate manually:
  - cross-repo drift decisions
  - conflicting team conventions
  - multi-agent coordination conflicts
  - deletion or refactor strategies that are larger than a single proposal

## CLI Surface

The MVP command surface is:

- Existing:
  - `dot-agents hooks list`
  - `dot-agents refresh`
  - `dot-agents status`
  - `dot-agents doctor`
- New:
  - `dot-agents workflow status`
  - `dot-agents workflow orient [--json]`
  - `dot-agents workflow checkpoint [--message ...] [--verification ...]`
  - `dot-agents workflow log [--all]`
  - `dot-agents review`
  - `dot-agents review show <id>`
  - `dot-agents review approve <id>`
  - `dot-agents review reject <id> [--reason ...]`

These commands are support and inspection commands. They do not change the core product principle that hooks and file conventions drive the normal agent workflow.

## MVP Phase Mapping

### Phase 1: Canonical hook bundles

Must deliver:

- all 5 required hook bundles
- correct hook metadata in `HOOK.yaml`
- bundle-local scripts/assets
- orient and persist behavior matching this spec
- non-blocking failure behavior except for `guard-commands`

### Phase 2: Init and detection support

Must deliver:

- starter hook bundles embedded or scaffolded for new users
- `~/.agents/context/` creation during init
- agentsrc generation that detects canonical hook bundles
- `.gitignore` coverage for local context state

### Phase 3: Workflow escape-hatch commands

Must deliver:

- `workflow status`
- `workflow orient`
- `workflow checkpoint`
- `workflow log`
- parity with the same underlying data model used by Phase 1 hooks

### Phase 4: Proposal queue and review UX

Must deliver:

- proposal schema support
- `review` command group
- safe apply semantics
- proposal archiving after review

## Explicitly Deferred Work

These items are acknowledged but not required for the MVP:

- canonical plan and task artifacts
- runtime MCP query surface for workflow state
- automatic tool-health and approval-state diagnostics beyond what existing commands already expose
- persisted repo preferences
- knowledge-graph bridge and integration readiness
- delegation manifests, fan-out orchestration, or merge-back artifacts
- structured intent-marker transport between multiple active agents
- cross-repo workflow sweep commands
- bash parity

## Implementation Guardrails

The follow-on implementation agent should treat these as hard constraints:

- Do not introduce new workflow resource families beyond the artifacts listed here.
- Do not add an MCP server in this pass.
- Do not invent additional human-facing commands.
- Do not move plans, lessons, or handoffs out of repo-local `.agents/`.
- Do not store proposal targets outside `~/.agents/`.
- Do not make `session-orient`, `session-capture`, `secret-scan`, or `auto-format` blocking.
- If data is missing, emit `unknown` or an empty list instead of creating new fallback semantics.
- Prefer Go as the source of truth for command behavior. Phase 1 shell hooks should mirror the same product contract.

## Acceptance Standard

The implementation is complete only when:

- a new session can orient from canonical artifacts without the agent guessing what to do next
- checkpoint state includes concrete next action and verification summary
- pending proposal review requires no human filesystem spelunking
- the hook system is canonical-bundle-first and platform-native on output
- the weaker implementation agent can follow this spec without adding product assumptions
