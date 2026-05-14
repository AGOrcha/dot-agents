# Proposal Routing

Proposal scope follows the intended write scope of the change.

## Rule

- Project-specific work routes to the project queue and project write surface.
- User-specific shared behavior routes to the user queue and user write surface.
- Team-shared behavior routes to the team queue and team write surface.
- Org-shared behavior routes to the org queue and org write surface.

Do not route by artifact type alone. Route by who owns the durable result and where the approved write must land.

## Scope mapping

- `project` / `repo`
  - Queue: `.agents/proposals/`
  - Archive: `.agents/proposals/archived/`
  - Target root: repo root
- `user`
  - Queue: `~/.agents/proposals/`
  - Archive: `~/.agents/proposals/archived/`
  - Target root: `~/.agents/`
- `team`
  - Queue: team-scope proposal store
  - Archive: team-scope archived proposal store
  - Target root: team-scope canonical store
- `org`
  - Queue: org-scope proposal store
  - Archive: org-scope archived proposal store
  - Target root: org-scope canonical store

Repo and user scope roots are filesystem-local today. Team and org scope roots follow the configured scope store for that environment.

## Implementation boundary

This rule defines routing intent only. It is not the full `da review` command contract.

Any implementation of scope-routed `da review` should be grounded by:

- a spec for reviewer-facing business rules and command behavior
- a plan for architecture, module boundaries, verification, and task scheduling

See `.agents/proposals/scope-routed-da-review.md` for the high-level proposal that should drive
those follow-on artifacts.

## Examples

- Team skill update -> `team`
- User hook update -> `user`
- Project code change or project workflow artifact -> `project`
- Project subagent -> `project`
- Org plugin -> `org`
- Org shared library metadata or config -> `org`
- Team KG memory -> `team`
- Project lesson -> `project`
- User KG note -> `user`

## Notes

- Project-local proposals may target repo-local agent artifacts under `.agents/` or normal project files when the approved write is project-owned.
- Shared scopes should only receive proposals when the resulting artifact is intended for reuse outside the current repo.
- If ownership is ambiguous, choose the narrowest scope that fully satisfies the use case.