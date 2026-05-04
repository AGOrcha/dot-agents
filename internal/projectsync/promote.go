package projectsync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
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

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return fmt.Errorf("loading .agentsrc.json for %s %q: %w", spec.Singular, name, err)
	}
	projectName := rc.Project
	if projectName == "" {
		return fmt.Errorf(".agentsrc.json has no project name set: run `dot-agents install --generate` or `dot-agents add .` to repair the manifest")
	}

	agentsHome := config.AgentsHome()
	destDir := filepath.Join(agentsHome, spec.Bucket, projectName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating %s directory for %q: %w", spec.Bucket, name, err)
	}
	canonicalPath := filepath.Join(destDir, name)

	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		existing, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("reading existing symlink for %s %q: %w", spec.Singular, name, err)
		}
		if existing != canonicalPath {
			return fmt.Errorf("%s %q is already a symlink but points to %q, not the canonical path %q; fix the link or remove it before promoting", spec.Singular, name, existing, canonicalPath)
		}
	} else {
		if _, err := os.Stat(filepath.Join(sourcePath, spec.ManifestName)); err != nil {
			return fmt.Errorf("%s %q not found in .agents/%s/ (expected %s at %s/%s)", spec.Singular, name, spec.Bucket, spec.ManifestName, sourcePath, spec.ManifestName)
		}
		if fi, err := os.Lstat(canonicalPath); err == nil {
			switch {
			case fi.Mode()&os.ModeSymlink != 0:
				if err := os.Remove(canonicalPath); err != nil {
					return fmt.Errorf("removing stale canonical symlink for %s %q: %w", spec.Singular, name, err)
				}
			case fi.IsDir():
				if !spec.Force {
					return fmt.Errorf("%s %q already exists at canonical path %s as a real directory%s", spec.Singular, name, canonicalPath, spec.ExistingRealDirHint)
				}
				if err := os.RemoveAll(canonicalPath); err != nil {
					return fmt.Errorf("removing existing canonical directory for %s %q: %w", spec.Singular, name, err)
				}
			default:
				return fmt.Errorf("%s %q already exists at canonical path %s; remove the file and retry", spec.Singular, name, canonicalPath)
			}
		}
		if err := CopyTree(sourcePath, canonicalPath); err != nil {
			return fmt.Errorf("copying %s %q to canonical path: %w", spec.Singular, name, err)
		}
		if err := os.RemoveAll(sourcePath); err != nil {
			return fmt.Errorf("removing repo-local %s directory for %q: %w", spec.Singular, name, err)
		}
		if err := os.Symlink(canonicalPath, sourcePath); err != nil {
			return fmt.Errorf("creating repo-local managed symlink for %s %q: %w", spec.Singular, name, err)
		}
	}

	count := spec.RegisterInRC(rc, name)
	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("updating .agentsrc.json for %s %q: %w", spec.Singular, name, err)
	}

	if spec.MirrorRefresh != nil {
		_ = spec.MirrorRefresh(projectName, projectPath)
	}

	ui.SuccessBox(
		fmt.Sprintf("Promoted %s '%s' for project '%s'", spec.Singular, name, projectName),
		fmt.Sprintf("Registered in .agentsrc.json (%d %s(s) total)", count, spec.Singular),
		"Run 'dot-agents refresh' to sync across all platforms",
	)
	return nil
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
