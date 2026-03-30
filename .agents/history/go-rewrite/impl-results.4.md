# Backup Process Hardening — impl-results.4

**Plan:** `.agents/history/go-rewrite/backup-process-hardening_c6f3414a.plan.md`

## Summary

All 5 plan items implemented. Build clean, all 9 new tests pass, full test suite green. Backup artifacts no longer appear in `add` output or link health checks; backups are stored centrally in `~/.agents/resources/` with no `*.dot-agents-backup` left in project trees.

## Changes

### 1. `commands/add.go` — Core backup redesign

- **`isBackupArtifact(name string) bool`** — new helper; returns true for any filename containing `.dot-agents-backup`. Single source of truth used by all three callsites below.

- **`scanExistingAIConfigs`** — calls `isBackupArtifact` before adding any discovered file. Backup artifacts never appear in "Other AI Configs Discovered" regardless of where they sit in the project tree.

- **`checkExistingConfigFiles`** — added `isBackupArtifact` guard at the top of the candidate loop. Any file whose basename is a backup artifact is silently skipped, preventing cascading re-backup on repeated `add` runs and stopping the function from treating legacy artifacts as live configs.

- **`backupExistingConfigsList`** — changed from rename-then-mirror to **copy-then-delete**:
  - Calls `mirrorBackup` with the original file path before deletion.
  - Calls `os.Remove` to delete the original from the project tree.
  - Never creates `F.dot-agents-backup` in the project.
  - Skips any input that `isBackupArtifact` identifies as already a backup.

- **`mirrorBackup`** — receives the original file (not a renamed backup); stores it under the **original relative path** in both:
  - `~/.agents/resources/<project>/<relPath>` (active/latest copy, overwritten on re-add)
  - `~/.agents/resources/<project>/backups/<timestamp>/<relPath>` (immutable timestamped snapshot)
  - No `.dot-agents-backup` suffix appears anywhere in the resources tree.

- **Preview message** — updated from `"Backups will be created as *.dot-agents-backup"` to `"Backups stored in ~/.agents/resources/<project>/backups/<timestamp>/"`.

### 2. `src/lib/commands/add.sh` — Bash parity

- **Backup loop** — added `[[ "$(basename "$file")" == *.dot-agents-backup ]] && continue` guard; changed `mv "$file" "$backup_file"` to `rm "$file"` (copy to resources first via `mirror_project_backup_to_resources`, then delete original).

- **`mirror_project_backup_to_resources`** — removed the `.dot-agents-backup`-suffixed copy entirely; now stores only:
  - Canonical active copy at `~/.agents/resources/<slug>/<relPath>`
  - Canonical timestamped copy at `~/.agents/resources/<slug>/backups/<timestamp>/<relPath>`

- **`restore_project_from_active_resources`** — kept legacy `.dot-agents-backup` handling for pre-existing resources (skip if canonical present, strip suffix and use canonical otherwise), with a comment marking it as a compat path for old backups. New restores use canonical names only.

- **Preview message** — updated to match Go: `"Backups stored in ~/.agents/resources/$project_name/backups/<timestamp>/"`.

### 3. `commands/add_test.go` — New test file (9 tests)

| Test | What it proves |
|------|---------------|
| `TestIsBackupArtifact` | Helper correctly identifies backup artifacts by name pattern |
| `TestCheckExistingConfigFiles_SkipsBackupArtifacts` | Artifacts in the project tree are not returned as files to back up |
| `TestCheckExistingConfigFiles_SkipsAlreadyManagedSymlinks` | Links already pointing into `~/.agents` are excluded |
| `TestCheckExistingConfigFiles_IncludesUnmanagedFile` | Regular unmanaged files are correctly included |
| `TestScanExistingAIConfigs_ExcludesBackupArtifacts` | Scan output is clean even when `.dot-agents-backup` files exist in watched dirs |
| `TestBackupExistingConfigsList_CopyDeleteNoArtifactInProject` | Core invariant: original deleted, no `*.dot-agents-backup` in tree, both resource copies exist |
| `TestBackupExistingConfigsList_SkipsBackupArtifacts` | Artifact inputs are a no-op (count=0, no resources created) |
| `TestBackupExistingConfigsList_RemovesSymlinkNoBackup` | Unmanaged symlinks are removed without producing a resources entry |
| `TestCheckExistingConfigFiles_IdempotentAfterAdd` | Post-add state (all links) produces nothing to back up on a second `add` run |
