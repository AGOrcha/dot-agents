# Changelog

All notable changes to dot-agents will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_No entries yet. Feature PRs add their lines here; the next release task
finalizes them into a version section._

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

[Unreleased]: https://github.com/NikashPrakash/dot-agents/compare/v0.3.3...HEAD
[0.3.3]: https://github.com/NikashPrakash/dot-agents/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/NikashPrakash/dot-agents/releases/tag/v0.3.2
[0.1.8]: https://github.com/dot-agents/dot-agents/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/dot-agents/dot-agents/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/dot-agents/dot-agents/compare/v0.1.0...v0.1.7
[0.1.0]: https://github.com/dot-agents/dot-agents/releases/tag/v0.1.0
