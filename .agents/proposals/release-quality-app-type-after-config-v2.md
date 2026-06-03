# Release-quality / promise-reconciliation as a config-v2 app-type

Status: proposed — **sequenced AFTER config-v2** (do it with the new lens in place)
Decision owner: maintainer (raised 2026-06-02 while building release-docs-refresh)

## The idea

The release-docs-refresh + gap-analysis work that emerged this release (promise-vs-code
reconciliation, the `--json` and global-flag-contract findings) is not really a
"docs" task — it is a **project-quality** task that fits the project's
**executor → verifier → reviewer** staged model. It deserves to be specified properly
rather than living as one skill + two ad-hoc agents:

- Its own **app_type** definition (the config-v2 / `WorkflowTask.app_type` concept that
  drives verifier/reviewer dispatch — e.g. alongside `go-cli`, `go-http-service`), so a
  "release-quality" / "promise-reconciliation" task gets the right staged agents,
  verifier profile, and review gate automatically.
- Its own **project scope** (what surfaces it owns: contracts/specs/user-facing docs,
  the code surfaces they promise over).
- Its own **thought agents** per stage — the two lenses built this release are the
  seed: `platform-dirs-change-analyst` (vendor lens) and `promise-gap-analyst`
  (code-vs-documented-promise lens). Under the staged model these become the
  *verifier/reviewer* agents for the app_type, with an executor that applies the
  doc-fixes and routes the code-fixes.

## Why after config-v2 (not now)

config-v2 (the §7A coherence lens — see `[[section-7a-units-lock-wiring]]` / plan task
`config-v2-migration/p4f`) reworks scopes, sources, app_types, and the resolution model.
Specifying a new app_type + its agent staging now, against the *old* model, means
redoing it. Building it **after** config-v2 lands lets it use the new app_type/scope
primitives directly — less rework, easier to work with.

## Ship as a starter + the generic/specific split

The capability should be a **starter offering** (like `skill-architect`, shipped by
`da init`) — but only the *generic mechanism* belongs in the starter:

- **Generic (starter / product layer):** the skill orchestration, the two analyst
  lenses (`platform-dirs-change-analyst`, `promise-gap-analyst`), the gap-classification
  + routing, the eval gate, and the *shape* of the audit matrix (empty/template).
- **dot-agents-specific (project / repo-local layer):** **which** docs/contracts we
  own and the invariants we promise — encoded in `references/audit-matrix.md` +
  `references/scope-doc-map.md` (our `*_CONTRACT.md`/`*_SPEC.md` list, the platform
  matrix, the per-category auditor focus). When working ON dot-agents we get our
  specific behavior; another project fills in its own matrix.

This split **is** the config-v2 layered model (product-defaults + repo-local), which is
why it sequences after config-v2: the starter ships the generic skill, and the project
layers its own audit matrix on top via the new scope/layer primitives. The audit matrix
(built this release) is the seam — generic mechanism reads project-specific knowledge.

## What this enables (the larger goal)

A loop where release/quality passes don't just fix cosmetics: they **notice gaps**
between the project's promises (mission, priorities, the architecture the specs intend)
and the code reality, **track** them, **connect** them to prior project info
(specs/plans/`history`), **plan** the work, and execute it autonomously with quality —
so the project's **architecture, performance, and maintainability evolve together**
instead of drifting apart. The `--json` find → fix → "scan for siblings" →
global-flag-contract reconciliation this release is the pattern in miniature.

## Interim state (until then)

- `release-docs-refresh` skill + `platform-dirs-change-analyst` + `promise-gap-analyst`
  exist in `~/.agents` (registration held for agents-config). They work today as a skill
  + dispatched subagents; they are NOT yet a config-v2 app-type.
- This proposal is the placeholder for the proper formalization. Revisit when
  `config-v2-migration/p4f` (and the app_type rework) lands.

## Done criteria (future)

- A `release-quality` (or similarly named) app_type exists in the config-v2 model with a
  declared verifier profile + review gate.
- The two analyst lenses are wired as that app_type's verifier/reviewer stages, with an
  executor stage that applies doc-fixes and routes code-fixes via `da workflow fold-back`.
- A release-quality task can be created, staged, and run through executor→verifier→reviewer
  like any other app_type task.
