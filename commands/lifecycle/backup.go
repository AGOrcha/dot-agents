package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

// HasMultipleHardLinks is the platform-tagged hard-link counter wired
// at init time by commands/add.go (which can import the
// build-constrained linkcount_unix.go / linkcount_windows.go that the
// lifecycle subpackage cannot reach without re-introducing the platform
// tag tree). Returns false by default so the lifecycle package builds
// standalone; production callers always supply the real implementation.
var HasMultipleHardLinks = func(path string) bool { return false }

// RestoreCanonicalResourceFileFn is the canonical-import branch wired at
// init time by commands/import.go (canonicalImportOutputs and its hook
// bundle helpers stay in import.go until t06 moves the whole import
// command). Returns (count, handled, err); handled=false means the file
// is not a canonical resource and the caller should fall back to
// RestoreLegacyResourceFile.
var RestoreCanonicalResourceFileFn = func(project, resourcesDir, agentsHome, path string, deps AddDeps) (int, bool, error) {
	return 0, false, nil
}

// IsBackupArtifact reports whether name is a dot-agents backup
// artifact. Lifted from commands/add.go in
// root-command-decomposition t02b.
func IsBackupArtifact(name string) bool {
	return strings.Contains(name, ".dot-agents-backup")
}

// IsManagedCursorRuleRel reports whether rel points at a managed
// Cursor rule under .cursor/rules/ with the project's or global
// namespace prefix. Internal helper used by IsManagedProjectOutput;
// not part of the t02b 15 but lifted alongside its sole caller to
// keep the backup cluster self-contained.
func IsManagedCursorRuleRel(project, rel string) bool {
	if !strings.HasPrefix(rel, RelCursorRulesDir) {
		return false
	}
	name := filepath.Base(rel)
	return strings.HasPrefix(name, "global--") || strings.HasPrefix(name, project+"--")
}

// IsManagedProjectOutput reports whether filePath is a managed
// dot-agents projection at projectPath under agentsHome for project.
// Lifted from commands/add.go in root-command-decomposition t02b.
func IsManagedProjectOutput(project, projectPath, filePath, agentsHome string) bool {
	if IsManagedSymlink(filePath, agentsHome) {
		return true
	}

	rel, err := filepath.Rel(projectPath, filePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)

	// Managed Cursor rule names live in a reserved namespace and should never
	// be re-imported or backed up as user-authored files.
	if IsManagedCursorRuleRel(project, rel) {
		return true
	}

	destRel := MapResourceRelToDest(project, rel)
	if destRel == "" {
		return false
	}
	linked, err := links.AreHardlinked(filePath, filepath.Join(agentsHome, destRel))
	return err == nil && linked
}

// isManagedHardlinkToCanonicalSource reports whether filePath is hard
// linked to the canonical source under agentsHome that this candidate's
// repo-relative path maps to for project. This is the target-identity
// proof BackupExistingConfigsList needs before dropping a multi-link
// file without a mirror backup: a bare nlink>1 only means "shares an
// inode with something", not "managed by da".
func isManagedHardlinkToCanonicalSource(project, projectPath, filePath, agentsHome string) bool {
	rel, err := filepath.Rel(projectPath, filePath)
	if err != nil {
		return false
	}
	destRel := MapResourceRelToDest(project, filepath.ToSlash(rel))
	if destRel == "" {
		return false
	}
	canonical := filepath.Join(agentsHome, destRel)
	linked, err := links.AreHardlinked(filePath, canonical)
	return err == nil && linked
}

// MirrorBackup copies srcFile (original path, before deletion) into the
// ~/.agents/resources/<project>/ tree using the file's original
// relative path. No *.dot-agents-backup suffix is added anywhere.
//
// This is the errorless wrapper retained for import.go callers, whose
// own failure handling keys off the subsequent CopyFile into the
// destination (a mirror-backup failure there does not destroy the
// user's only copy because import.go never removes the source after
// MirrorBackup). Callers that delete the original after backing it up
// (BackupExistingConfigsList) MUST use MirrorBackupChecked so a failed
// backup aborts before the destructive removal.
func MirrorBackup(project, projectPath, srcFile, timestamp string) {
	_ = MirrorBackupChecked(project, projectPath, srcFile, timestamp, StdAddDeps{})
}

// MirrorBackupChecked performs the same copy as MirrorBackup but
// propagates the CopyFile errors. BackupExistingConfigsList relies on
// this: it removes the user's only copy of an unmanaged config after
// backing it up, so a silent backup failure (unwritable
// ~/.agents/resources, disk full, unreadable source through a symlink)
// would destroy that config while reporting a successful backup.
// Exported (not in the named 15) because add_test.go exercises it
// directly through its var alias in commands/add.go.
func MirrorBackupChecked(project, projectPath, srcFile, timestamp string, deps AddDeps) error {
	agentsHome := config.AgentsHome()
	relPath, err := filepath.Rel(projectPath, srcFile)
	if err != nil || relPath == "." || strings.HasPrefix(relPath, "..") {
		relPath = filepath.Base(srcFile)
	}

	// Active (latest) copy — overwritten on each backup run. This is the
	// recoverable copy `da refresh` / restore reads back, so it is required.
	activeTarget := filepath.Join(agentsHome, "resources", project, relPath)
	if cpErr := deps.CopyFile(srcFile, activeTarget); cpErr != nil {
		return fmt.Errorf("backing up %s -> %s: %w", srcFile, activeTarget, cpErr)
	}

	// Timestamped immutable copy — also required when a timestamp is given:
	// it is the only point-in-time snapshot the user can recover from.
	if timestamp != "" {
		tsTarget := filepath.Join(agentsHome, "resources", project, "backups", timestamp, relPath)
		if cpErr := deps.CopyFile(srcFile, tsTarget); cpErr != nil {
			return fmt.Errorf("backing up %s -> %s: %w", srcFile, tsTarget, cpErr)
		}
	}
	return nil
}

// BackupExistingConfigsList backs up the given files into
// ~/.agents/resources/<project>/... and removes the originals from the
// project tree. No *.dot-agents-backup files are left in the project.
// Returns count of files processed and a non-nil error if any required
// backup copy failed. On backup failure the original is NOT removed
// (the user's only copy is preserved) and the error aborts runAdd
// before any destructive removal. Lifted from commands/add.go in
// root-command-decomposition t02b.
func BackupExistingConfigsList(files []string, projectPath, agentsHome, project, timestamp string, deps AddDeps) (int, error) {
	count := 0
	for _, f := range files {
		// Safety: never back up backup artifacts
		if IsBackupArtifact(filepath.Base(f)) {
			continue
		}
		if _, err := os.Lstat(f); err != nil {
			continue
		}
		// A PROVEN managed link (resolvable POSIX symlink / Windows junction
		// whose target resolves under the canonical agents root) has no
		// standalone content to preserve — remove it without a backup.
		// A merely-resolvable link is NOT proof: a project-owned
		// symlink/junction pointing at a real user file OUTSIDE dot-agents
		// (the symlink twin of the unmanaged-hard-link case below) carries
		// the user's only copy of that config. Dropping it without mirroring
		// the resolved content destroys it while claiming a backup. Such an
		// unmanaged link falls through to the normal mirror/backup path,
		// which copies the resolved bytes before removal.
		if links.IsManagedLinkUnder(f, agentsHome) {
			_ = os.Remove(f)
			count++
			continue
		}
		// A hard link is only safe to drop without a backup when it is PROVEN
		// managed: its inode is shared with the canonical source this candidate
		// maps to under agentsHome. A bare nlink>1 is NOT proof — an
		// UNMANAGED hard-linked AGENTS.md/.mcp.json (e.g. the project hard
		// links its real config elsewhere) also has nlink>1, and dropping it
		// without a mirror backup destroys the project's real config while
		// claiming it was backed up. Unknown/unmanaged hard links fall through
		// to the normal backup/mirror path below.
		if HasMultipleHardLinks(f) && isManagedHardlinkToCanonicalSource(project, projectPath, f, agentsHome) {
			_ = os.Remove(f)
			count++
			continue
		}
		// Regular file: copy into resources, then delete from project.
		// The removal below is destructive — it deletes the user's only
		// copy of an unmanaged config. Only proceed once the required
		// backup copies have actually landed; otherwise abort so runAdd
		// returns an error WITHOUT removing the original.
		if err := MirrorBackupChecked(project, projectPath, f, timestamp, deps); err != nil {
			return count, fmt.Errorf("backing up %s: %w", f, err)
		}
		if err := deps.Remove(f); err != nil {
			continue
		}
		count++
	}
	return count, nil
}

// RestoreFromResourcesCountedWithDeps restores files from
// ~/.agents/resources/<project>/ and returns the number of files
// restored plus a non-nil error if any directory walk, mkdir, write, or
// copy failed. Callers that stamp success (e.g. refresh metadata) MUST
// observe this error: a partially-applied restore that is reported as
// success makes retries and doctor/refresh recovery ambiguous. Lifted
// from commands/add.go in root-command-decomposition t02b.
func RestoreFromResourcesCountedWithDeps(project, projectPath string, deps AddDeps) (int, error) {
	agentsHome := config.AgentsHome()
	resourcesDir := filepath.Join(agentsHome, "resources", project)
	info, err := os.Stat(resourcesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No resources to restore is not a failure.
			return 0, nil
		}
		// A permission-denied / broken-symlink / other non-ENOENT stat
		// error is NOT "nothing to restore": treating it as success makes
		// refresh stamp fresh metadata over backed-up resource data that
		// was never restored. Surface it so refresh.go's projectFailed
		// path fires.
		return 0, fmt.Errorf("stat resources dir %s: %w", resourcesDir, err)
	}
	if !info.IsDir() {
		// A non-directory squatting the resources path cannot be walked;
		// silently skipping it would also mask unrestored data.
		return 0, fmt.Errorf("resources path %s is not a directory", resourcesDir)
	}
	count := 0
	var restoreErr error
	walkErr := filepath.WalkDir(resourcesDir, func(path string, d os.DirEntry, err error) error {
		n, ferr := restoreResourceFileCount(project, resourcesDir, agentsHome, path, d, err, deps)
		count += n
		if ferr != nil && restoreErr == nil {
			restoreErr = ferr
		}
		return nil
	})
	if walkErr != nil && restoreErr == nil {
		restoreErr = fmt.Errorf("walking resources dir %s: %w", resourcesDir, walkErr)
	}
	return count, restoreErr
}

func restoreResourceFileCount(project, resourcesDir, agentsHome, path string, d os.DirEntry, walkErr error, deps AddDeps) (int, error) {
	if walkErr != nil {
		return 0, fmt.Errorf("walking %s: %w", path, walkErr)
	}
	if d.IsDir() {
		return 0, nil
	}
	relPath, err := filepath.Rel(resourcesDir, path)
	if err != nil {
		return 0, fmt.Errorf("resolving relative path for %s: %w", path, err)
	}
	relPath = filepath.ToSlash(relPath)
	if strings.HasPrefix(relPath, "backups/") || IsCanonicalResourceBackupRel(relPath) {
		return 0, nil
	}
	canonicalCount, handled, canonErr := RestoreCanonicalResourceFileFn(project, resourcesDir, agentsHome, path, deps)
	if handled {
		return canonicalCount, canonErr
	}
	return RestoreLegacyResourceFile(project, relPath, agentsHome, path, deps)
}

// IsCanonicalResourceBackupRel reports whether relPath sits under one
// of the canonical resource roots (rules/, settings/, mcp/, skills/,
// agents/, hooks/) and should therefore be ignored when the restore
// loop walks the resources directory (those subtrees are produced by
// canonical-import, not legacy backups). Exported (not in the named
// 15) because add_test.go covers it directly.
func IsCanonicalResourceBackupRel(relPath string) bool {
	for _, prefix := range []string{"rules/", "settings/", "mcp/", "skills/", "agents/", AgentsHooksPrefix} {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

// RestoreLegacyResourceFile restores one non-canonical resource file by
// mapping its repo-relative path to its canonical ~/.agents location
// (via MapResourceRelToDest) and copying via deps.CopyFile. Returns 0
// when the rel path has no known mapping. Lifted from commands/add.go
// in root-command-decomposition t02b.
func RestoreLegacyResourceFile(project, relPath, agentsHome, path string, deps AddDeps) (int, error) {
	destRel := MapResourceRelToDest(project, relPath)
	if destRel == "" {
		return 0, nil
	}
	destPath := filepath.Join(agentsHome, destRel)
	if err := deps.CopyFile(path, destPath); err != nil {
		return 0, fmt.Errorf("restoring %s -> %s: %w", path, destPath, err)
	}
	return 1, nil
}
