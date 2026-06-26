# Proposal: Config-derived agent capability profiles

- type: project-local design input
- status: promoted -> spec `unified-config-profiles` (.agents/workflow/specs/unified-config-profiles/design.md), PR #182, 2026-06-26
- date: 2026-06-26
- source: Claude session `327154c6-3b96-4f48-b9c5-237263d27ec2` rate-limited while discussing orchestrator Skill access and broader config-derived bundles
- related worktree: `.claude/worktrees/agent-a06622f32c13e5299`

## Goal

Replace one-off per-agent skill allowlists with a config-resolved capability
profile: a named bundle that ties together the runtime role, allowed tools,
skills, MCP servers, hooks, rules/settings overlays, and the stage/profile
context that should be materialized for that role.

The immediate bug is narrow: the `orchestrator` agent needs the `Skill` tool to
invoke `orchestrator-session-start`, but Claude Code has no native per-skill
allowlist. The durable fix is broader: the permitted Skill calls should be one
projection of an effective config bundle, not a hand-maintained inline list in
each `AGENT.md`.

## Current state

- `app-type-profiles` already defines a profile as a named, versioned bundle
  for workflow behavior: verifier chain, review kind, graph backend, topology,
  and lenses.
- `skill-relevance-filter` / `config relevance` already treats
  `execution_profile.by_app_type` as the app_type-routed working-set surface for
  skills/agents/lenses/topology.
- `da-project-specifics-source` already says dot-agents-specific agents,
  skills, stage bindings, rules, and settings should move into an explicit
  source layer instead of staying implicit in the repo-local manifest.
- `config-distribution-model` §15 already provides the substrate: scope,
  source, and kind are orthogonal; mergeable policy is a `layer`, executable
  units are `artifact`s, and both are resolved through the one `units` model.
- Claude Code subagent frontmatter only has all-or-nothing `tools: Skill`;
  `skills:` preloads content but does not gate what the `Skill` tool may invoke.
  A `PreToolUse` hook can block disallowed Skill invocations, but the hook
  policy should be generated from effective config, not invented per agent.

## Proposed model

Add a config-layer concept tentatively named `agent_capability_profiles`:

```jsonc
{
  "agent_capability_profiles": {
    "orchestrator": {
      "role": "orchestrator",
      "materializes_agent": "orchestrator",
      "tools": [
        "Bash",
        "Read",
        "Grep",
        "Glob",
        "Skill",
        "Agent",
        "SendMessage",
        "Monitor",
        "TaskStop",
        "PushNotification"
      ],
      "skills": {
        "preload": ["orchestrator-session-start"],
        "allow": [
          "orchestrator-session-start",
          "delegation-lifecycle",
          "isp",
          "iteration-close",
          "plan-wave-picker"
        ]
      },
      "mcp": [
        "code-review-graph",
        "sonarqube"
      ],
      "hooks": {
        "PreToolUse": [
          {
            "matcher": "Skill",
            "gate": "skill-allowlist-gate"
          }
        ]
      },
      "rules": ["global", "project"],
      "stage_context": {
        "execution_profile": "go-cli",
        "stages": ["orchestrate"]
      }
    }
  }
}
```

The exact field names are negotiable. The important boundary is not:

- `app_type` selects the work/pipeline shape.
- `stage_profiles` describe verifier/reviewer prompt composition and return
  gates.
- `agent_capability_profiles` describe what a concrete runtime role can access.
- platform-specific files (`AGENT.md`, hook frontmatter, allowlist JSON, MCP
  config) are projections of the resolved profile.

## Why this is broader than `app_type`

`app_type=go-cli` is too coarse to answer "which skills may the orchestrator
invoke?" A Go CLI plan can run an orchestrator, an implementation worker,
reviewers, verifiers, and a cross-harness reviewer. Those roles share the same
app_type but need different capabilities.

The capability profile is therefore role/stage-specific. It can still reference
`app_type` to inherit defaults, but it should not be keyed solely by app_type.

## Why this is broader than a skill allowlist

The same policy decision usually spans multiple runtime surfaces:

- granting `Skill` means a hook policy must exist;
- allowing `orchestrator-session-start` implies preloading that skill;
- allowing graph readback implies MCP access to CRG;
- a reviewer role may need no `Edit`/`Write` but may need a return-gate skill;
- a cross-harness reviewer may need an alternate-agent CLI probe but must not
  silently self-review.

Keeping those as separate manual lists invites drift. A capability profile
keeps them inspectable as one resolved bundle.

## Enumerated design options

### Option A: Inline hook allowlists in every AGENT.md

This is the narrowest stopgap. It grants `Skill`, adds `skills:` preload, and
adds a `PreToolUse` hook with agent-specific allowlist data.

Use only as a short-term fix for a proven runtime breakage. It is easy to ship
but creates a second config surface and does not explain how skills, hooks, MCP,
and tools relate.

### Option B: Shared hook with managed allowlist data

This is the branch the rate-limited worker started: add one scaffold hook such
as `skill-allowlist-gate` and one data table keyed by agent type.

This is better than Option A because the policy table is centralized and can be
scaffolded. It is still too narrow if the table is only a hook-owned JSON file;
it should be a projection from effective config, not a new source of truth.

### Option C: Capability profiles in config, projected into platform files

This is the target. A layer declares role capability profiles. `da config
explain` and `da config relevance` can show the resolved bundle. `da refresh`
or install/projection materializes:

- Claude subagent frontmatter (`tools`, `skills`, inline `hooks`);
- hook data files such as skill allowlists;
- MCP inclusion for the role;
- platform-specific settings where needed.

The hook remains a runtime enforcement mechanism, but the policy comes from the
layered config model.

### Option D: Derive all capability profiles from traces/relevance

This is future automation, not v1. Trace/scoring data can propose changes:
"orchestrator used plan-wave-picker in 12 successful runs; promote to preload"
or "reviewer never uses build-graph; classify as noise." But the accepted policy
should still be explicit config, with recompute producing proposals rather than
auto-mutating role capabilities.

## Merge and override semantics

Capability profiles should be map-merged by stable profile id, with these
rules:

- additive sets (`preload`, `allow`, MCP refs, rules) merge by set union unless
  the higher layer explicitly replaces the profile;
- dangerous capabilities (`Edit`, `Write`, unrestricted `Bash`, broad MCP
  write tools) should be protected by policy locks or require audited override;
- a lower-precedence layer may define a base role, a repo layer may add
  repo-specific MCP or project skills, and a runtime/task layer may only narrow
  unless explicitly authorized;
- `deny` or `forbid` entries should be considered for hooks and tools because
  subtractive policy is sometimes clearer than replacement.

The merge contract should be owned by the config-layer schema, not by generated
hook scripts.

## Required explain/readback surfaces

The design is only useful if it is explainable:

- `da config explain agent_capability_profiles.orchestrator --json` should show
  the resolved tools, skills, hooks, MCP, source layers, and policy locks.
- `da config relevance --stage orchestrate --app-type go-cli --agent
  orchestrator` should be able to slice the same information for an active task.
- `da doctor` / `da config verify` should detect projection drift: AGENT.md says
  `Skill` is granted but the skill gate is missing, or allowlist data does not
  match the effective profile digest.

## Handling the interrupted branch

The branch in `.claude/worktrees/agent-a06622f32c13e5299` should not be merged
as-is without review:

- It began from a narrow orchestrator bug but expanded to loop-worker and
  multiple reviewer agents.
- It uses a shared managed hook, which is directionally right, but appears to
  keep the allowlist as hook-local data rather than a config-derived projection.
- It likely still needs empirical validation of the exact Claude `Skill`
  `tool_input` field and shell/Sonar checks.

Recommended disposition:

1. Keep the branch as evidence and possible stopgap implementation.
2. Do not broaden all agents until this capability-profile design is accepted.
3. If the orchestrator runtime bug must be fixed immediately, extract the
   minimal orchestrator-only version, but mark it as a projection placeholder
   for the future config-derived capability profile.

## Required canonical follow-on

Create a focused canonical spec/plan for `agent-capability-profiles` that:

1. adds schema shape for role capability profiles;
2. defines merge/protected-field semantics;
3. extends `config explain` / `config relevance` readback;
4. defines projection into Claude subagent frontmatter and hook data;
5. adds drift verification;
6. migrates the orchestrator Skill-access fix as the first dogfood case.

## Non-goals

- Do not create a new standalone skill for this. It is a config/schema/projection
  concern plus a small runtime hook.
- Do not make `app_type` carry every role capability. It should select work
  shape, not runtime authority for every agent role.
- Do not make hook-local JSON the long-term source of truth.
- Do not let trace-derived relevance auto-upgrade permissions without a reviewed
  config change.
