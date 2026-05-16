package projectsync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// PromoteSpec parameterizes PromoteResource for the agents/skills buckets.
// Add a new bucket by populating BucketSpec, wiring RegisterInRC and
// MirrorRefresh, and choosing whether the CLI exposes a force flag.
type PromoteSpec struct {
	BucketSpec
	// Force overwrites an existing real canonical directory at
	// ~/.agents/<bucket>/<project>/<name>/. When false, an existing real dir
	// is fatal and the returned error appends ExistingRealDirHint as a
	// recovery hint.
	Force bool
	// ExistingRealDirHint is the trailing recovery hint shown when canonical
	// is a real directory and Force is false. Pass "; use --force to overwrite"
	// for buckets that expose a force flag, or "; cannot promote" otherwise.
	ExistingRealDirHint string
	// RegisterInRC appends name to the bucket-specific slice in
	// .agentsrc.json (e.g. rc.Agents or rc.Skills) and returns the new slice
	// length, used in the SuccessBox count line.
	RegisterInRC func(rc *config.AgentsRC, name string) int
	// MirrorRefresh refreshes platform-level mirrors for the bucket. Errors
	// must be surfaced via ui.Bullet by the callback itself; PromoteResource
	// ignores the returned error so a failed mirror refresh does not roll
	// back the manifest update (matching pre-extraction behavior).
	MirrorRefresh func(projectName, projectPath string) error
}

// Test seams: replaced in tests to drive the symlink/rollback failure paths
// in materializePromoteSource. Production code never overrides these.
var (
	osSymlink = os.Symlink
	osRename  = os.Rename
)

// PromoteResource promotes a repo-local resource (.agents/<bucket>/<name>/)
// into the shared agents store at ~/.agents/<bucket>/<project>/<name>/. The
// canonical location becomes the real directory and the repo-local path is
// replaced with a managed symlink pointing at it. Idempotent when the
// repo-local path is already a managed symlink to the canonical path.
func PromoteResource(name, projectPath string, spec PromoteSpec) error {
	sourcePath := filepath.Join(projectPath, ".agents", spec.Bucket, name)
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("%s %q not found in .agents/%s/: %w", spec.Singular, name, spec.Bucket, err)
	}

	rc, projectName, err := loadPromoteRC(projectPath, name, spec)
	if err != nil {
		return err
	}

	canonicalPath, err := preparePromoteDest(name, projectName, spec)
	if err != nil {
		return err
	}

	journalPath, jerr := BeginPromoteJournal(config.AgentsHome(), PromoteJournalEntry{
		Singular:      spec.Singular,
		Bucket:        spec.Bucket,
		Name:          name,
		SourcePath:    sourcePath,
		CanonicalPath: canonicalPath,
	})
	if jerr != nil {
		// Journal failures are non-fatal: a missing journal degrades recovery
		// quality but does not block a working promote. We surface a warning
		// via ui.Bullet and continue.
		ui.Bullet("warn", fmt.Sprintf("could not open promote journal: %v", jerr))
		journalPath = ""
	}

	if err := materializePromoteSource(sourcePath, canonicalPath, sourceInfo, name, spec, journalPath); err != nil {
		if journalPath != "" {
			_ = AdvancePromoteJournal(journalPath, PromoteStateRolledBack)
			_ = RemovePromoteJournal(journalPath)
		}
		return err
	}

	count := spec.RegisterInRC(rc, name)
	if err := rc.Save(projectPath); err != nil {
		if journalPath != "" {
			_ = AdvancePromoteJournal(journalPath, PromoteStateRolledBack)
			_ = RemovePromoteJournal(journalPath)
		}
		return fmt.Errorf("updating .agentsrc.json for %s %q: %w", spec.Singular, name, err)
	}
	if journalPath != "" {
		_ = AdvancePromoteJournal(journalPath, PromoteStateRCSaved)
		_ = RemovePromoteJournal(journalPath)
	}

	if spec.MirrorRefresh != nil {
		_ = spec.MirrorRefresh(projectName, projectPath)
	}

	ui.SuccessBox(
		fmt.Sprintf("Promoted %s '%s' for project '%s'", spec.Singular, name, projectName),
		fmt.Sprintf("Registered in .agentsrc.json (%d %s(s) total)", count, spec.Singular),
		"Run 'da refresh' to sync across all platforms",
	)
	return nil
}

// loadPromoteRC loads the project AgentsRC and validates that a project name
// is set, returning a typed error for the common "missing project" case.
func loadPromoteRC(projectPath, name string, spec PromoteSpec) (*config.AgentsRC, string, error) {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return nil, "", fmt.Errorf("loading .agentsrc.json for %s %q: %w", spec.Singular, name, err)
	}
	if rc.Project == "" {
		return nil, "", fmt.Errorf(".agentsrc.json has no project name set: run `da install --generate` or `da add .` to repair the manifest")
	}
	return rc, rc.Project, nil
}

// preparePromoteDest ensures the bucket directory under ~/.agents/<bucket>/<project>/
// exists and returns the canonical path for the named resource.
func preparePromoteDest(name, projectName string, spec PromoteSpec) (string, error) {
	destDir := filepath.Join(config.AgentsHome(), spec.Bucket, projectName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("creating %s directory for %q: %w", spec.Bucket, name, err)
	}
	return filepath.Join(destDir, name), nil
}

// materializePromoteSource handles the symlink-vs-real-dir branching for the
// repo-local source path: an existing managed symlink is validated, while a
// real directory is copied to canonical and replaced with a symlink.
//
// journalPath, when non-empty, is advanced after each destructive transition
// so a SIGKILL leaves a recoverable breadcrumb under ~/.agents/.promote-journal/.
// Journal advance failures are non-fatal — they are silently ignored because a
// missing journal only degrades downstream recovery quality, never the promote
// itself.
func materializePromoteSource(sourcePath, canonicalPath string, sourceInfo os.FileInfo, name string, spec PromoteSpec, journalPath string) error {
	// "Already a managed link" is OS-specific: a POSIX symlink, or on Windows
	// a directory junction / hard link (neither of which sets os.ModeSymlink
	// on a hard link, and a junction is only a symlink to recent Go). Route
	// the decision through the links abstraction so an already-promoted
	// repo-local link is recognized as idempotent on every OS instead of
	// being mistaken for a real foreign directory.
	if isManagedSource(sourcePath, sourceInfo) {
		return validatePromoteSymlink(sourcePath, canonicalPath, name, spec)
	}
	if _, err := os.Stat(filepath.Join(sourcePath, spec.ManifestName)); err != nil {
		return fmt.Errorf("%s %q not found in .agents/%s/ (expected %s at %s/%s)", spec.Singular, name, spec.Bucket, spec.ManifestName, sourcePath, spec.ManifestName)
	}
	if err := clearExistingCanonical(canonicalPath, name, spec); err != nil {
		return err
	}
	if err := CopyTree(sourcePath, canonicalPath); err != nil {
		return fmt.Errorf("copying %s %q to canonical path: %w", spec.Singular, name, err)
	}
	if journalPath != "" {
		_ = AdvancePromoteJournal(journalPath, PromoteStateCanonicalCopied)
	}
	if err := os.RemoveAll(sourcePath); err != nil {
		return fmt.Errorf("removing repo-local %s directory for %q: %w", spec.Singular, name, err)
	}
	if journalPath != "" {
		_ = AdvancePromoteJournal(journalPath, PromoteStateSourceRemoved)
	}
	if err := osSymlink(canonicalPath, sourcePath); err != nil {
		// Rollback: restore the repo-local directory from the canonical copy so
		// we never leave the source path missing if symlink creation fails.
		if rerr := osRename(canonicalPath, sourcePath); rerr != nil {
			// Cross-filesystem rename fails with EXDEV (e.g. NFS home + local
			// repo, Docker bind-mount over tmpfs). Fall back to CopyTree + remove
			// so rollback still succeeds across mount boundaries.
			if errors.Is(rerr, syscall.EXDEV) {
				if cerr := CopyTree(canonicalPath, sourcePath); cerr != nil {
					return fmt.Errorf("creating managed symlink failed and rollback failed (canonical=%s, source=%s now missing): symlink=%w; rename=%w; copy=%w",
						canonicalPath, sourcePath, err, rerr, cerr)
				}
				if rmerr := os.RemoveAll(canonicalPath); rmerr != nil {
					// Source restored but canonical still present — partial recovery.
					return fmt.Errorf("creating managed symlink failed; rolled back to repo-local %s %q but canonical still present at %s (manual cleanup needed): symlink=%w; canonical-remove=%w",
						spec.Singular, name, canonicalPath, err, rmerr)
				}
				return fmt.Errorf("creating managed symlink failed; rolled back to repo-local %s %q (via cross-fs copy): %w", spec.Singular, name, err)
			}
			return fmt.Errorf("creating managed symlink failed and rollback also failed; canonical=%s, source=%s now missing: symlink=%w; rollback=%w",
				canonicalPath, sourcePath, err, rerr)
		}
		return fmt.Errorf("creating managed symlink failed; rolled back to repo-local %s %q: %w", spec.Singular, name, err)
	}
	if journalPath != "" {
		_ = AdvancePromoteJournal(journalPath, PromoteStateSymlinked)
	}
	return nil
}

// isManagedSource reports whether the repo-local source path is an
// already-materialized managed link (POSIX symlink, or Windows junction /
// hard link) rather than a real directory tree awaiting promotion. The
// POSIX-symlink fast path preserves the prior behavior exactly; the
// resolvable-target / hard-link branches add Windows correctness, where a
// junction or hard link does not set os.ModeSymlink.
func isManagedSource(sourcePath string, sourceInfo os.FileInfo) bool {
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if _, ok := links.ManagedLinkTarget(sourcePath); ok {
		return true
	}
	// Hard link (no reparse point): identity is a shared inode with the
	// canonical store. Without a known target here we treat any resolvable
	// reparse point as managed (above); a plain real dir falls through to
	// the copy path, and validatePromoteSymlink does the canonical compare.
	return false
}

// validatePromoteSymlink confirms that an existing repo-local managed link
// references the canonical path; a mispointed link and unreadable links
// surface as fatal errors. Detection is OS-aware: a POSIX symlink or Windows
// junction resolves via links.ManagedLinkTarget, while a Windows hard link
// (no reparse point) is recognized by links.IsManagedLink against the known
// canonical target.
func validatePromoteSymlink(sourcePath, canonicalPath, name string, spec PromoteSpec) error {
	if links.IsManagedLink(sourcePath, canonicalPath) {
		return nil // already promoted → idempotent success
	}
	if existing, ok := links.ManagedLinkTarget(sourcePath); ok {
		return fmt.Errorf("%s %q is already a symlink but points to %q, not the canonical path %q; fix the link or remove it before promoting", spec.Singular, name, existing, canonicalPath)
	}
	// Reached only for a managed entry with no resolvable target that is not
	// the canonical file: a dangling link, or (Windows) a hard link to some
	// other file. There is no target to name, so report the mispointing
	// generically. Preserves the prior fatal-on-mismatch contract.
	return fmt.Errorf("%s %q is already a symlink but does not point to the canonical path %q; fix the link or remove it before promoting", spec.Singular, name, canonicalPath)
}

// clearExistingCanonical removes a stale symlink or, when Force is set, a real
// directory at the canonical path. A real file or a real directory without
// Force is fatal.
func clearExistingCanonical(canonicalPath, name string, spec PromoteSpec) error {
	fi, err := os.Lstat(canonicalPath)
	if err != nil {
		return nil
	}
	// A stale managed link occupying the canonical slot is removable on every
	// OS. ModeSymlink covers POSIX symlinks; ManagedLinkTarget additionally
	// recognizes a Windows directory junction (which carries no ModeSymlink
	// bit on older Go and would otherwise be misread as a real directory).
	_, isResolvableLink := links.ManagedLinkTarget(canonicalPath)
	switch {
	case fi.Mode()&os.ModeSymlink != 0 || isResolvableLink:
		if err := os.Remove(canonicalPath); err != nil {
			return fmt.Errorf("removing stale canonical symlink for %s %q: %w", spec.Singular, name, err)
		}
		return nil
	case fi.IsDir():
		if !spec.Force {
			return fmt.Errorf("%s %q already exists at canonical path %s as a real directory%s", spec.Singular, name, canonicalPath, spec.ExistingRealDirHint)
		}
		if err := os.RemoveAll(canonicalPath); err != nil {
			return fmt.Errorf("removing existing canonical directory for %s %q: %w", spec.Singular, name, err)
		}
		return nil
	default:
		return fmt.Errorf("%s %q already exists at canonical path %s; remove the file and retry", spec.Singular, name, canonicalPath)
	}
}

// CopyTree recursively copies the directory tree at src to dst, preserving
// file modes. Symlinks in the source tree are skipped — the canonical store
// holds only real files.
func CopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode())
	})
}
