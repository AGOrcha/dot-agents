# ADR-0006: Binary rename strategy — `dot-agents` → `da` via hard cutover

**Status:** accepted
**Date:** 2026-05-03
**Owners:** dot-agents (user-driven)
**Related:** [ADR-0001](0001-adopt-architecture-decision-records.md) (ADR conventions); [`binary-rename-da-sweep` plan](../../.agents/workflow/plans/binary-rename-da-sweep/) (operationalizes this decision); [`agent-context-resolution-architecture.md` §6.5](../../.agents/proposals/agent-context-resolution-architecture.md) (audit pattern that motivated the broader sweep discipline)

## Context

The user is renaming the user-facing binary from `dot-agents` to `da`,
following the UV convention (short, fast to type, distinct from the
project name). WIP changes have been started in `.goreleaser.yaml`,
`.github/workflows/auto-release.yml`, and `.agentsrc.json` — the
`builds:` block already produces `binary: da`, the smoke step in
auto-release runs `./bin/da --version` etc. The brew formula in the
goreleaser config still has a residual `bin.install "dot-agents"` line
that needs reconciling.

The rename touches a large surface (~150–200 files across docs, plans,
specs, skills, rules, research, tests) but the source dir
(`cmd/dot-agents/`), Go module path
(`github.com/NikashPrakash/dot-agents`), project name, and `.agents/` /
`~/.agents/` directory layout are explicitly **not** part of the
rename. The rename is the *invocation surface*, not the *project
identity*.

Three strategy options were on the table:

- **(A) Compat shim:** ship both `da` and `dot-agents` binaries from
  the same Go entrypoint. `dot-agents` prints a one-line stderr
  deprecation notice, then runs normally. Existing user scripts and
  hooks keep working through a deprecation window (6–12 months); the
  shim is dropped after the window expires.
- **(B) Hard cutover:** rename in one sweep. Only `da` is built and
  installed. `dot-agents` no longer exists post-release. Users update
  scripts and habits at release time.
- **(C) Hybrid:** ship the shim now, sweep documentation at a relaxed
  pace, drop the shim later. Smoothest migration; highest total cost.

The user's stated intent (encoded in the
`binary-rename-da-sweep/TASKS.yaml` t1 notes) is **hard cutover**.

## Decision

**Adopt option (B) — hard cutover.**

- `da` becomes the only binary built by `.goreleaser.yaml`. The legacy
  `dot-agents` binary is not built, installed, or smoke-tested
  post-rename.
- The `binary-rename-da-sweep` plan's t2 lands the build/CI rename in
  one focused commit, including reconciling the brew formula's
  `bin.install` line so it matches the binary name.
- Plan tasks t3–t5 sweep documentation (repo + `~/.agents/` +
  `~/.claude/CLAUDE.md`) so user-facing references read `da`
  consistently. These can run in parallel since their write scopes do
  not overlap.
- The plan's t7 (drop shim) is **not applicable** under hard cutover
  and will be marked closed-NA when the plan archives.
- Release notes for the version that introduces `da` call out the
  breaking change with a one-line migration: replace `dot-agents` with
  `da` in scripts, aliases, and CI configs.
- Major version bump on the release that introduces the rename
  (semver: removing `dot-agents` is a breaking change).

The `binary-rename-da-sweep/TASKS.yaml` t1 notes captured the
direction explicitly: hard cutover unless t1 surfaces a concrete
blocker. None has been surfaced. The decision stands.

## Consequences

**Easier:**

- Single binary name everywhere — no maintenance overhead for the
  legacy invocation, no deprecation-notice drift, no two-builds
  goreleaser config.
- Documentation, skills, plans, and ADRs reach a consistent
  steady-state in one release cycle rather than carrying "either name
  works" prose for 6–12 months.
- Cleaner mental model: `dot-agents` is the *project*, `da` is the
  *binary*. The two stop being conflated.
- The plan's task graph simplifies: t7 (drop shim) collapses to a
  closeout note instead of a calendar-gated future task.

**Harder:**

- Existing user scripts, shell aliases, hooks, and CI configs that
  invoke `dot-agents <cmd>` break at the moment they pull the new
  release. Migration is one-shot and visible.
- External integrations (if any) that exec the binary by name need
  attention; mitigated by clear release notes and major version bump.
- Documentation sweep (t3–t5) is now load-bearing for release
  readiness — stale references read as outright wrong, not as
  legacy/working. The sweep cannot be deferred indefinitely.
- The repo's `.agents/history/` is preserved unchanged (sacred record
  of what was actually run), so reading old impl-results.md and
  archived plan notes will continue to show `dot-agents <cmd>` —
  that's correct for the era they document.

**New risks:**

- Users who pull the rename release without reading release notes hit
  "command not found." Mitigation: release notes + a one-liner in the
  brew formula description explaining the rename.
- The `~/.agents/skills/dot-agents/` namespace dir keeps the project
  name and is **not** renamed; agents reading `da` skills must not
  confuse the binary rename with a directory rename. Mitigation: the
  rename plan's anti-scope makes this explicit.
- The TS port binary (`dot-agents-ts`) still uses the legacy name.
  ADR-0007 (produced by t6) decides naming there; if it chooses
  `da-ts`, the same hard-cutover principle applies; if it keeps
  `dot-agents-ts`, the inconsistency is documented and accepted.

**Locked-in commitments:**

- Once the rename release ships, reverting to `dot-agents` would
  itself be a breaking change. The hard cutover is intentionally
  forward-only.
- Any future invocation-name changes (e.g. `da` → something else)
  would need their own ADR superseding this one.

## Alternatives considered

- **(A) Compat shim** — rejected on user preference. The maintenance
  overhead of carrying two binaries plus the deprecation-window
  bookkeeping wasn't judged worth the migration smoothness for this
  project's user base. The shim's main benefit (zero-friction
  migration for existing scripts) matters less here than for a
  wide-deployment tool.
- **(C) Hybrid** — rejected as the worst of both: ship the shim's
  complexity now and still pay the sweep cost later. Hybrid only wins
  when the user-base is large enough that the deprecation-window
  smoothness is critical; that's not this project.
- **No rename** — rejected: the user has already started the rename
  per UV convention, and the typing-cost-per-invocation benefit is
  real. The longer the rename is deferred, the more documentation
  drift accumulates from continued `dot-agents` references.

## References

- The user-WIP diff on `.goreleaser.yaml` (build target renamed to
  `binary: da`), `.github/workflows/auto-release.yml` (smoke step uses
  `./bin/da`), and `.agentsrc.json` (rename only; no schema change).
- [ADR-0001](0001-adopt-architecture-decision-records.md) — establishes
  the Nygard format and conventions used here.
- The `binary-rename-da-sweep` plan's TASKS.yaml — t1 notes
  pre-encoded user intent toward hard cutover; t2 implements; t3–t5
  sweep; t6 decides TS port naming (potentially ADR-0007); t7 closes
  not-applicable under this decision.
- UV (`https://github.com/astral-sh/uv`) — convention precedent for
  short binary names paired with longer project names.
