# Changelog

All notable changes to dot-agents will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_No entries yet. Feature PRs add their lines here; the next release task
finalizes them into a version section._

## [0.3.3] - 2026-05-28

This patch release organizes by **theme** rather than the Keep-a-Changelog
Added/Changed/Fixed split, because the 0.3.3 surface is dominated by
coordinated infrastructure/distribution efforts (the agorcha.dev docs site,
signing & release hardening, platform-driven diagnostics, reviewer
infrastructure, and config-v2 groundwork) where a single theme cuts across
all three categories. Per the release-gated-plans convention, distribution
and infrastructure work rides the patch train; `0.4.0` is reserved for the
next genuine feature land.

### Docs site (agorcha.dev)

- Interactive Astro + Cytoscape documentation site under `docs/web/`,
  deployed to agorcha.dev (PR #143).
- Cloudflare Worker deploy pipeline (`deploy-docs.yml`) plus a scheduled
  deploy-token auto-rotation workflow (PRs #150, #151).
- Demo → pitch reframing: a 5-section pitch deck with presenter notes
  alongside the existing demo pages (PR #164).
- Usability fixes: fluid graph canvas + persistent legend (PR #163),
  mobile sidebar / heading / theming polish, and a demo-title prefix
  fallback (PRs #159, #166).

### Signing & release

- Cosign keyless signing via sigstore + GitHub OIDC, wired into
  goreleaser (PR #138). Every release artifact + checksum is signed;
  verify with `cosign verify-blob` per `docs/RELEASE_VERIFICATION.md`.
- Homebrew dual-cask emit: unversioned `dot-agents.rb` + versioned
  `dot-agents@{version}.rb` (PR #144).
- `VERSION` file + `auto-release.yml` continue to drive tag/sign/publish:
  bumping `VERSION` on a merge to `master` is the release trigger.
- Native macOS (Apple Developer ID) + Windows (Authenticode) code
  signing remain **deferred** (secrets pending).

### Configuration (config-v2)

- Additive config-v2 schema extension: new fields layered onto
  `.agentsrc.json` without breaking existing configs (PRs #124, #141).
- `da explain` top-level command surfacing config/resolution detail.

  Note: the codex-track "snapshot API + `da config explain` subtree"
  (P4) has **not** shipped in 0.3.3 and is tracked for a later release.

### Platform-driven diagnostics

- `doctor` + `status` now dispatch over the
  `internal/platform.Platform` interface instead of hand-rolled
  per-platform branches (PRs #118, #128, #130, #133, #135). Adds
  `BrokenLinkReporter`, `Badge`, and `CountLinks` across
  cursor / claude / codex / copilot / opencode.
- Multi-OS coverage (macOS + Linux + Windows test matrices).

### Reviewer infrastructure & skills

- 3 starter reviewer-lens agents
  (architecture-standards, acceptance-invariants, adversarial)
  + AGENT.md scaffolding + a lens-count assertion test (PRs #122, #134).
- `da workflow review_gate` staged-dispatch machinery
  (PR #119; codex follow-on split out as PR #120) and a `pr-ci`
  `verifier_profile` default (PR #129).
- `isp.prompt.md` ↔ `verifier_profile` cross-reference enforcement test
  (PR #140) prevents scaffold drift from stranding `verifier_sequence`
  refs.
- Orchestration skills promoted to global starter via `da skills promote`
  (PR #141): `orchestrator-session-start`, `delegation-lifecycle`,
  `plan-wave-picker`, `provider-consumer-pair`, `iteration-close`,
  `isp`, `loop-worker`. `da init` now scaffolds the full chain.

### Workflow tooling

- `da workflow archive-orphans` sweep to reconcile stranded delegation /
  merge-back artifacts (PR #158).
- Hook-sentinel companion ops: expanded `commands/workflow/hook_sentinel`
  + schema (PR #157).
- Evidence-policy schema cleanup on delegation/fanout types (PR #148).
- History-archive location unified (PR #154).

### Renames & refactors

- `cmd/dot-agents/` → `cmd/da/` (PR #139) — Go convention; binary name
  matches install path. Module path stays
  `github.com/NikashPrakash/dot-agents` (project identity preserved).
- `cmdutil` judo refactor: folded `canonical/` into `cmdutil/`; extracted
  shared resource-cmd helpers (PR #115).
- `internal/` package rename + importguard narrowing (PR #117).
- Canonical `internal/gitremote` package: `ParseRemoteURL` +
  `CanonicalRepoID` + `ReadOriginURL` (go-git in-process, no subprocess);
  `repo_id` derivation from git remote (PR #127).
- `internal/testutil.MakeDirWriteDenied` + 9-site migration (PR #128).
- DRY `commands/workflow/contract_core.go`: fanout calls contract core
  (PR #131).

### CI & quality

- Deduplicated push + PR CI pipelines (PR #146).
- Sonar-scanner worktree-path fix (PR #147).
- Coverage lift on global-flag handling (PR #137).

### Design & research (proposals/specs — not shipped capability)

These PRs landed **design artifacts only**: no runtime behavior shipped.
They scope future work and are recorded here for traceability.

- `layered-pr-fanout` spec (PR #149).
- `lens-evidence-policy` spec (PR #152).
- agorcha public/internal split + observability deploy architecture
  proposal (PR #156).
- Monitor PR review/comment routing proposal (PR #160).
- Auto-dream + background-tasks research proposal (PR #161).

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
