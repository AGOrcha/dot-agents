package platform

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
)

type resourceDir struct {
	Name string
	Dir  string
	File string
}

// ErrReservedSourceID reports a packages ref whose source id collides with a
// local scope segment (package-artifact-install spec §3A H3): the universal
// "global" scope or a caller-supplied local project scope name. errors.Is
// lets a caller distinguish this from any other materialize failure.
var ErrReservedSourceID = errors.New("platform: source id collides with a reserved local scope")

// reservedLocalScopes are the scope segments a packages source id may never
// equal (H3): the universal "global" scope shared by every project.
var reservedLocalScopes = []string{"global"}

// ValidateResolvedUnitIdentity enforces the H15 identity-component
// containment contract for a resolved packages unit's on-disk identity:
// family, source-id, and name must EACH be a single canonical path segment
// (config.ValidateStoreSegment — rejects "."/".."/separators/absolute/
// volume/NUL), and the source id must not collide with a reserved local
// scope (H3 — "global" or any caller-supplied local scope, typically the
// consuming project's own scope name). It is the single gate every
// materialize/projection entry point calls BEFORE deriving any filesystem
// path from caller-influenced identity, so a hostile ".." source id can
// never widen a CAS or projection path.
func ValidateResolvedUnitIdentity(family, sourceID, name string, localScopes ...string) error {
	if err := config.ValidateStoreSegment(family); err != nil {
		return fmt.Errorf("platform: family: %w", err)
	}
	if err := config.ValidateStoreSegment(sourceID); err != nil {
		return fmt.Errorf("platform: source id: %w", err)
	}
	if err := config.ValidateStoreSegment(name); err != nil {
		return fmt.Errorf("platform: resource name: %w", err)
	}
	return validateSourceIDScope(sourceID, localScopes...)
}

// validateSourceIDScope rejects a source id equal to a reserved local scope
// (H3): "global", or any of the caller-supplied local scope names (the
// consuming project's own scope). Distinct source ids never collide with
// each other — each project links its own resolved digest directly, so
// there is no shared cross-source alias to contend over (H13).
func validateSourceIDScope(sourceID string, localScopes ...string) error {
	for _, scope := range reservedLocalScopes {
		if sourceID == scope {
			return fmt.Errorf("%w: %q", ErrReservedSourceID, sourceID)
		}
	}
	for _, scope := range localScopes {
		if scope != "" && sourceID == scope {
			return fmt.Errorf("%w: %q", ErrReservedSourceID, sourceID)
		}
	}
	return nil
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

// resolveScopedFile returns the first candidate under
// <agentsHome>/<bucket>/<scope>/<name> (scanning project scope then global)
// that os.Stat confirms exists, or "" if none of the candidates exist. A
// candidate that is genuinely absent (os.IsNotExist) is a silent skip — that
// is the expected steady state for most (bucket, scope, name) combinations.
// A real Stat failure (permission-denied, I/O error) is NOT the same as
// absent: we cannot prove the candidate doesn't exist, so it is logged
// rather than silently treated as "keep looking". The return value stays ""
// either way — callers across platform files rely on that contract — but
// the log line makes a masked real error distinguishable from legitimate
// absence.
func resolveScopedFile(agentsHome, bucket, project string, names ...string) string {
	for _, scope := range scopedNames(project) {
		for _, name := range names {
			src := filepath.Join(agentsHome, bucket, scope, name)
			if _, err := os.Stat(src); err == nil {
				return src
			} else if !os.IsNotExist(err) {
				slog.Default().Warn("platform: stat failed while resolving scoped file, skipping candidate",
					"path", src, "error", err)
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

// resolveScopedFileFromBuckets is resolveScopedFile's cross-bucket sibling:
// same candidate-loop/skip-on-absence contract, same real-error visibility.
func resolveScopedFileFromBuckets(agentsHome string, buckets []string, project string, names ...string) string {
	for _, scope := range scopedNames(project) {
		for _, bucket := range buckets {
			for _, name := range names {
				src := filepath.Join(agentsHome, bucket, scope, name)
				if _, err := os.Stat(src); err == nil {
					return src
				} else if !os.IsNotExist(err) {
					slog.Default().Warn("platform: stat failed while resolving scoped file across buckets, skipping candidate",
						"path", src, "error", err)
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
