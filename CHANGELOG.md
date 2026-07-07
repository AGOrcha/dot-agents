# Changelog

All notable changes to dot-agents will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.0] - 2026-07-04

A capability release: a code-task generation and evaluation pipeline (`da eval`),
shebang-executable `da run` recipe scripts, review RBAC plus a tamper-evident
audit log, and the crash-survivable session-handoff journal (`da workflow
journal`). This 0.5.0 entry also backfills the previously missed 0.4.2
CHANGELOG window — most notably the layered-config follow-through and the macOS
release-signing repair that shipped in that cut. All additive — no breaking
changes.

### Added

- **`da eval` — code-task generation and evaluation pipeline.** A new command
  group (`gen`, `run`, `ls`) that turns the Tree-sitter knowledge graph into
  runnable agent evaluations. `da eval gen` synthesises a language-agnostic
  `TaskSpec` (`--language go|python|typescript`, `--difficulty easy|medium|hard`,
  `--template`, `--out`); `da eval run` runs one task end-to-end inside an
  isolated sandbox worktree, verifies the result, and scores it
  (`--agent claude|codex|copilot`, `--task`, `--repo-dir`, `-n`/`--dry-run`
  preview); `da eval ls` lists persisted runs. Go, Python, and TypeScript
  generators and verifiers run over one shared engine, difficulty is derived from
  graph signals, and each run's outcome is scored through the same bridge that
  feeds `da score iteration`. `da eval gen` reads the global knowledge-graph
  store, so build the graph for the current repository first (`da kg build` /
  `da kg warm`) — see [docs/EVAL_HARNESS.md](docs/EVAL_HARNESS.md).
- **`da run <file>` — recipe scripts.** Execute a line-oriented sequence of `da`
  commands from a file. Each non-blank, non-comment line is tokenized with
  shell-like quoting and dispatched in order, with `$VAR` / `${VAR}` environment
  substitution and no shell invoked (recipes are cross-platform). Runs are
  fail-fast — the first failing step aborts and the error names the step index and
  the original line. A leading shebang line is ignored, so a `chmod +x` recipe
  starting with `#!/usr/bin/env -S da run` is directly executable; recursion is
  depth-bounded.
- **`da review users` — review RBAC administration (admin only).** `add <email>`
  mints a user's bearer token (printed once), `list` shows users with hashed
  secrets only, `remove <email>` revokes a token immediately, and
  `set-role <email>` changes a role while keeping the token. `remove` and
  `set-role` are guarded against locking out the last admin.
- **`da review audit` — review audit-log inspection and maintenance (admin
  only).** A tamper-evident, chained audit log of review actions: `tail` shows
  recent records, `verify` checks the chain and exits non-zero on any integrity
  break, `repair` heals a benign torn-append head anchor, and `prune` compacts
  rotated archives transactionally. Appends are atomic and anchored against
  tampering.
- **`da workflow journal` — session-handoff journal.** An append-only,
  crash-survivable journal for recovering workflow state across sessions:
  `snapshot` captures the deterministic live-state snapshot, `show` displays the
  snapshot plus recent events, `recover` builds a verified recovery view
  (snapshot plus replay, re-verified against reality), `prune` bounds retention,
  and `append` writes a single low-level event.

- **Layered config follow-through (`0.4.2` backfill).** The v2 config model
  gained its operator/portability surfaces: unified config profiles on the
  shared substrate, distributable config manifests, `da init --from` cross-
  machine bootstrap, content-addressed `.agentsrc.lock` units keyed by
  `inputs_digest`, and the `da config sync` / `lint` / `verify` command family.

### Changed

- **Iteration scoring rubric (RubricVersion 3.0.0).** Scores now incorporate a
  `human_label` signal and route recompute through a single unified entry point,
  and `da eval` runs are scored with the same production rubric as workflow
  iterations — so `da score iteration` reflects the new dimensions.

### Fixed

- **`agentslock` contention and Windows reliability.** Lock acquire/release is now
  robust under concurrency and on Windows: the release/reclaim race and a
  transient claim-degrade are fixed, the Windows "delete-pending" and
  concurrent-read races during the lock-file rename no longer block acquisition,
  and release/reclaim are unified into one rename-then-verify primitive. This
  hardens first-run `da install` / `da config` / `da refresh` across platforms.
- **Filesystem mutations on Windows.** `da` now retries the native remove (and
  recursive remove) on transient Windows sharing violations instead of failing the
  operation outright.
- **Hook-bucket errors surfaced on every OS.** A regular file found where a hooks
  directory is expected is now reported on all platforms (it was silently
  swallowed on Windows).
- **`da workflow plan archive` persistence.** The archive move is now committed by
  default (it was previously silently non-persistent), while archive sweeps
  correctly skip the cwd-bound commit.

- **macOS release signing (`0.4.2` backfill).** The 0.4.2 cut repaired Developer
  ID signing by rebuilding the full-chain signing input and added a real-Mac
  verification gate for release signatures/executability so the broken-signature
  path does not silently ship again.

### Internal

- Zero-new-issues Sonar and coverage gate hardening across the release wave, plus
  a heavy cross-OS pre-merge gate tier (`make gate-cross`).
- Foundational build-out behind the CLI for the in-progress workflow service and
  dashboard (event-bus fan-out with terminal overflow semantics, content-
  fingerprint cache invalidation and content-derived ETags, and iteration-log
  ingestion) and for the eval KG-query and scoring-bridge layers. These are not
  yet exposed as a user-facing command.

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

[Unreleased]: https://github.com/AGOrcha/dot-agents/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/AGOrcha/dot-agents/compare/v0.4.2...v0.5.0
[0.3.3]: https://github.com/NikashPrakash/dot-agents/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/NikashPrakash/dot-agents/releases/tag/v0.3.2
[0.1.8]: https://github.com/dot-agents/dot-agents/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/dot-agents/dot-agents/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/dot-agents/dot-agents/compare/v0.1.0...v0.1.7
[0.1.0]: https://github.com/dot-agents/dot-agents/releases/tag/v0.1.0
