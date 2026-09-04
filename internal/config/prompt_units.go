package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// prompt_units.go owns the SOURCE-QUALIFIED prompt file: a stage profile's
// prompt_files entry that names a config source rather than a local path
// (stage-profile-and-routing-consolidation § Source-Qualified Prompt Files).
//
// The three moving parts, matching the layer model one-for-one:
//
//  1. Grammar — a typed {source, path, version} entry is source-qualified by
//     construction; a bare string is source-qualified ONLY when its prefix before
//     ':' names a source declared in the effective config (the §5
//     "source-id:path[@version]" ref grammar ParseLayerRef already implements).
//     Anything else keeps local-path semantics, so a legacy "verifiers/unit.md"
//     (or a Windows "C:\..." literal) is never mistaken for a source ref.
//  2. Sync-time fetch — Resolve (the network-touching path behind `da config
//     sync`) fetches each declared prompt file through the SAME per-source-type
//     Fetcher the extends layers use and caches it content-addressed.
//  3. Lock pinning — each fetched prompt file is recorded as a UnitKindPrompt
//     lock unit keyed "<source-id>:<prompt-path>[@version]" with its resolved
//     digest, extending the R5 reproducibility guarantee (same source-set + lock
//     digest ⇒ identical effective bundle) from config content to prompt content.
//
// Every offline consumer (`da workflow resolve-prompt`, the ISP/orchestrator
// dispatch it feeds) then resolves a prompt purely from lock + cache, never the
// network — the same offline contract ResolveLocked keeps for layers.

// PromptUnitRef identifies one source-qualified prompt file: the source it is
// fetched from, the source-relative path, and an optional version pin. It is the
// prompt-tier analogue of LayerRefParts.
type PromptUnitRef struct {
	// SourceID is the declared `sources[].id` the prompt file comes from.
	SourceID string
	// Path is the prompt file path relative to the source root.
	Path string
	// Version pins a source-relative revision (branch/tag/sha). Empty means the
	// source's own declared ref.
	Version string
}

// Key returns the lock unit key for the prompt unit — "<source-id>:<prompt-path>"
// plus the "@<version>" pin when one is declared. It is the same
// source-id:path[@version] grammar layer and artifact unit keys use, so a prompt
// unit is addressable in the lock exactly like every other unit.
func (r PromptUnitRef) Key() string {
	key := r.SourceID + ":" + r.Path
	if r.Version != "" {
		key += "@" + r.Version
	}
	return key
}

// String returns the canonical wire form of the ref (identical to Key), so a
// prompt ref renders the same way in a lock key, a warning, and CLI output.
func (r PromptUnitRef) String() string { return r.Key() }

// SourceIndex maps source id → Source for ref lookup. Sources without an id are
// skipped (they cannot be referenced by a ref). It is the exported form of the
// resolver's internal index so a command surface can classify a prompt ref
// against the effective source set without re-implementing the rule.
func SourceIndex(srcs []Source) map[string]Source { return indexSources(srcs) }

// PromptUnitRefFor classifies ONE prompt_files entry against the declared source
// set, returning the source-qualified ref and true when the entry names a source.
//
// The typed object form is canonical: a non-empty Source is honored as declared
// even when that source id is unknown, so the failure surfaces as an unresolved
// prompt (with a sync hint) rather than being silently downgraded to a local
// path. A bare string is source-qualified only when its prefix before ':' matches
// a DECLARED source id — that guard is what keeps legacy local paths (and Windows
// drive letters) resolving locally.
func PromptUnitRefFor(entry PromptFileRef, sources map[string]Source) (PromptUnitRef, bool) {
	source := strings.TrimSpace(entry.Source)
	path := strings.TrimSpace(entry.Path)
	version := strings.TrimSpace(entry.Version)
	if path == "" {
		return PromptUnitRef{}, false
	}
	if source != "" {
		return PromptUnitRef{SourceID: source, Path: path, Version: version}, true
	}
	parts, err := ParseLayerRef(path)
	if err != nil {
		return PromptUnitRef{}, false
	}
	if _, declared := sources[parts.SourceID]; !declared {
		return PromptUnitRef{}, false
	}
	if version == "" {
		version = parts.Version
	}
	return PromptUnitRef{SourceID: parts.SourceID, Path: parts.LayerPath, Version: version}, true
}

// PromptUnitRefs collects every source-qualified prompt ref the effective
// config's stage_profiles declare, deduped by unit key and returned in stable
// key order so a resolve produces a byte-identical lock contribution run to run.
func PromptUnitRefs(rc AgentsRC) []PromptUnitRef {
	sources := indexSources(rc.Sources)
	byKey := map[string]PromptUnitRef{}
	for _, profiles := range rc.StageProfiles {
		for _, profile := range profiles {
			for _, entry := range profile.PromptFiles {
				if ref, ok := PromptUnitRefFor(entry, sources); ok {
					byKey[ref.Key()] = ref
				}
			}
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]PromptUnitRef, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}

// promptCacheDir is the content-addressed cache directory for one source+prompt
// file: <root>/<source-id>/<prompt-path>, the exact layout layerCacheDir uses for
// a config layer (spec §8). A prompt path and a layer path never collide in
// practice (a layer is a .json manifest fragment, a prompt is a source-tree
// document), and if they ever did the content is addressed by the same resolved
// SHA, so the entry is still correct.
func promptCacheDir(sourceID, promptPath string) string {
	return filepath.Join(configCacheRoot(), sourceID, filepath.FromSlash(promptPath))
}

// promptTarget is the FetchTarget one prompt unit's bytes are cached under: the
// content-addressed prompt cache dir plus the prompt's REAL basename
// ("verifiers/ts-lint.md" -> "ts-lint.md"). The name is threaded through the
// Fetcher interface (FetchTarget) rather than hard-coded, so a prompt caches as
// <sha>/ts-lint.md while a config layer still caches as <sha>/layer.json — one
// content-addressed writer/reader pair, two honest file names. A degenerate
// basename falls back to the layer default (cacheFileName).
func promptTarget(ref PromptUnitRef) FetchTarget {
	return FetchTarget{Dir: promptCacheDir(ref.SourceID, ref.Path), FileName: cacheFileName(ref.Path)}
}

// CachedPromptPath is the absolute path a prompt unit's bytes are cached at for a
// given resolved digest, e.g.
// ~/.agents/cache/config/team/verifiers/ts-lint.md/<sha>/ts-lint.md. The bytes
// are the prompt's own (markdown, usually), written by the SAME per-source-type
// Fetcher an extends layer rides.
func CachedPromptPath(ref PromptUnitRef, digest string) string {
	return promptTarget(ref).pathFor(digest)
}

// LockedPromptFile resolves a source-qualified prompt ref against the lock's
// units map and the on-disk cache — OFFLINE, with no fetch and no lock mutation.
// ok is false when the ref is not pinned as a prompt unit, carries no digest, or
// its cached bytes are absent (a pruned cache); the caller then reports the
// prompt unresolved and hints at `da config sync`, mirroring the
// LockedRemoteLayerBytes skip+sync-hint precedent.
func LockedPromptFile(units map[string]LockedUnit, ref PromptUnitRef) (string, bool) {
	unit, ok := units[ref.Key()]
	if !ok || unit.Kind != UnitKindPrompt || unit.Digest == "" {
		return "", false
	}
	path := CachedPromptPath(ref, unit.Digest)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

// promptUnits fetches every source-qualified prompt file the resolved snapshot
// declares and projects them to UnitKindPrompt lock entries. It runs on the
// online Resolve path only; offline it carries the previously locked entries
// forward so an offline resolve never wipes a pinned prompt.
//
// A per-ref failure (undeclared source, unsupported source type, transport) is
// NON-FATAL: the resolve keeps going with a warning and the previous pin is
// carried forward when there is one. A prompt is composition input, not policy —
// failing the whole `da config sync` because one stage profile names a stale
// prompt would be a strictly worse trade than surfacing it as unresolved at
// `da workflow resolve-prompt` time.
func (r *LayeredResolver) promptUnits(trace auditTrace, projectPath string, snap *Snapshot) (map[string]LockedUnit, []ProvenanceWarning, error) {
	refs := PromptUnitRefs(snap.Effective)
	if len(refs) == 0 {
		// The overwhelmingly common case (no source-qualified prompt anywhere in
		// the effective config) costs one map walk and no lock read.
		return nil, nil, nil
	}
	prevLock, err := ReadUnits(projectPath)
	if err != nil {
		return nil, nil, err
	}
	prev := prevLock.Units
	sources := indexSources(snap.Effective.Sources)
	out := make(map[string]LockedUnit, len(refs))
	var warnings []ProvenanceWarning
	for _, ref := range refs {
		key := ref.Key()
		if r.offline {
			carryPromptUnit(out, prev, key)
			continue
		}
		unit, err := r.fetchPromptUnit(trace, ref, sources)
		if err != nil {
			warnings = append(warnings, promptSkipWarning(key, err))
			carryPromptUnit(out, prev, key)
			continue
		}
		out[key] = unit
	}
	return out, warnings, nil
}

// carryPromptUnit copies a previously locked prompt unit forward into out when
// one exists, so an offline resolve or a failed re-fetch keeps the last good pin
// instead of dropping it (which would make an already-cached prompt resolve as
// unresolved).
func carryPromptUnit(out, prev map[string]LockedUnit, key string) {
	if unit, ok := prev[key]; ok && unit.Kind == UnitKindPrompt {
		out[key] = unit
	}
}

// fetchPromptUnit fetches one prompt file through the source's own Fetcher — the
// same seam (and the same test fake) the extends layers use — and returns its
// lock unit. The fetcher writes the bytes into the prompt cache dir addressed by
// the resolved SHA, which is exactly what CachedPromptPath reads back offline.
// The resolver-level `--refresh` force escape is threaded through so
// `da config sync` re-reads upstream rather than serving the SHA-addressed cache.
func (r *LayeredResolver) fetchPromptUnit(trace auditTrace, ref PromptUnitRef, sources map[string]Source) (LockedUnit, error) {
	src, ok := sources[ref.SourceID]
	if !ok {
		return LockedUnit{}, &ImportError{Ref: ref.Key(), SourceID: ref.SourceID, Reason: ReasonNotFound, Err: fmt.Errorf("source %q not declared", ref.SourceID)}
	}
	fetcher, err := r.fetcherFor(src.Type)
	if err != nil {
		return LockedUnit{}, &ImportError{Ref: ref.Key(), SourceID: ref.SourceID, Reason: ReasonSchema, Err: err}
	}
	parts := LayerRefParts{SourceID: ref.SourceID, LayerPath: ref.Path, Version: ref.Version}
	fetched, err := fetchWithRefresh(fetcher, src, parts, promptTarget(ref), r.refresh)
	if err != nil {
		var ie *ImportError
		if errors.As(err, &ie) {
			// A fetcher that already classified the failure (e.g. the oci
			// media-type guard) keeps its own reason.
			return LockedUnit{}, err
		}
		return LockedUnit{}, &ImportError{Ref: ref.Key(), SourceID: ref.SourceID, Reason: ReasonTransport, Err: err}
	}
	trace.emit(sourceFetchEvent(ref.SourceID, fetched.ResolvedSHA, fetched.CacheHit))
	return LockedUnit{
		Kind:   UnitKindPrompt,
		Digest: fetched.ResolvedSHA,
		// Commit-pinned, like an ordinary layer resolve: contentCacheKey derives
		// the key from content facts (and any declared cache_keys override) but NOT
		// from the per-resolve `--refresh` escape. `da config sync` always sets
		// --refresh, so recording the escape would stamp every synced prompt unit
		// with the AlwaysRevalidate sentinel permanently — a transient runtime flag
		// frozen into the lock, forcing an upstream re-check on every later resolve.
		// A source that declares always_revalidate still records the sentinel.
		CacheKey: r.contentCacheKey(src, withResolvedSHA(fetched.KeyInputs, fetched.ResolvedSHA)),
	}, nil
}

// promptSkipWarning records that a source-qualified prompt file could not be
// fetched on this resolve. It reuses the ProvenanceWarning channel the optional-
// extends skip rides, so a caller that already renders resolve warnings surfaces
// the prompt gap with no new plumbing.
func promptSkipWarning(key string, cause error) ProvenanceWarning {
	return ProvenanceWarning{FieldPath: "stage_profiles.prompt_files", AttemptedByLayer: key, Outcome: "prompt_skipped: " + cause.Error()}
}
