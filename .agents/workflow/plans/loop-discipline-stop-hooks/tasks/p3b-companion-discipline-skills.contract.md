# P3b Companion Discipline Skills Contract

- task: `p3b-companion-discipline-skills`
- requirements: R9.1, DC10
- dependencies: `p2-hook-scripts`, `p3-starter-promotion`

## Goal

Treat `agent-handoff` and `delegation-lifecycle` as first-class companion
skills: both should ship complete starter assets, and each should receive a
specific lifecycle-hook suitability assessment rather than being ignored or
wired to a hook by assumption.

## Grounded Starting State

- `internal/scaffold/home/starter/skills/global/agent-handoff/` currently
  contains only `SKILL.md`, while the source
  `~/.agents/skills/global/agent-handoff/` also contains instruction and
  template trees.
- `~/.agents/skills/dot-agents/delegation-lifecycle/` contains `SKILL.md`
  and three instruction files; no corresponding starter tree exists.

## Source to Destination Map

| Source | Starter destination |
| --- | --- |
| `~/.agents/skills/global/agent-handoff/` complete tree | `internal/scaffold/home/starter/skills/global/agent-handoff/` |
| `~/.agents/skills/dot-agents/delegation-lifecycle/` complete tree | `internal/scaffold/home/starter/skills/global/delegation-lifecycle/` |

## Hook Suitability Assessment

Write `.agents/history/loop-discipline-stop-hooks/additional-discipline-skill-assessment.md`
with, for each skill:

- the durable artifact or state transition it owns;
- whether that transition is observable at `Stop` / `SubagentStop`, or has a
  deterministic `PreToolUse` prerequisite or bounded `PreCompact` continuity
  use;
- whether a hard-remediation rule can be deterministic and recoverable;
- the decision: no hook required, follow-up hook proposal, or fold into an
  existing bundle.

Record `PostToolUse` / `PostToolUseFailure` observation opportunities only as
inputs to the R1.5 evaluation; this assessment does not create private
telemetry or scoring.

Do not create a hook solely because a skill is related to loop discipline.

## Acceptance

- Starter copy tests assert representative instruction/template files for
  `agent-handoff` and instruction files for `delegation-lifecycle`.
- A sandbox starter materialization produces both complete skill trees.
- The hook assessment uses D7's observable-evidence boundary.

## Out of Scope

- Implementing a new companion hook before an evidence-backed contract is
  approved.
