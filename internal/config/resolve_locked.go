package config

import (
	"fmt"
)

// ResolveLocked produces an effective-config Snapshot WITHOUT any network or git
// fetch and WITHOUT mutating .agentsrc.lock or the layer cache. It is the
// read-only seam `da config explain` (config-v2 p4e) parses the locked state
// through, so explain can be inspected offline and as a pure observer.
//
// Resolution model (mirrors Resolve, minus all writes/fetches):
//   - The repo-local manifest is read. If it declares no `extends`, resolution
//     degrades to the FLAT layer set (product-defaults → user-local → repo-local)
//     via the embedded FlatResolver, so explain still works on a flat project.
//   - Otherwise the imported `extends` layers are reconstructed by reading each
//     layer's bytes from the on-disk cache at its LOCKED SHA (from .agentsrc.lock).
//     No fetcher is ever invoked: a layer that is absent from the lockfile, or
//     whose cached bytes are missing, is a hard error (explain surfaces it) rather
//     than a fetch trigger.
//   - The assembled stack (product-defaults → user-local → imported → repo-local)
//     is merged through resolveSnapshot (the §7.2 merge shared with Resolve).
//
// ResolveLocked NEVER calls WriteConfigLock and NEVER calls a Fetcher.
func (r *LayeredResolver) ResolveLocked(projectPath string) (*Snapshot, error) {
	repoLayer, repoRaw, err := r.loadRepoLayer(projectPath)
	if err != nil {
		return nil, err
	}

	rc, err := decodeEffective(repoRaw)
	if err != nil {
		return nil, fmt.Errorf("decoding repo manifest: %w", err)
	}

	// Flat degrade: no extends means the locked stack is just the FLAT layers.
	// The embedded FlatResolver already shares this resolver's ProductDefaults
	// and user-local path, so its result matches the locked online resolution
	// of a flat project field-for-field.
	if len(rc.Extends) == 0 {
		return r.flat.Resolve(projectPath)
	}

	// §7A units read: the authoritative units section is read through the
	// one-time on-read migration (ReadUnits upgrades a legacy config-only lock),
	// so a units-only lock resolves offline and a pre-§7A lock is upgraded on
	// first read — no permanent dual-read of the legacy section.
	locked, err := readLockedLayersFromUnits(projectPath)
	if err != nil {
		return nil, err
	}

	stack := []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: r.productDefaults()},
	}
	if userLayer, ok, err := r.loadUserLayer(); err != nil {
		return nil, err
	} else if ok {
		stack = append(stack, userLayer)
	}

	imported, importWarnings, err := r.readLockedExtends(rc, locked)
	if err != nil {
		return nil, err
	}
	stack = append(stack, imported...)
	stack = append(stack, repoLayer)

	snap, err := resolveSnapshot(stack)
	if err != nil {
		return nil, err
	}
	snap.Warnings = append(snap.Warnings, importWarnings...)
	return snap, nil
}

// readLockedExtends reconstructs the imported `extends` layers from the on-disk
// cache using each entry's LOCKED SHA — read-only, no fetch. It returns the
// imported ResolvedLayers in precedence order plus any non-fatal warnings
// (optional entries whose lock/cache is missing are skipped with a warning, the
// same as the online optional-skip semantics). A required entry that is absent
// from the lockfile, or whose cached bytes are missing, is a fatal *ImportError
// so explain can surface the gap without ever triggering a fetch.
func (r *LayeredResolver) readLockedExtends(rc AgentsRC, locked map[string]LockedLayer) ([]ResolvedLayer, []ProvenanceWarning, error) {
	st := &lockedExtendsState{
		locked:      locked,
		digestByRef: map[string]string{},
		onStack:     map[string]bool{},
	}
	rootEnv := sourceEnv(indexSources(rc.Sources))
	for _, entry := range rc.Extends {
		if err := st.walk(r, entry, rootEnv); err != nil {
			if entry.Optional {
				st.warnings = append(st.warnings, optionalSkipWarning(entry.Ref, err))
				continue
			}
			return nil, nil, err
		}
	}
	return st.imported, st.warnings, nil
}

// lockedExtendsState mirrors extendsGraphState for the OFFLINE replay: the same
// children-first transitive walk (a layer's own sources/extends expand before
// the layer is admitted, post-order org→team→repo-local), but each layer is read
// from the cache at its locked SHA rather than fetched. This is what lets a repo
// declaring only its team source reproduce the org layer offline — the org unit
// was locked transitively by the online resolve (org-config-resolution §6.6,
// "Offline Locked Behavior").
type lockedExtendsState struct {
	locked      map[string]LockedLayer
	imported    []ResolvedLayer
	digestByRef map[string]string
	onStack     map[string]bool
	warnings    []ProvenanceWarning
}

func (st *lockedExtendsState) walk(r *LayeredResolver, entry LayerRef, env sourceEnv) error {
	parts, err := ParseLayerRef(entry.Ref)
	if err != nil {
		return &ImportError{Ref: entry.Ref, Reason: ReasonSchema, Err: err}
	}
	key := parts.SourceID + ":" + parts.LayerPath
	if parts.Version != "" {
		key += "@" + parts.Version
	}
	if st.onStack[key] {
		return &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonCycle, Err: fmt.Errorf("extends cycle detected at %q", entry.Ref)}
	}
	layer, warns, err := r.readOneLockedLayer(entry, env, st.locked)
	st.warnings = append(st.warnings, warns...)
	if err != nil {
		return err
	}
	sha := st.locked[entry.Ref].ResolvedSHA
	if prev, seen := st.digestByRef[key]; seen {
		if prev != sha {
			return &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonContent, Err: fmt.Errorf("ref %q locked at conflicting digests (%s vs %s)", entry.Ref, prev, sha)}
		}
		return nil
	}
	st.digestByRef[key] = sha
	childRC, err := decodeEffective(layer.Raw)
	if err != nil {
		return &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonSchema, Err: fmt.Errorf("decoding layer %q sources/extends: %w", entry.Ref, err)}
	}
	childEnv := env.child(childRC.Sources)
	st.onStack[key] = true
	for _, child := range childRC.Extends {
		if err := st.walk(r, child, childEnv); err != nil {
			if child.Optional {
				st.warnings = append(st.warnings, optionalSkipWarning(child.Ref, err))
				continue
			}
			st.onStack[key] = false
			return err
		}
	}
	st.onStack[key] = false
	st.imported = append(st.imported, layer)
	return nil
}

// readOneLockedLayer reconstructs a single extends entry's ResolvedLayer from the
// cache at its locked SHA. It parses the ref, resolves the source (for the cache
// dir), reads the locked SHA's cached bytes (no fetch), then decodes + validates
// the layer exactly as the online path does. Every failure is an *ImportError so
// callers map to config.import.failed with the right reason; a missing lock entry
// or missing cached bytes is ReasonTransport (the offline cache gap), mirroring
// fetchLayer's offline branch.
func (r *LayeredResolver) readOneLockedLayer(entry LayerRef, sources map[string]Source, locked map[string]LockedLayer) (ResolvedLayer, []ProvenanceWarning, error) {
	parts, err := ParseLayerRef(entry.Ref)
	if err != nil {
		return ResolvedLayer{}, nil, &ImportError{Ref: entry.Ref, Reason: ReasonSchema, Err: err}
	}
	if _, ok := sources[parts.SourceID]; !ok {
		return ResolvedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonNotFound, Err: fmt.Errorf("source %q not declared", parts.SourceID)}
	}

	lock, ok := locked[entry.Ref]
	if !ok || lock.ResolvedSHA == "" {
		return ResolvedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("no resolved SHA in %s for %q (run `da config sync`)", AgentsLockFile, entry.Ref)}
	}

	cacheDir := layerCacheDir(parts.SourceID, parts.LayerPath)
	data, ok := readCachedLayer(cacheDir, lock.ResolvedSHA)
	if !ok {
		return ResolvedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("locked SHA %s for %q not in cache (run `da config sync`)", lock.ResolvedSHA, entry.Ref)}
	}

	raw, err := decodeLayerBytes(entry.Ref, data)
	if err != nil {
		return ResolvedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}
	sanitized, schemaWarns, err := validateLayer(entry.Ref, raw)
	if err != nil {
		return ResolvedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}

	return ResolvedLayer{ID: entry.Ref, Present: true, Raw: sanitized}, schemaWarns, nil
}
