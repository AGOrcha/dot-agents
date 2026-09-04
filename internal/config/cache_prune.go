package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// cache_prune.go implements garbage collection for the SHARED config cache
// (~/.agents/cache/config), the one both tiers that ride the Fetcher plumbing
// write into: `extends` config layers and source-qualified prompt files
// (prompt_units.go).
//
// Nothing ever removed a superseded entry. Every resolve writes its content under
// <source-id>/<unit-path>/<digest>/, so a source that moves forward leaves the
// previous digest's entry behind forever — on a busy team source that is the
// dominant contributor to the cache's growth.
//
// The liveness rule is deliberately conservative and purely local: an entry is
// prunable when NO registered project's lockfile references its digest. A lock is
// exactly what an offline resolve reads, so keeping every referenced digest keeps
// every project that could resolve offline able to keep doing so; anything else is
// content no lock can name, which the next sync would re-fetch anyway.

// CacheEntry is one content-addressed entry in the shared config cache: the
// <digest>/ directory holding a resolved layer's or prompt's bytes.
type CacheEntry struct {
	// Path is the absolute entry directory
	// (<root>/<source-id>/<unit-path>/<digest>).
	Path string `json:"path"`
	// SourceID is the declared source the entry was fetched from.
	SourceID string `json:"source_id,omitempty"`
	// UnitPath is the source-relative layer/prompt path.
	UnitPath string `json:"unit_path,omitempty"`
	// Digest is the resolved SHA / content hash the entry is addressed by (the
	// on-disk directory segment, i.e. an OCI "sha256:<hex>" appears as its hex).
	Digest string `json:"digest,omitempty"`
	// Bytes is the total size of the files in the entry.
	Bytes int64 `json:"bytes"`
	// Referenced reports whether some project's lock pins this digest for this
	// source+path.
	Referenced bool `json:"referenced"`
}

// CacheScan is the result of scanning the shared config cache against the
// lockfiles of a set of projects.
type CacheScan struct {
	// Root is the scanned cache root.
	Root string `json:"root"`
	// Entries are every discovered entry, sorted by path.
	Entries []CacheEntry `json:"entries"`
	// Projects are the project paths whose locks contributed references.
	Projects []string `json:"projects"`
	// Skipped are project paths that pinned nothing because they carry no
	// lockfile (never resolved, or a stale registry row). They are reported so a
	// prune is never silently based on an incomplete reference set.
	Skipped []string `json:"skipped,omitempty"`
}

// Prunable returns the entries no project lock references, in scan order.
func (s CacheScan) Prunable() []CacheEntry {
	out := make([]CacheEntry, 0, len(s.Entries))
	for _, e := range s.Entries {
		if !e.Referenced {
			out = append(out, e)
		}
	}
	return out
}

// PrunableBytes is the total size of the prunable entries.
func (s CacheScan) PrunableBytes() int64 {
	var total int64
	for _, e := range s.Prunable() {
		total += e.Bytes
	}
	return total
}

// ScanConfigCache walks the shared config cache and marks each entry against the
// digests the given projects' lockfiles reference. It is READ-ONLY.
//
// A project whose lock cannot be parsed is an ERROR, not a skip: pruning on a
// partial reference set could delete content a valid lock still pins, so the scan
// fails closed and the operator fixes (or unregisters) the project first.
func ScanConfigCache(projectPaths []string) (CacheScan, error) {
	root := configCacheRoot()
	scan := CacheScan{Root: root, Entries: []CacheEntry{}, Projects: []string{}}
	referenced := map[string]struct{}{}
	for _, project := range dedupeSorted(projectPaths) {
		pinned, ok, err := referencedCacheDirs(project)
		if err != nil {
			return CacheScan{}, err
		}
		if !ok {
			scan.Skipped = append(scan.Skipped, project)
			continue
		}
		scan.Projects = append(scan.Projects, project)
		for dir := range pinned {
			referenced[dir] = struct{}{}
		}
	}
	entries, err := walkCacheEntries(root)
	if err != nil {
		return CacheScan{}, err
	}
	for i := range entries {
		_, entries[i].Referenced = referenced[entries[i].Path]
	}
	scan.Entries = entries
	return scan, nil
}

// referencedCacheDirs returns the set of absolute cache entry directories the
// project's lock pins. ok is false when the project has no lockfile at all (it
// pins nothing and is reported as skipped rather than silently contributing an
// empty reference set).
//
// Only `layer` and `prompt` units are considered: they are the two kinds whose
// bytes live in this cache. An `artifact` unit is installed from the packages
// cache and a `profile` unit is derived from config, so neither addresses an
// entry here.
func referencedCacheDirs(projectPath string) (map[string]struct{}, bool, error) {
	if _, err := os.Stat(AgentsLockPath(projectPath)); err != nil {
		return nil, false, nil
	}
	lock, err := ReadUnits(projectPath)
	if err != nil {
		return nil, false, err
	}
	dirs := map[string]struct{}{}
	for key, unit := range lock.Units {
		if unit.Kind != UnitKindLayer && unit.Kind != UnitKindPrompt {
			continue
		}
		if unit.Digest == "" {
			continue
		}
		parts, err := ParseLayerRef(key)
		if err != nil {
			// A key that is not a source ref cannot address a cache entry; the
			// verify surfaces report it, and skipping it here cannot make a real
			// entry look unreferenced.
			continue
		}
		dir := filepath.Join(layerCacheDir(parts.SourceID, parts.LayerPath), digestDir(unit.Digest))
		dirs[dir] = struct{}{}
	}
	return dirs, true, nil
}

// walkCacheEntries discovers every content-addressed entry under root. An entry
// is the directory a cached file sits in — the <digest>/ level of
// <source-id>/<unit-path>/<digest>/<file> — so the layout is discovered from the
// files themselves rather than assumed from a fixed depth (a nested unit path
// contributes arbitrarily many intermediate directories). A missing cache root
// yields no entries.
func walkCacheEntries(root string) ([]CacheEntry, error) {
	if _, err := os.Stat(root); err != nil {
		// No cache on this machine yet: nothing to scan, nothing to prune.
		return nil, nil
	}
	sizes := map[string]int64{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// An unreadable directory means the scan cannot see everything it is
			// about to reason about, and a partial view is exactly what must not
			// drive a delete — surface it instead.
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		// A file that vanished between enumeration and stat contributes no size
		// rather than failing the whole scan; its entry is still discovered.
		if info, err := d.Info(); err == nil {
			sizes[filepath.Dir(path)] += info.Size()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(sizes))
	for dir := range sizes {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	out := make([]CacheEntry, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, newCacheEntry(cacheEntryRel(root, dir), dir, sizes[dir]))
	}
	return out, nil
}

// cacheEntryRel is an entry directory's path relative to the cache root, in
// slash form. Every discovered entry lives under root by construction, so this is
// a prefix trim rather than a fallible path computation.
func cacheEntryRel(root, dir string) string {
	return strings.TrimPrefix(filepath.ToSlash(dir), filepath.ToSlash(root)+"/")
}

// newCacheEntry splits an entry's root-relative path back into its (source, unit
// path, digest) coordinates. A path too shallow to carry both a source and a
// digest yields only what it does carry.
func newCacheEntry(rel, dir string, bytes int64) CacheEntry {
	entry := CacheEntry{Path: dir, Bytes: bytes, Digest: filepath.Base(dir)}
	segments := strings.Split(rel, "/")
	if len(segments) < 2 {
		return entry
	}
	entry.SourceID = segments[0]
	entry.UnitPath = strings.Join(segments[1:len(segments)-1], "/")
	return entry
}

// PruneCacheEntries removes the given cache entries and returns how many were
// removed and how many bytes were reclaimed. Removal routes through the fsops
// seam (the module's cross-platform mutation surface). Directories left empty by
// a removal are cleaned up to — but never including — root, so a pruned source
// does not leave an empty skeleton behind.
func PruneCacheEntries(root string, entries []CacheEntry) (int, int64, error) {
	removed := 0
	var bytes int64
	for _, e := range entries {
		if err := fsops.RemoveAll(e.Path); err != nil {
			return removed, bytes, err
		}
		removed++
		bytes += e.Bytes
		if err := removeEmptyParents(filepath.Dir(e.Path), root); err != nil {
			return removed, bytes, err
		}
	}
	return removed, bytes, nil
}

// removeEmptyParents deletes now-empty directories from dir upward, stopping at
// (and never removing) root. A non-empty directory ends the walk.
func removeEmptyParents(dir, root string) error {
	for dir != root && strings.HasPrefix(dir, root+string(os.PathSeparator)) {
		remaining, err := os.ReadDir(dir)
		if err != nil || len(remaining) > 0 {
			return nil
		}
		if err := fsops.Remove(dir); err != nil {
			return err
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

// dedupeSorted cleans, dedupes, and sorts a list of project paths so the scan is
// deterministic and a project registered twice (or passed alongside the cwd)
// contributes once.
func dedupeSorted(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		clean := filepath.Clean(p)
		if _, dup := seen[clean]; dup {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}
