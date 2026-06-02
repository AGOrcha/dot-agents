package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LayerLockStatus is the offline verification result for one declared `extends`
// layer: whether the lockfile pins a resolved SHA for it and, for remote
// sources, whether the downloaded layer bytes are present in the on-disk cache
// at that SHA. It is what `da config verify` (config-v2 p4c) cross-checks so a
// remote layer can be confirmed offline — present and consistent with the
// lockfile — without ever re-fetching.
type LayerLockStatus struct {
	Ref        string `json:"ref"`
	SourceID   string `json:"source_id,omitempty"`
	SourceType string `json:"source_type,omitempty"` // local | git | http | oci
	Optional   bool   `json:"optional,omitempty"`
	// Locked is true when the lockfile has an entry for Ref with a non-empty SHA.
	Locked bool   `json:"locked"`
	SHA    string `json:"sha,omitempty"`
	// Cached is true when the layer is satisfied offline: for remote sources the
	// cached layer.json exists at the locked SHA; local-source layers resolve
	// from disk (no SHA cache) and are reported satisfied when locked.
	Cached    bool   `json:"cached"`
	CachePath string `json:"cache_path,omitempty"`
	// Problem is empty when the layer verifies; otherwise a short, actionable
	// reason (missing lock entry, cache miss, bad ref, undeclared source).
	Problem string `json:"problem,omitempty"`
}

// OK reports whether this layer verified cleanly offline.
func (s LayerLockStatus) OK() bool { return s.Problem == "" }

// VerifyLayerLocks cross-checks every declared `extends` layer in the project's
// manifest against the lockfile and the on-disk layer cache, WITHOUT any fetch
// or lockfile mutation. For each layer it reports whether the lockfile pins a
// SHA and — for remote (git/http/oci) sources — whether the downloaded bytes
// for that SHA are present in the cache. Local-source layers resolve from disk,
// so they are reported satisfied once locked.
//
// Returns an empty slice (no error) when the project declares no `extends`, or
// when the manifest is absent (the caller's manifest check owns that failure).
func VerifyLayerLocks(projectPath string) ([]LayerLockStatus, error) {
	data, err := os.ReadFile(filepath.Join(projectPath, AgentsRCFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rc AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", AgentsRCFile, err)
	}
	if len(rc.Extends) == 0 {
		return nil, nil
	}

	sources := indexSources(rc.Sources)
	locked, err := readLockedLayers(projectPath)
	if err != nil {
		return nil, err
	}

	out := make([]LayerLockStatus, 0, len(rc.Extends))
	for _, entry := range rc.Extends {
		out = append(out, verifyOneLayerLock(entry, sources, locked))
	}
	return out, nil
}

func verifyOneLayerLock(entry LayerRef, sources map[string]Source, locked map[string]LockedLayer) LayerLockStatus {
	st := LayerLockStatus{Ref: entry.Ref, Optional: entry.Optional}

	parts, err := ParseLayerRef(entry.Ref)
	if err != nil {
		st.Problem = "invalid layer ref: " + err.Error()
		return st
	}
	st.SourceID = parts.SourceID

	src, ok := sources[parts.SourceID]
	if !ok {
		st.Problem = fmt.Sprintf("source %q not declared in manifest", parts.SourceID)
		return st
	}
	st.SourceType = src.Type

	if lock, ok := locked[entry.Ref]; ok && lock.ResolvedSHA != "" {
		st.Locked = true
		st.SHA = lock.ResolvedSHA
	}
	if !st.Locked {
		st.Problem = "not pinned in " + AgentsLockFile + " (run `da config sync`)"
		return st
	}

	// Local-source layers come from disk, not the SHA-addressed cache; being
	// locked is sufficient for the offline check.
	if src.Type == "local" {
		st.Cached = true
		return st
	}

	cacheDir := layerCacheDir(parts.SourceID, parts.LayerPath)
	st.CachePath = cachedLayerPath(cacheDir, st.SHA)
	if _, ok := readCachedLayer(cacheDir, st.SHA); ok {
		st.Cached = true
		return st
	}
	st.Problem = fmt.Sprintf("locked SHA %s not in cache (run `da config sync`)", shortSHA(st.SHA))
	return st
}

// shortSHA abbreviates a resolved SHA for human messages without assuming a
// minimum length (content hashes and git SHAs both flow through here).
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
