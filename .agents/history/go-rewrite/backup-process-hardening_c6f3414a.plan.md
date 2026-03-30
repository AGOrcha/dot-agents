---
name: backup-process-hardening
overview: Redesign and harden the project backup flow in dot-agents so that backups are robust, idempotent, and don’t pollute add/status output with existing *.dot-agents-backup files.
todos:
  - id: analyze-current-backup
    content: Document current Go and bash backup behavior (rename vs copy, resources layout, scan interactions).
    status: completed
  - id: design-backup-normalization
    content: "Specify exact new backup semantics: copy+delete, skip *.dot-agents-backup, centralize in ~/.agents/resources with timestamps."
    status: completed
  - id: update-go-backup-logic
    content: Change commands/add.go backupExistingConfigsList, mirrorBackup, scanExistingAIConfigs and messages to match new design.
    status: completed
  - id: optional-bash-alignment
    content: Align bash add.sh and resource-restore-map.sh semantics with the new Go backup design if bash is still supported.
    status: completed
  - id: add-tests-and-verify
    content: Add or extend tests for backup behavior and run end-to-end adds to ensure no *.dot-agents-backup noise and idempotence.
    status: completed
isProject: false
---

# Backup Process Hardening

## Goals

- **Stop treating existing `*.dot-agents-backup` files as live configs** during `add` (and related commands).
- **Avoid multiplying backup suffixes** (e.g. `rules.mdc.dot-agents-backup.dot-agents-backup`).
- **Keep centralized, timestamped backups in `~/.agents/resources/<project>/...`** as the source of truth.
- **Make repeated `add` / `refresh` runs idempotent and noise-free** in both CLI output and repo contents.

## Current behavior (Go side)

- `commands/add.go`:
  - Computes `existingFiles` via `checkExistingConfigFiles` (root-level `.mcp.json`, `AGENTS.md`, `opencode.json`, `.github/copilot-instructions.md`).
  - Shows **“Files to Replace”** and the note: *“Backups will be created as *.dot-agents-backup”*.
  - Calls `backupExistingConfigsList(existingFiles, projectPath, agentsHome, projectName, timestamp)`.
- `backupExistingConfigsList` (in `commands/add.go`):
  - For **symlinks**: deletes them in-place (no backup file left in repo).
  - For **regular files**: renames `F` → `F.dot-agents-backup` inside the project, then calls `mirrorBackup`.
- `mirrorBackup`:
  - Copies that renamed file into `~/.agents/resources/<project>/<relPath>` and also into a timestamped backup tree under `~/.agents/resources/<project>/backups/<timestamp>/<relPath>`.
- `scanExistingAIConfigs` (also in `add.go`):
  - Walks `.cursor/rules`, `.claude/rules`, etc. and **does not skip `*.dot-agents-backup`**, so they show up under **“Other AI Configs Discovered”**.

## Proposed design

### 1. Treat `*.dot-agents-backup` as *backup artifacts*, not live configs

- **Never back up a file that already ends with `.dot-agents-backup`.**
  - When computing `existingFiles` (root-level configs), explicitly skip any path with the suffix `.dot-agents-backup`.
  - In any future expansion of `existingFiles` (if more paths are added), keep the same rule.
- **Filter backups from scans and UX:**
  - In `scanExistingAIConfigs`, skip any file whose basename ends with `.dot-agents-backup` so they don’t appear in **“Other AI Configs Discovered”**.
  - Keep the `doctor` / `status` behavior we already added where `*.dot-agents-backup` are ignored for link health.

### 2. Stop leaving backup files in the project tree for new runs

Redesign `backupExistingConfigsList` for **new backups**:

- For a regular file `F` to be replaced:
  - **Copy** `F` into `~/.agents/resources/<project>/<relPath>` and `~/.agents/resources/<project>/backups/<timestamp>/<relPath>`.
  - **Delete** the original `F` from the project (it will soon be replaced by a link).
  - **Do NOT create `F.dot-agents-backup` inside the repo** for new backups.
- For symlinks, keep the current behavior (just remove them) since they are already indirect references.
- Update the user-facing message from *“Backups will be created as *.dot-agents-backup”* to something like:
  - *“Backups will be stored under ~/.agents/resources/**/backups/**/”*.

### 3. Handle legacy `*.dot-agents-backup` already in repos

- **Detection:**
  - When scanning for AI configs (`scanExistingAIConfigs`), filter them out as above.
  - When computing link health (`doctor`, `status --audit`), we already ignore them; keep that behavior.
- **Optional light migration (Go-only):**
  - When `backupExistingConfigsList` runs and encounters a file already ending with `.dot-agents-backup`, **do not rename or re-backup it**.
  - Optionally, add a small helper used by `doctor`/`add` that:
    - Detects `.dot-agents-backup` files in known locations.
    - Ensures they’re mirrored into `~/.agents/resources/<project>/backups/<timestamp>/...` if not already present, then (optionally) offers a one-time cleanup path in `doctor` (out of scope unless you want migration UX).

### 4. Keep `~/.agents/resources` as the canonical backup store

- Preserve `mirrorBackup` semantics (active copy + timestamped copy), but adapt it to work with the **original filename** rather than the in-project `*.dot-agents-backup` name:
  - Pass the original `F` path and the desired `relPath` into `mirrorBackup`, instead of deriving from the renamed `F.dot-agents-backup`.
  - Make sure mappings in `mapResourceRelToDest` / `dot_agents_map_resource_rel_to_agents_dest` are still correct for existing resource layouts.
- Confirm that **restore** flows (if any) depend only on the `resources/<project>` tree, not on `.dot-agents-backup` in the project.

### 5. Idempotence and repeated `add` runs

- With the new design:
  - Running `dot-agents add` again on the same project should:
    - See no root-level configs to back up (they are now links).
    - See no `.dot-agents-backup` as candidates (they are filtered).
    - Skip the backup step entirely and go straight to refreshing links / config.
- To enforce this:
  - Ensure `checkExistingConfigFiles` only considers files that are neither symlinks into `~/.agents` nor `*.dot-agents-backup`.
  - Consider adding a small helper like `isManagedByDotAgents(path)` that recognizes the link target pattern and avoids trying to “back up” files that are already managed.

### 6. Bash parity / compatibility

If you want bash and Go implementations to stay functionally equivalent for a while:

- **Bash add script** (`src/lib/commands/add.sh`):
  - Apply analogous changes to its backup logic (where it renames to `.dot-agents-backup`, if present).
  - Update its help text and any echoed messages about where backups live.
- **resource-restore-map.sh**:
  - Review patterns that reference `.dot-agents-backup` (e.g. Codex cases) and ensure they still work with the new layout.
  - If we stop creating new `*.dot-agents-backup` in `resources`, keep support only for legacy backups and prefer the newer, clean names for anything Go writes.

## Implementation outline (once plan is approved)

- `**commands/add.go`**
  - Update `backupExistingConfigsList` to copy+delete instead of rename+mirror, and skip `*.dot-agents-backup`.
  - Update `mirrorBackup` to work with original names.
  - Teach `scanExistingAIConfigs` to ignore `*.dot-agents-backup`.
  - Update user-facing text about where backups live.
- `**internal/config` / helpers**
  - (Optional) Add helper(s) for detecting already-managed links, reused by `checkExistingConfigFiles` to avoid backing up managed files.
- **Bash scripts** (if needed)
  - Mirror the above semantics in `add.sh` and `resource-restore-map.sh`.
- **Verification**
  - Add Go tests around `backupExistingConfigsList` and `scanExistingAIConfigs` to confirm:
    - No `.dot-agents-backup` are left in a temp project tree after backup.
    - Legacy `.dot-agents-backup` are ignored by scans.
    - Re-running `runAdd` with the same project is a no-op for backups and doesn’t spam “Other AI Configs Discovered”.

