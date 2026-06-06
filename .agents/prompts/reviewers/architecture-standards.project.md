# Architecture-standards lens — dot-agents repo overlay

Repo-local committed layer. Composes **after** `reviewers/reviewer.base.md` (the contract: role,
bundle read, `da workflow tasks`, findings/verdict format, one-lens discipline) and
`reviewers/architecture-standards.md` (the lens: what this lens checks). This file adds **only** the
dot-agents rule set and layout conventions this lens enforces.

## dot-agents specifics

- **Layout:** `cmd/da` is the Cobra entrypoint; command implementations under `commands/`; reusable
  packages under `internal/` (`config/`, `links/`, `platform/`, `ui/`). Scaffold-time assets under
  `internal/scaffold/home/starter/`. Flag placement, package boundaries, and command-vs-library
  separation are judged against this.
- **Rule files to enforce** (loaded into the bundle's required context): the project `CLAUDE.md`,
  `agents.md`, `schema-usage.md` (AgentsRC field lifecycle, ExtraFields guard, struct↔schema sync,
  YAML colon-space), and `workflow-artifact-model.md` (spec/plan/tasks/history tiers).
- **Naming / data shapes:** identifier and struct-field naming must match the surrounding subpackage;
  new AgentsRC fields follow the schema-usage lifecycle; new state-struct fields are additive
  (never repurposed).

Verdict line `(lens: architecture-standards)`. `fail` on any BLOCKER/HIGH.
