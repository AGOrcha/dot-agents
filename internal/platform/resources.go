package platform

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/links"
)

type resourceDir struct {
	Name string
	Dir  string
	File string
}

// removeHardlinkedManaged is the error-propagating adapter every RemoveLinks
// path uses for links.RemoveIfHardlinkedToAny. The matched/not-matched bool
// is informational for removal; only the error matters here — a non-nil err
// means a still-present managed hard link could NOT be removed, which must
// surface so `da remove` / doctor do not report success while an active
// managed file is left behind. Discarding the (bool,err) pair (the prior
// behavior at 11 call sites) silently hid that failure.
func removeHardlinkedManaged(path string, sources []string) error {
	_, err := links.RemoveIfHardlinkedToAny(path, sources)
	return err
}

func scopedNames(project string) []string {
	return []string{project, "global"}
}

func resolveScopedFile(agentsHome, bucket, project string, names ...string) string {
	for _, scope := range scopedNames(project) {
		for _, name := range names {
			src := filepath.Join(agentsHome, bucket, scope, name)
			if _, err := os.Stat(src); err == nil {
				return src
			}
		}
	}
	return ""
}

// scopedAgentFileSources returns every canonical AGENT.md path a repo-local
// shared agent file (`<name><destSuffix>`) could have been linked from, across
// the project and global scopes. createAgentsLinks materializes these as a
// POSIX symlink or, on Windows, a hard link with no reparse point — so
// RemoveIfSymlinkUnder alone cannot clean the Windows case. Callers pair this
// with links.RemoveIfHardlinkedToAny, mirroring removeTopLevelLinks.
//
// The flat `<scope>/<name><destSuffix>` form is also included so simplified
// test fixtures and any legacy non-manifest layout are cleaned too.
func scopedAgentFileSources(agentsHome, project, name, destSuffix string) []string {
	var srcs []string
	for _, scope := range scopedNames(project) {
		srcs = append(srcs,
			filepath.Join(agentsHome, "agents", scope, name, agentManifestName),
			filepath.Join(agentsHome, "agents", scope, name+destSuffix),
		)
	}
	return srcs
}

// scopedBucketFileSources returns every canonical path under a single bucket a
// repo-local managed file named `name` could have been linked from, across the
// project and global scopes. Used for buckets whose canonical layout is a flat
// file (e.g. hooks), as the hard-link companion to RemoveIfSymlinkUnder.
func scopedBucketFileSources(agentsHome, bucket, project, name string) []string {
	var srcs []string
	for _, scope := range scopedNames(project) {
		srcs = append(srcs, filepath.Join(agentsHome, bucket, scope, name))
	}
	return srcs
}

func resolveScopedFileFromBuckets(agentsHome string, buckets []string, project string, names ...string) string {
	for _, scope := range scopedNames(project) {
		for _, bucket := range buckets {
			for _, name := range names {
				src := filepath.Join(agentsHome, bucket, scope, name)
				if _, err := os.Stat(src); err == nil {
					return src
				}
			}
		}
	}
	return ""
}

func listScopedResourceDirs(agentsHome, bucket, scope, marker string) ([]resourceDir, error) {
	root := filepath.Join(agentsHome, bucket, scope)
	entries, err := os.ReadDir(root)
	if err != nil {
		// Distinguish "bucket absent" (legitimate no-op, callers swallow
		// via errors.Is(fs.ErrNotExist)) from "bucket exists but is not a
		// listable directory" (a regular file masquerading as the bucket,
		// EACCES, EIO — real corruption that must surface). This must be
		// OS-independent: Windows os.ReadDir on a regular-file path maps
		// to a NotExist-class error, which would otherwise silently hide
		// the corruption. The %v (not %w) deliberately breaks the
		// fs.ErrNotExist chain so the fault propagates on every OS.
		if _, statErr := os.Lstat(root); statErr == nil {
			return nil, fmt.Errorf("resource bucket %s exists but is not a listable directory (%v)", root, err)
		}
		return nil, err
	}

	out := []resourceDir{}
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if !links.IsDirEntry(dir) {
			continue
		}
		markerPath := filepath.Join(dir, marker)
		if _, err := os.Stat(markerPath); err != nil {
			continue
		}
		out = append(out, resourceDir{
			Name: e.Name(),
			Dir:  dir,
			File: markerPath,
		})
	}
	return out, nil
}

// syncScopedDirSymlinks creates per-entry symlinks under dstRoot for every
// directory under <agentsHome>/<bucket>/<scope>/ that owns the marker file.
// A missing bucket (ENOENT) is a legitimate no-op — there is simply nothing
// to sync. Other errors (ENOTDIR, EACCES, EIO) propagate.
func syncScopedDirSymlinks(io platformIO, agentsHome, bucket, scope, marker, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, bucket, scope, marker)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return syncResourceDirEntries(io, entries, dstRoot)
}

// syncScopedDirSymlinksTargets mirrors syncScopedDirSymlinks for multiple
// destination roots. ENOENT on the source bucket is a no-op; other errors
// propagate.
func syncScopedDirSymlinksTargets(io platformIO, agentsHome, bucket, scope, marker string, dstRoots ...string) error {
	entries, err := listScopedResourceDirs(agentsHome, bucket, scope, marker)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, dstRoot := range dstRoots {
		if err := syncResourceDirEntries(io, entries, dstRoot); err != nil {
			return err
		}
	}
	return nil
}

func syncResourceDirEntries(io platformIO, entries []resourceDir, dstRoot string) error {
	if err := io.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		// Managed-replace: per-entry resource-dir mirror symlinks re-emitted
		// every refresh under a dot-agents-managed dstRoot. Stale managed
		// symlink → idempotent re-point; a genuine user entry → preserved as
		// <name>.dot-agents-backup.
		if err := links.SymlinkReplacing(entry.Dir, filepath.Join(dstRoot, entry.Name), backupSidecar); err != nil {
			return err
		}
	}
	return nil
}

// syncScopedFileSymlinks creates per-entry file symlinks (with suffix) under
// dstRoot for every entry under <agentsHome>/<bucket>/<scope>/. ENOENT on the
// bucket is a no-op; other errors propagate.
func syncScopedFileSymlinks(io platformIO, agentsHome, bucket, scope, marker, dstRoot, suffix string) error {
	entries, err := listScopedResourceDirs(agentsHome, bucket, scope, marker)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := io.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		// Managed-replace: per-entry scoped file symlinks re-emitted every
		// refresh under a dot-agents-managed dstRoot (same rationale as
		// syncResourceDirEntries).
		if err := links.SymlinkReplacing(entry.File, filepath.Join(dstRoot, entry.Name+suffix), backupSidecar); err != nil {
			return err
		}
	}
	return nil
}

func readFrontmatter(mdPath string) map[string]string {
	f, err := os.Open(mdPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	inFrontmatter := false
	out := map[string]string{}
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		if lineNum == 1 {
			if strings.TrimSpace(line) != "---" {
				return out
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			return out
		}
		if strings.TrimSpace(line) == "---" {
			return out
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return out
}
