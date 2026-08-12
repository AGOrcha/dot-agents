# Changelog

All notable changes to dot-agents will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`da install` now maintains the managed `.gitignore` block.** Previously only `da refresh` wrote it, so a freshly-installed repo carried its projected outputs (`.codex/`, `.mcp.json`, `.vscode/mcp.json`, `AGENTS.md`, `.cursor/`, `.github/copilot-instructions.md`, `.github/hooks/`) as untracked noise until someone happened to run refresh. Install and refresh now share one step and converge on a byte-identical block, derived from the platforms' own declared outputs rather than a hardcoded list.
- **`gitignore_projections` in `.agentsrc.json`.** Optional boolean opting a project out of the managed block; absent means enabled, and an explicit `false` also removes a block a previous run wrote.
- **`kg build --no-recurse-submodules`.** Opts a superproject build out of indexing its submodules. The exclusion is reported, not silent: the skipped roots are named in the build summary and in the per-root status breakdown. Each build records which roots it covered next to the graph (`.code-review-graph/da-workspace.json`) so a later `code-status` can tell a deliberately excluded root from one that was never indexed.

### Fixed

- The managed `.gitignore` block now covers the `*.dot-agents-backup` sidecars install writes when it displaces a pre-existing user file (e.g. `AGENTS.md.dot-agents-backup`), which were previously left untracked.
- **`da kg build` no longer ignores git submodules.** Enumeration went through `git ls-files`, which reports a submodule as a single gitlink and none of the files inside it, so a superproject build indexed only root-level source and still reported READY — measured live at 47 nodes / 2 files where the workspace held 5,946 nodes / 885 files. A build now indexes the superproject and every initialized submodule, merging their graphs under a per-repository namespace.
- **`da kg code-status` reports what was actually indexed.** Status carries a per-root breakdown (nodes, files, indexed) and a new `incomplete` state: a submodule that exists in the checkout but is absent from the graph is named, with the reason, and `ready` is false. `--require-graph` consumers refuse an incomplete graph instead of silently querying a partial one. A root the last build deliberately excluded, or indexed and found no symbols in, is reported as such and does not make the graph incomplete.
- **Merged graph data now goes through postprocess.** Submodule graphs are merged before any derived state is computed, and one postprocess pass rebuilds flows, communities, and the FTS index over the merged rows — closing the trap where merged base tables sat next to an empty search index that still looked healthy.
- **Merged rows are replaced, not accumulated.** A re-merge of the same submodule now clears its namespace first. `edges` carries no unique constraint, so an insert-only merge would have duplicated every submodule edge on a second merge and left symbols deleted upstream in the graph forever.
- **No more cross-repository false edges.** CRG resolves edge endpoints by qualified name, so merging two repositories linked every `Button` to every other `Button`. Merged node names and both edge endpoints are now namespaced per repository, so an edge can only form where a real reference exists.

## [0.5.0] - 2026-07-20

A large feature release: a git-backed workflow WorkStore, a first-class worktree
platform, an observability dashboard, an evaluation harness (`da eval`) and recipe
runner (`da run`), a session-handoff journal, and a review/admin surface — plus a
broad "loud failures" hardening pass.

### Added

- **git-ref WorkStore backend.** Opt-in `work_tracking.backend=git-ref` stores canonical plan/task coordination state as per-task blobs on `refs/agents/state` (CAS-guarded), decoupled from code-branch commits, with graceful working-copy fallback, `da workflow state-ref reconcile` to seed the ref, managed-artifact mirroring (delegation / merge-back / verification / review), and a `read_from=master` additive stopgap (#410, #413, #418, #419, #424, #425, #426, #432, #435, #439, #441).
- **Worktree platform.** `da worktree create` / `merge-back` with resolved agent-config loading, shared-git-dir admin resolution, registry isolation, and an eval sandbox worktree (#268, #381, #409, #411, #412, #414).
- **Evaluation harness + `da eval`.** Sandbox, agent runners (claude/codex/copilot), a KG-derived task generator and language verifiers (Go/Python/TS) over shared generator + verifier cores, a harness driver (generate→provision→run→verify→score), a scoring bridge into R1 records, and atomic run persistence (#268, #274, #279, #280, #285, #287, #288, #292, #297, #299–#302, #306, #311, #315, #322, #328).
- **`da run` recipe execution.** Parser/tokenizer + in-process dispatch with env-substitution, fail-fast, and acceptance checks (#293, #316, #323, #327, #330, #378, #380).
- **Observability dashboard.** An SSE broker, a read-through store projection with recompute-on-miss + fswatch bridge, REST handlers on a pinned API contract, a Vite/React SPA (rubric view, R3 mount), and a Cloudflare deploy path (#251, #260, #269, #275, #286, #398, #404, #406, #421–#423, #428, #430, #443, #444).
- **Observability dashboard deployment + `da observability`.** The dashboard also ships as a single-tenant Cloudflare Worker (reference deployment `obs.agorcha.dev`) — Durable Objects + D1 for durable iteration/score history, fail-closed CF Access JWT auth, and the SPA served over a live WebSocket transport. `da observability login | sync | status` publishes local workflow telemetry to the endpoint configured in the `.agentsrc.json` `observability` block (a `credential-ref` resolved from the credential store over HTTPS only); events queue crash-safe in `.agents/active/obs-outbox/` and drain idempotently (server dedupes; `sync --full` rebuilds the remote from local `.agents/history/`), wired best-effort into `workflow checkpoint` / `verify record` without changing a local command's exit. See `docs/cf-access-bootstrap.md` and `obs/`.
- **`observability` block in `.agentsrc.json`.** Configures the endpoint and a strict `credential-ref` auth reference (`{kind, id}`); an enabled non-loopback endpoint requires auth and must be absolute `https:`.
- **Session-handoff journal.** An append-only journal of workflow/KG/review mutations with typed schemas, a live-state snapshot, verified replay-and-recover (`da workflow journal` + PreCompact / SessionStart hooks), and agent-handoff cadence (#205–#212, #216–#218, #220).
- **Review / admin surface.** A review collection endpoint, a `human_label` scoring signal, the `da review` admin CLI, and an append-only SHA-256-chained audit log (#250, #261, #270, #277, #282).
- **KG-ideate (`kgi`).** A KG-brief → staged-handoff → spec/plan-scaffold ideation flow with a verification model and typed schemas (#317–#320, #324, #326, #329, #331).
- **App-type profiles + full-loop orchestration runtime.** Explicit app-type → verifier/reviewer profile resolution and a staged loop runtime (#388, #389).
- **Graph-backend / CRG.** A cross-adapter `reads_from` compliance view and a named CRG parity gate (#207, #396, #397, #416).
- **KG query layer + difficulty derivation** for task generation (#248, #259).
- **Proposal `write_scope`.** Validated scope routing on proposals + fold-back CLI threading (#440).
- **Antigravity** shipped as the sixth projection platform, with onboarding docs (#204, #223, #240).
- **Config v2** layered model + docs and the git-source install smoke path (#228, #253, #313, #314).
- **da-managed `.gitignore` autofill** for generated wiring (#382–#384), and a package/OCI artifact install path (#407).

### Changed

- Dashboard API ETags are content-derived (the mtime sketch was unimplementable) and recompute-on-miss aligns to the store contract (#276).
- Platform docs, decks, and the README realigned to the shipped six-platform model and the `PLATFORM_DIRS_DOCS.md` matrix (#240, #241, #334).
- `da workflow plan archive` commits the move by default so archives persist through the fresh-clone / worktree loop (#246).

### Fixed

- **Loud-failure hardening train.** Config detectors, platform prune, graphstore atomic writes, sync git checks, user-home preflight, delegation / registration visibility, and workflow closeout now fail loudly / atomically instead of silently degrading (#351–#369, #408).
- **git-ref structural-write clobber.** All canonical writes mirror at the save choke point so structural writes stay ref-visible (#434, #435).
- **Windows reliability.** `agentslock` acquire/release races + transient hardlink retries, `fsops` remove retries, and KG seam path normalization (#219, #221, #255, #264, #308).
- Fold-back `--dry-run` no longer has side effects (#305); `da run` / eval dry-run paths propagate correctly (#321, #325, #380).
- Git SSH auth fallback + schema URL host correction (#348, #349).

### Removed

- The unsafe initial `backend=git-ref` cutover was rolled back mid-flight while a clobber bug was open, then re-cut over after the fix (#433 → #434 → #439).
- Presenter-only ASDLC material no longer renders on the public docs site (still in the repo / decks) (#234).

### Internal

- Extensive workflow-state reconciliations, plan closeouts, and proposal retirements across the r2–r5 / git-ref / graph-backend / worktree lanes; dependency bumps; Sonar-debt cleanups; a mandatory release-docs-refresh step and the 0.5.0 docs refresh (#333, #334); pipeline-craft / transcript-analysis research and the lesson index (#390, #393, #405, #442, #446). A full per-PR coverage index (223 merged PRs since v0.4.2) is in the PR description.

## [0.4.2] - 2026-06-29

A patch re-release fixing the macOS Developer ID signing chain (the v0.4.1 artifact failed Gatekeeper on a clean install).

### Fixed

- **macOS signing chain.** Re-released with a corrected Developer ID signing + notarization flow; the fsguard `agentslock` allowlist line moved with the lock primitive (#213, #214, #215).

## [0.4.1] - 2026-06-24

A cross-platform reliability release: first-run lock acquisition now works on
Windows, and `da config migrate` lands as the opt-in v1→v2 manifest migrator.

### Fixed

- **First-run lock acquisition on Windows.** `da config explain` / `da install`
  no longer fail with "The system cannot find the file specified" — `agentslock`
  acquire now creates the lock file's parent directory (`MkdirAll`) before
  acquiring, so first-run lock acquisition succeeds cross-platform (#148).

### Added

- **`da config migrate`** — an opt-in v1→v2 `.agentsrc.json` migrator. It backs up
  the original to `.agentsrc.json.v1.bak`, is idempotent, and supports `--dry-run`
  to preview without writing. The `da init` deprecation hint now points at this
  real command (#138).

### Internal

- First-run absent-parent lock smoke test across the OS matrix, guarding the #148
  escape (#152).
- A `fsguard` lint enforcing that filesystem mutations route through
  `internal/fsops` (#151).

## [0.4.0] - 2026-06-23

The config-v2 release: a layered configuration model with a content-addressed
lockfile, a `da config` command surface to inspect and reconcile it, and a
reshaped status/doctor/install/refresh lifecycle built on it.

### Added

- **Layered configuration (config-v2).** A `.agentsrc.json` manifest can now
  `extends` config layers sourced from local paths, git, or HTTP via
  `source-id:path@version`. Layers are merged by winning-layer precedence and the
  resolved set is pinned in a unified `.agentsrc.lock` — one `units` section keyed
  by `source:path@version` plus a top-level `inputs_digest`. `kind` (layer vs
  artifact) governs merge/trust; source is orthogonal to kind.
- **`da config sync`** — re-fetch the declared layers and rewrite the
  `.agentsrc.lock` `units` section (the explicit upstream re-check). Honors
  `--dry-run` (previews without writing the lock).
- **`da config lint`** — validate the repo manifest and each declared `extends`
  layer against the AgentsRC layer schema; non-zero exit on any invalid layer.
- **`da config relevance`** — resolve a task's execution profile
  (units / topology / lenses) by `app_type` (`--app-type`/`--task`/`--stage`/`--json`).
- **Unified artifact sourcing.** Executable `packages` (artifacts) may be sourced
  from git, local, HTTP, or OCI via `source:path@version` — not OCI-only.
- **Content-driven staleness + `cache_keys`.** Re-resolution is driven by an
  `inputs_digest` over local scopes and per-source cache keys (no clock); a
  source's `cache_keys` now actually governs online re-fetch.
- **`internal/docsaccess` client** — attaches Cloudflare Access service-token
  headers so `da` can reach the maintainer-only internal-docs surface.

### Changed

- **`da config explain` is the effective-config truth surface** and now
  **auto-locks** (writes the lock to stay current, like `uv tree`), showing each
  field's value and provenance (winning layer) across the resolved stack.
- **`da status` and `da doctor` reshaped.** `status` is now fleet / link-health
  only — config inspection moved to `da config explain`. `doctor` is **read-only**:
  it reports problems and the command to fix them (`da refresh` / `da config sync`)
  and **no longer repairs**.
- **`da install` and `da refresh` prune by default** (exact projection: converge
  the managed tree to exactly what the lock declares, removing stale managed
  links). Pass `--inexact` to keep the additive behavior.
- **Concurrency-safe lockfile writes.** An interprocess lock on `.agentsrc.lock`
  prevents lost updates when multiple `da` processes write concurrently, with
  stale-lock reclaim so a crashed writer cannot wedge future writes.

## [0.3.4] - 2026-06-02

### Added

- `skill-architect` now ships as a starter skill, so `da init` includes it out of
  the box for designing, auditing, and improving your own skills.
- `skill-architect` is **provider-pluggable**: its `eval`/`improve`/`optimize`
  modes drive whichever of the five dot-agents platform CLIs is present —
  auto-detecting claude / cursor / codex / opencode / copilot — and can target the
  Anthropic API, any OpenAI-compatible endpoint, or an arbitrary CLI via
  `SKILL_ARCHITECT_PROVIDER`. The default (local `claude`) is unchanged.
- `da config verify` — a standalone, offline setup/release contract check: the
  manifest parses, declared local source layers exist, optional integrations
  (code-review-graph) are ready, and for declared remote `extends` layers the
  downloaded assets are present in the cache at the SHA the lockfile pins — all
  without re-fetching. Emits `--json` and exits non-zero on failure for CI.

### Changed

- `da config explain` / `da config verify` now honor the **global** `--json` flag
  like every other command, instead of defining a local one — so `--json` is
  position-independent and consistent (`da --json config verify` and
  `da config verify --json` are equivalent).
- **AGOrcha org migration.** The project moved to the `AGOrcha` GitHub org: the Go
  module path is now `github.com/AGOrcha/dot-agents`, releases and the `da`
  Homebrew cask publish under AGOrcha (`brew install agorcha/tap/da`), and
  SonarCloud/CI/docs point at AGOrcha. No CLI behavior change.
- `da refresh` now re-detects installed editors and **auto-enables** newly
  installed platforms (recording their versions), so installing an editor after
  `da init` no longer leaves a "nothing to refresh" dead end.

### Fixed

- `da doctor` / `da status --audit` report accurate state: the link-health
  headline count matches the real managed-link count, platform badges reflect
  config-enabled ∧ installed platforms (no false ✓ for disabled ones), and
  present non-symlink files read "(local file)" instead of "(not linked)".
- The `graph-update` post-edit hook no longer errors on every edit when the
  optional code-review-graph tool is absent (it degrades to a no-op), and
  `graph-orient` calls a valid `da kg health` instead of a missing subcommand.
- A fresh clone installs cleanly: the project manifest no longer references a
  private external source, so `da install` doesn't error or silently drop skills.
- Bumped `github.com/jackc/pgx/v5` (memory-safety advisory) and the docs-site
  `astro` dependency to patched versions.

## [0.3.3] - 2026-05-31

A patch release. The user-facing CLI behaves exactly as it did in 0.3.2 —
this train is dominated by internal foundation work, workflow-orchestration
machinery, and CI/release hardening. It is organized by **theme** rather than
the Keep-a-Changelog Added/Changed/Fixed split because most of the ~775
commits since v0.3.2 cut across all three. Where a subsystem is merged but not
yet wired to a command, that is stated explicitly; the next genuine
feature land is reserved for `0.4.0`.

### Configuration foundation (config-v2 — internal/dormant)

- Landed the two-tier config-v2 substrate as internal packages only. It is
  **foundation, not a user-facing feature**: none of the surfaces below are
  reachable from the CLI yet, and existing `.agentsrc.json` loading is
  unchanged. The user-facing config-v2 commands land in `0.4.0`.
  - Units lockfile + content-hash staleness detection
    (`internal/config/lock_units.go`, `resolve_locked.go`,
    `lockstatus.go`, `staleness.go`).
  - `EnsureResolved` auto-sync seam (`internal/config/ensure_resolved.go`) —
    the single resolution entry point future commands will call.
  - Layered resolver + layer schema (`resolver.go`, `layer_schema.go`).
  - Source-type fetchers — local, HTTP, and OCI
    (`local_source.go`, `fetcher_http.go`, `fetcher_oci.go`).

### Workflow orchestration (layered-pr-fanout)

- `awaiting_review` task status with its sub-status umbrella (verifier-pass,
  lens-accepted, human-pending) so review-blocked work is tracked distinctly
  from `in_progress` and `completed`.
- Slot/dependency accounting for the eligible queue, plus `blocked-on:<ref>`
  state with auto-resume when the referenced upstream clears.
- PR base-branch resolution for stacked/layered fan-out.

### Events

- Unified internal event-contract core (`internal/events`): envelope schema,
  dispatch, producer, registry, and JSONPath matching. Internal substrate
  for upcoming event-driven workflow features; not yet wired to a command.

### Security

- Encrypted credential store (`internal/credstore`) with hybrid
  post-quantum at-rest encryption: payloads are sealed with AES-256-GCM
  under a key derived from a hybrid X25519 + ML-KEM-768 KEM, so stored
  credentials stay confidential if **either** the classical or the
  post-quantum half is broken. Private seed material is held in the OS
  keyring. Internal substrate; not yet wired to a command.

### CI, coverage & quality gates

- Per-file coverage gate (`scripts/coverage-gate.sh`) replacing the single
  aggregate threshold, with an explicit exceptions allowlist that is pruned
  as files reach the bar.
- Zero-new-issues Sonar gate (`scripts/sonar-new-issues-gate.sh`) blocking
  net-new static-analysis findings on a PR.
- Cross-platform (Windows) test fixes, including byte-range file locking and
  path/cleanup handling, plus a multi-OS test matrix.
- Deduplicated push + PR CI pipelines and Sonar worktree-path correctness.

### Docs, site & release tooling

- Interactive Astro + Cytoscape documentation site under `docs/web/`,
  deployed to agorcha.dev via a Cloudflare Worker pipeline
  (`deploy-docs.yml`) with scheduled deploy-token rotation.
- Cosign keyless signing (sigstore + GitHub OIDC) wired into goreleaser;
  every release artifact and checksum is signed — verify per
  `docs/RELEASE_VERIFICATION.md`. Native macOS/Windows code signing remain
  **deferred**.
- `cmd/dot-agents/` → `cmd/da/` rename (binary name matches install path;
  Go module path unchanged); docs-accuracy pass to match.
- Orchestration starter skills promoted to global and scaffolded by
  `da init`; reviewer-lens agents and `da workflow review_gate`
  staged-dispatch machinery.

## [0.3.2] - 2026-05-17

Knowledge-graph subpackage line (PR3c).

### Added

- **`commands/kg/`** extracted — graph/CRG bridge, code-warm link sync,
  query/lint/maintain, curation cycle; wired under the CLI.

### Fixed

- `persistReweavedNote` no longer drops note bodies on reweave.
- Note-id path traversal: ingest now sanitizes `src.ID` from inbox
  frontmatter (regression-tested).

### Changed

- kg test layout normalized to source-mirroring files (no behavior
  change).

## [0.3.1] - 2026-05-17

Workflow subpackage line (PR3b).

### Added

- **`da workflow`** surface extracted into `commands/workflow/` —
  plan/task lifecycle, state/checkpoint/orient, verification +
  review-gate + delegation, drift/sweep/graph, fold-back.
- **`internal/graphstore`** — `Store` interface with SQLite + Postgres
  backends, CRG bridge, MCP server, impact BFS, schema.

### Changed

- Test layout normalized to source-mirroring files in the
  workflow/graphstore packages (no behavior change).

## [0.3.0] - 2026-05-17

First Go-binary (`da`) release of the extracted surface; ships PR1–PR3a
plus test-structure hygiene.

### Added

- **Go CLI foundation** (PR1): `internal/config`, JSON schemas, CI,
  Homebrew tap, `da` binary entrypoint.
- **Platform core** (PR2): resource model, shared target plan, and the
  `internal/projectsync` extraction.
- **New command surface** (PR3a): `review`, `mcp`, `settings`, `rules`,
  `ux`, `session_stats`; `agents`/`hooks`/`sync`/`skills` extracted into
  cohesive subpackages.

### Changed

- Binary renamed to `da`; numerous lifecycle commands (`add`, `doctor`,
  `import`, `init`, `install`, `refresh`, `remove`, `status`) re-homed
  on the Go surface.
- Test layout normalized to source-mirroring files; iteration-numbered
  grab-bag test files retired (no behavior change).

### Fixed

- Windows link model (junction + hardlink, no Developer Mode),
  SIGKILL-safe promote journal, command-layer transactional-integrity
  and platform sweep hardlink-cleanup hardening.

## [0.1.8] - 2026-01-11

### Added

- **Unified Skills Architecture**
  - `skills` - New CLI command to manage directory-based skills
  - `skills new <name>` - Create a new skill from template
  - `skills edit <name>` - Open skill's SKILL.md in $EDITOR
  - `skills show <name>` - Display skill contents
  - `skills validate <name>` - Validate skill frontmatter
  - `skills migrate` - Migrate from old flat commands/ format
  - `link --global` - Link global skills to all platforms
- **Directory-based Skill Structure**
  - Each skill is a directory with SKILL.md (not a flat .md file)
  - Optional scripts/ and references/ subdirectories
  - YAML frontmatter for metadata (description, platforms, etc.)
- **Default Skills**
  - `agent-start` - Session startup procedure
  - `agent-handoff` - Session handoff procedure
  - `self-review` - Pre-commit checklist
- **Multi-Platform Skills Integration**
  - Claude Code: Symlinks directories to `.claude/skills/`
  - Cursor: Symlinks SKILL.md to `.cursor/commands/{name}.md`
  - Codex CLI: Symlinks directories to `.codex/skills/`
  - No prefix required - `/agent-start` not `/global--agent-start`
  - Project skills shadow global skills (with CLI warning)

### Changed

- `doctor` now checks for skills directory structure and symlinks
- `init` now creates `~/.agents/skills/global/` with skill templates
- `add` now creates platform-specific skill symlinks automatically

## [0.1.7] - 2026-01-11

### Added

- **Claude Code Hooks Support**
  - `hooks` - New CLI command to manage hooks
  - `hooks list` - List configured hooks
  - `hooks add` - Add a new hook
  - `hooks remove` - Remove a hook
  - Global hooks in `~/.agents/settings/global/claude-code.json`
  - Project hooks in `~/.agents/settings/<project>/claude-code.json`
- Settings templates created during `init` and `add`

### Changed

- `doctor` now validates hooks configuration
- `init` creates settings templates with hooks examples

### Fixed

- bash 3.x compatibility (removed `local -n` nameref)
- Empty array handling in strict mode

## [0.1.0] - 2026-01-10

### Added

- Initial release
- **Core Commands**
  - `init` - Initialize `~/.agents/` directory structure
  - `add <path>` - Add a project to dot-agents management
  - `remove <project>` - Remove a project from management
  - `status` - Show managed projects and their status
  - `doctor` - Health check and diagnostics
  - `audit` - Show which configs are applied where
- **Sync Commands**
  - `sync init` - Initialize git repository in `~/.agents/`
  - `sync status` - Show git status
  - `sync commit` - Commit all changes
  - `sync push` - Push to remote
  - `sync pull` - Pull from remote
  - `sync log` - Show recent commits
- **Utility Commands**
  - `context` - Output configuration as JSON for AI agents
- **Agent Support**
  - Cursor (`.cursor/rules/` with hard links)
  - Claude Code (`CLAUDE.md`, `.claude/` with symlinks)
  - Codex (`AGENTS.md` with symlinks)
  - OpenCode (detection only)
- **Installation**
  - Homebrew formula
  - curl install script
- **Features**
  - Automatic agent detection
  - Hard links for Cursor (required - doesn't follow symlinks)
  - Symlinks for Claude Code and Codex
  - JSON output for all inspection commands
  - Dry-run mode for all mutating commands
  - XDG-compliant state storage

### Notes

- Windows support deferred to future release
- Tasks and History features are opt-in and not yet implemented

[Unreleased]: https://github.com/NikashPrakash/dot-agents/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/NikashPrakash/dot-agents/compare/v0.4.2...v0.5.0
[0.4.2]: https://github.com/NikashPrakash/dot-agents/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/NikashPrakash/dot-agents/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/NikashPrakash/dot-agents/compare/v0.3.4...v0.4.0
[0.3.3]: https://github.com/NikashPrakash/dot-agents/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/NikashPrakash/dot-agents/releases/tag/v0.3.2
[0.1.8]: https://github.com/dot-agents/dot-agents/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/dot-agents/dot-agents/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/dot-agents/dot-agents/compare/v0.1.0...v0.1.7
[0.1.0]: https://github.com/dot-agents/dot-agents/releases/tag/v0.1.0
