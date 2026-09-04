package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LayerLockStatus is the offline verification result for one declared `extends`
// layer: whether the lockfile pins a resolved SHA for it and whether the
// downloaded layer bytes are present in the on-disk cache at that SHA. Every
// source type (git/http/local) is content-hashed and cached identically, so the
// same check applies to all. It is what `da config verify` (config-v2 p4c)
// cross-checks so a layer can be confirmed offline — present and consistent with
// the lockfile — without ever re-fetching.
type LayerLockStatus struct {
	Ref        string `json:"ref"`
	SourceID   string `json:"source_id,omitempty"`
	SourceType string `json:"source_type,omitempty"` // local | git | http | oci
	Optional   bool   `json:"optional,omitempty"`
	// Locked is true when the lockfile has an entry for Ref with a non-empty SHA.
	Locked bool   `json:"locked"`
	SHA    string `json:"sha,omitempty"`
	// Cached is true when the cached layer.json exists at the locked SHA. Applies
	// to every source type — git/http/local layers are all content-hashed and
	// written to the same content-addressed cache.
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
// SHA and whether the downloaded bytes for that SHA are present in the cache —
// the same check for git, http, and local sources (all are content-hashed and
// cached identically).
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
	// §7A units read: verify reads the authoritative units section through the
	// one-time on-read migration so a units-only lock verifies its pinned layers
	// (and a legacy config-only lock is upgraded on first read) — not a permanent
	// dual-read of the legacy section.
	locked, err := readLockedLayersFromUnits(projectPath)
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

	// Every fetched source — git, http, AND local — is content-hashed and cached
	// the same way (localFetcher hashes the bytes and writes the cache), and the
	// offline resolver reads all of them from the cache at the locked SHA. So a
	// locked layer of any source type must have its cached bytes present.
	target := layerTarget(parts.SourceID, parts.LayerPath)
	st.CachePath = target.pathFor(st.SHA)
	if _, ok := readCachedUnit(target, st.SHA); ok {
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
