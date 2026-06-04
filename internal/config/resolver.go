package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
)

// Resolver produces an effective-config Snapshot from a set of layers. The FLAT
// implementation (FlatResolver) walks only the local layers (product defaults,
// user-local, repo-local). The layered implementation (config-v2 p1b) extends
// the same interface to fetch declared `extends` layers over git/http/local
// before the repo-local layer. The interface is the seam both share.
type Resolver interface {
	// Resolve produces the effective Snapshot for the project at projectPath.
	// A fatal error (e.g. repo-local manifest fails to parse) is returned;
	// non-fatal events (protected-field violations) surface in Snapshot.Warnings.
	Resolve(projectPath string) (*Snapshot, error)
}

// MergeCategory classifies how a field combines across layers, per
// org-config-resolution §7.2. The default for any field not explicitly
// categorized is CategoryScalar (last writer wins).
type MergeCategory int

const (
	// CategoryScalar: last writer in precedence order wins (the whole value is
	// replaced). Applies to scalars and, by default, to any uncategorized field.
	CategoryScalar MergeCategory = iota
	// CategorySetUnion: arrays representing sets — union with stable order,
	// dedup by value. Applies to skills, agents, rules.
	CategorySetUnion
	// CategoryMapMerge: object maps — merge by key, recursing into nested maps;
	// per-key value uses CategoryScalar semantics. Applies to verifier_profiles,
	// execution_profile, features, kg. For execution_profile this is what makes a
	// scope override (or a recompute-proposed relevance diff) facet-independent:
	// a higher layer that sets only one facet (e.g. relevance) deep-merges over
	// the base, so the untouched topology/lenses facets are preserved instead of
	// being wiped by a wholesale last-writer replace.
	CategoryMapMerge
	// CategoryOrderedReplace: arrays representing ordered execution — replaced
	// wholesale by the highest-precedence writer (never merged). Applies to
	// sources and to each app_type_verifier_map entry's sequence.
	CategoryOrderedReplace
)

// fieldCategories maps top-level manifest keys to their merge category. Keys
// absent from the map fall through to CategoryScalar. app_type_verifier_map is
// CategoryMapMerge at the top level (merge by app-type key); each entry's value
// is an ordered sequence replaced wholesale, which CategoryMapMerge already does
// because it only recurses into nested maps, not arrays.
var fieldCategories = map[string]MergeCategory{
	"skills":                CategorySetUnion,
	"agents":                CategorySetUnion,
	"rules":                 CategorySetUnion,
	"verifier_profiles":     CategoryMapMerge,
	"app_type_verifier_map": CategoryMapMerge,
	"execution_profile":     CategoryMapMerge,
	"features":              CategoryMapMerge,
	"kg":                    CategoryMapMerge,
	"sources":               CategoryOrderedReplace,
	"extends":               CategoryOrderedReplace,
	"packages":              CategoryOrderedReplace,
}

// protectedSet is the lookup form of ProtectedFields.
var protectedSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(ProtectedFields))
	for _, f := range ProtectedFields {
		m[f] = struct{}{}
	}
	return m
}()

// FlatResolver resolves effective config from the FLAT layer set only: built-in
// product defaults, the user-local manifest (~/.agents/.agentsrc.json), and the
// repo-local manifest. It performs no network or git fetch — `extends` entries
// are recorded on the effective config but not followed (that is config-v2 p1b).
type FlatResolver struct {
	// ProductDefaults is the lowest-precedence layer. When nil, an empty object
	// is used so the layer is always present in the stack (Present=true) even
	// when it carries no fields, which keeps explain output stable.
	ProductDefaults map[string]any
	// userLocalPath overrides the user-local manifest path (test seam). When
	// empty, defaults to <AgentsHome>/.agentsrc.json.
	userLocalPath string
}

// NewFlatResolver returns a FlatResolver with empty product defaults and the
// default user-local manifest path.
func NewFlatResolver() *FlatResolver {
	return &FlatResolver{ProductDefaults: map[string]any{}}
}

// WithUserLocalPath sets an explicit user-local manifest path (test seam) and
// returns the receiver for chaining.
func (r *FlatResolver) WithUserLocalPath(path string) *FlatResolver {
	r.userLocalPath = path
	return r
}

// Resolve implements Resolver for the FLAT layer set.
func (r *FlatResolver) Resolve(projectPath string) (*Snapshot, error) {
	layers, err := r.loadLayers(projectPath)
	if err != nil {
		return nil, err
	}
	return resolveSnapshot(layers)
}

// loadLayers loads the three FLAT layers in precedence order. The repo-local
// manifest is required (a missing or unparseable file is fatal); the user-local
// manifest is optional (absence is not an error). Product defaults come from the
// resolver's ProductDefaults field.
func (r *FlatResolver) loadLayers(projectPath string) ([]ResolvedLayer, error) {
	product := r.ProductDefaults
	if product == nil {
		product = map[string]any{}
	}
	layers := []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: product},
	}

	userPath := r.userLocalPath
	if userPath == "" {
		userPath = filepath.Join(AgentsHome(), AgentsRCFile)
	}
	userLayer := ResolvedLayer{ID: LayerUserLocal}
	if raw, ok, err := decodeObjectFile(userPath); err != nil {
		return nil, fmt.Errorf("parsing user-local %s: %w", userPath, err)
	} else if ok {
		userLayer.Present = true
		userLayer.Raw = raw
	}
	layers = append(layers, userLayer)

	repoPath := filepath.Join(projectPath, AgentsRCFile)
	repoRaw, ok, err := decodeObjectFile(repoPath)
	if err != nil {
		return nil, fmt.Errorf("parsing repo-local %s: %w", repoPath, err)
	}
	if !ok {
		return nil, fmt.Errorf("no %s found at %s", AgentsRCFile, projectPath)
	}
	layers = append(layers, ResolvedLayer{ID: LayerRepoLocal, Present: true, Raw: repoRaw})
	return layers, nil
}

// resolveSnapshot merges the ordered layers into an effective Snapshot. It is
// the shared core both FlatResolver and (later) the layered resolver feed.
func resolveSnapshot(layers []ResolvedLayer) (*Snapshot, error) {
	merged := map[string]any{}
	warnings := []ProvenanceWarning{}

	for _, layer := range layers {
		if layer.Raw == nil {
			continue
		}
		for k, v := range layer.Raw {
			// Protected fields may only be set by the repo-local layer. A
			// lower-precedence (imported / user-local) layer attempting to set
			// one is dropped with a non-fatal warning.
			if _, prot := protectedSet[k]; prot && layer.ID != LayerRepoLocal {
				warnings = append(warnings, ProvenanceWarning{
					FieldPath:        k,
					AttemptedByLayer: layer.ID,
					Outcome:          "dropped",
				})
				continue
			}
			merged[k] = mergeField(k, merged[k], v)
		}
	}

	effective, err := decodeEffective(merged)
	if err != nil {
		return nil, err
	}

	snap := &Snapshot{
		Effective:  effective,
		Provenance: map[string]FieldProvenance{},
		Layers:     layers,
		Warnings:   warnings,
	}
	snap.populateProvenance()
	return snap, nil
}

// populateProvenance fills snap.Provenance with the per-field layer stack for
// every top-level field any layer sets, honoring the protected-field drop so a
// protected field's stack only credits the repo-local layer.
func (s *Snapshot) populateProvenance() {
	for _, name := range s.FieldNames() {
		fp := s.FieldAt(name)
		if _, prot := protectedSet[name]; prot {
			fp = s.protectedFieldProvenance(name, fp)
		}
		s.Provenance[name] = fp
	}
}

// protectedFieldProvenance rebuilds a protected field's stack so only the
// repo-local layer is eligible to be active — lower layers show their attempted
// value but are never marked active and never win.
func (s *Snapshot) protectedFieldProvenance(name string, fp FieldProvenance) FieldProvenance {
	fp.ActiveLayer = ""
	for i := range fp.Layers {
		fp.Layers[i].Active = false
		if fp.Layers[i].Layer == LayerRepoLocal && fp.Layers[i].Value != nil {
			fp.Layers[i].Active = true
			fp.ActiveLayer = LayerRepoLocal
		}
	}
	return fp
}

// mergeField combines a previously-accumulated value (prev) with the next
// layer's value for key, per the field's merge category. prev is nil when no
// prior layer set the field.
func mergeField(key string, prev, next any) any {
	switch fieldCategories[key] {
	case CategorySetUnion:
		return unionSlices(prev, next)
	case CategoryMapMerge:
		return mergeMaps(prev, next)
	default:
		// CategoryScalar (the zero value, so also every uncategorized key) and
		// CategoryOrderedReplace both replace wholesale with the latest writer.
		return next
	}
}

// unionSlices unions two JSON arrays of scalars with stable order (prev entries
// first, then new next entries), deduplicating by value. A non-array next falls
// back to last-writer-wins; a nil/non-array prev is treated as the empty set, so
// next is still deduplicated against itself.
func unionSlices(prev, next any) any {
	nextArr, nextOK := next.([]any)
	if !nextOK {
		return next // next isn't an array — replace
	}
	prevArr, _ := prev.([]any)
	out := make([]any, 0, len(prevArr)+len(nextArr))
	seen := map[string]struct{}{}
	for _, item := range append(append([]any{}, prevArr...), nextArr...) {
		key := scalarKey(item)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

// scalarKey returns a stable dedup key for a set element. Strings key by their
// raw value; anything else keys by its JSON encoding.
func scalarKey(v any) string {
	if s, ok := v.(string); ok {
		return "s:" + s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("x:%v", v)
	}
	return "j:" + string(data)
}

// mergeMaps merges two JSON objects by key. Nested objects recurse (map-merge);
// every other value type uses last-writer-wins. Non-object inputs fall back to
// last-writer-wins.
func mergeMaps(prev, next any) any {
	prevMap, prevOK := prev.(map[string]any)
	nextMap, nextOK := next.(map[string]any)
	if !nextOK {
		return next
	}
	if !prevOK {
		return next
	}
	out := make(map[string]any, len(prevMap)+len(nextMap))
	for k, v := range prevMap {
		out[k] = v
	}
	for k, v := range nextMap {
		if existing, ok := out[k]; ok {
			if _, bothObj := existing.(map[string]any); bothObj {
				out[k] = mergeMaps(existing, v)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// decodeEffective marshals the merged generic object back through the AgentsRC
// unmarshaler so the effective Snapshot carries a fully-typed manifest (with
// ExtraFields populated for keys outside the typed surface).
func decodeEffective(merged map[string]any) (AgentsRC, error) {
	data, err := json.Marshal(merged)
	if err != nil {
		return AgentsRC{}, fmt.Errorf("marshaling merged config: %w", err)
	}
	var rc AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		return AgentsRC{}, fmt.Errorf("decoding effective config: %w", err)
	}
	return rc, nil
}

// decodeObjectFile reads path and decodes it into a generic JSON object.
// Returns (obj, true, nil) on success, (nil, false, nil) when the file is
// absent, and (nil, false, err) when the file exists but does not parse as a
// JSON object.
func decodeObjectFile(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, err
	}
	return m, true, nil
}

// --- Lockfile (.agentsrc.lock) ---------------------------------------------

// AgentsLockFile is the lockfile name — the resolved-state companion to
// AgentsRCFile (.agentsrc.json), committed alongside it (spec §7).
const AgentsLockFile = ".agentsrc.lock"

// LockSectionConfig is the agentslock section name owned by the config resolver
// (spec §7). The package resolver (pass 2) owns "packages" and the graph
// adapter owns "adapters"; this resolver writes config + an empty packages stub.
const (
	LockSectionConfig   = "config"
	LockSectionPackages = "packages"
)

// AgentsLockPath returns the canonical .agentsrc.lock path for a project: the
// sibling of the repo-local .agentsrc.json (spec §7). This is the single shared
// definition of the lockfile location. Every section writer — the config
// resolver here, the package resolver (pass 2), the graph-adapter lifecycle
// (#178, "adapters" section), and `da doctor`/`da status` (config-v2 p2) — MUST
// resolve the lockfile through this helper rather than re-deriving the path, so
// the canonical location can never drift between writers.
func AgentsLockPath(projectPath string) string {
	return filepath.Join(projectPath, AgentsLockFile)
}

// LockedLayer is one entry in the lockfile's "config" section: the resolved SHA
// a config layer was fetched at, plus its cache TTL window (spec §7). The map
// key is the layer ref ("acme:org/base").
type LockedLayer struct {
	// ResolvedSHA is the git commit SHA or content hash at fetch time.
	ResolvedSHA string `json:"resolved_sha"`
	// FetchedAt is the RFC3339 timestamp the layer was fetched.
	FetchedAt string `json:"fetched_at"`
	// TTLExpiresAt is when the SHA should be re-checked, derived from the
	// source cache_ttl. Empty means never re-check automatically (requires an
	// explicit `da config sync`).
	TTLExpiresAt string `json:"ttl_expires_at,omitempty"`
}

// WriteConfigLock writes the resolved config-layer state to .agentsrc.lock via
// the shared agentslock writer, preserving any sibling sections (packages,
// adapters) another writer already populated. It also stages an empty packages
// stub when none exists yet, so a fresh lockfile carries both tier-1 sections
// (spec §7); a pre-existing packages section written by pass 2 is left intact.
func WriteConfigLock(projectPath string, layers map[string]LockedLayer) error {
	lf, err := agentslock.Open(AgentsLockPath(projectPath))
	if err != nil {
		return err
	}
	if err := lf.SetSection(LockSectionConfig, layers); err != nil {
		return err
	}
	// Establish an empty packages stub only if pass 2 has not written one.
	if present, err := lf.Section(LockSectionPackages, &map[string]json.RawMessage{}); err != nil {
		return err
	} else if !present {
		if err := lf.SetSection(LockSectionPackages, map[string]json.RawMessage{}); err != nil {
			return err
		}
	}
	return lf.Flush()
}

// readLockedLayers loads the "config" section of an existing lockfile, or an
// empty map when the file or section is absent. It is the offline / TTL source
// of last-resolved SHAs.
func readLockedLayers(projectPath string) (map[string]LockedLayer, error) {
	lf, err := agentslock.Open(AgentsLockPath(projectPath))
	if err != nil {
		return nil, err
	}
	locked := map[string]LockedLayer{}
	if _, err := lf.Section(LockSectionConfig, &locked); err != nil {
		return nil, err
	}
	return locked, nil
}

// --- LayeredResolver --------------------------------------------------------

// LayeredResolver extends the FLAT layer set with tier-1 `extends` imports
// (spec §6 pass 1): product defaults → user-local → extends[] (left-to-right,
// fetched over git/http/local) → repo-local. It resolves each extends ref to a
// source, enforces the tier constraint (oci in extends is a schema error),
// fetches + caches the layer content-addressed by SHA, validates it against the
// layer schema, and records the resolved SHAs to .agentsrc.lock.
type LayeredResolver struct {
	flat *FlatResolver
	// fetchers overrides the per-source-type Fetcher (test seam). When a type
	// is absent, the default SelectFetcher impl is used.
	fetchers map[string]Fetcher
	// offline forces use of the last resolved SHA from the lockfile instead of
	// contacting any source; a cache_hit_offline warning is emitted per layer.
	offline bool
	// now is the clock seam for TTL math (test override). Nil uses time.Now.
	now func() time.Time
	// emitter receives config.* audit events emitted during resolution. Nil
	// means no auditing (normalized to the no-op sink per resolve).
	emitter AuditEmitter
}

// NewLayeredResolver returns a LayeredResolver wrapping a default FlatResolver.
func NewLayeredResolver() *LayeredResolver {
	return &LayeredResolver{flat: NewFlatResolver(), fetchers: map[string]Fetcher{}}
}

// WithProductDefaults sets the product-defaults layer and returns the receiver.
func (r *LayeredResolver) WithProductDefaults(d map[string]any) *LayeredResolver {
	r.flat.ProductDefaults = d
	return r
}

// WithUserLocalPath sets the user-local manifest path (test seam) and returns
// the receiver.
func (r *LayeredResolver) WithUserLocalPath(path string) *LayeredResolver {
	r.flat.WithUserLocalPath(path)
	return r
}

// WithFetcher registers a Fetcher for a source type (test seam: inject a
// fakeFetcher for "git" so no test touches the network). Returns the receiver.
func (r *LayeredResolver) WithFetcher(sourceType string, f Fetcher) *LayeredResolver {
	r.fetchers[sourceType] = f
	return r
}

// WithOffline toggles offline mode (use last resolved SHA from the lockfile).
func (r *LayeredResolver) WithOffline(offline bool) *LayeredResolver {
	r.offline = offline
	return r
}

// WithClock sets the TTL clock seam and returns the receiver.
func (r *LayeredResolver) WithClock(now func() time.Time) *LayeredResolver {
	r.now = now
	return r
}

// WithEmitter registers the AuditEmitter that receives config.* events emitted
// during resolution (spec §9). A nil emitter disables auditing. Returns the
// receiver for chaining.
func (r *LayeredResolver) WithEmitter(e AuditEmitter) *LayeredResolver {
	r.emitter = e
	return r
}

func (r *LayeredResolver) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now().UTC()
}

func (r *LayeredResolver) fetcherFor(sourceType string) (Fetcher, error) {
	if f, ok := r.fetchers[sourceType]; ok {
		return f, nil
	}
	return SelectFetcher(sourceType)
}

// Resolve implements Resolver. It builds the full layer stack (FLAT + imported
// extends), merges it into a Snapshot, and writes the resolved-layer SHAs to
// .agentsrc.lock. Layer fetch/validation errors surface as *ImportError for
// non-optional entries; optional entries that fail are skipped with a warning.
func (r *LayeredResolver) Resolve(projectPath string) (*Snapshot, error) {
	trace := newAuditTrace(r.emitter)

	repoLayer, repoRaw, err := r.loadRepoLayer(projectPath)
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

	imported, locked, importWarnings, err := r.resolveExtends(trace, projectPath, repoRaw)
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

	// Field-level audit derives from the produced snapshot so the shared merge
	// core (resolveSnapshot) stays unchanged: overrides come from the provenance
	// stacks, protection violations from the recorded warnings.
	emitFieldEvents(trace, snap)
	trace.emit(effectiveProducedEvent(repoLayerID(snap), len(snap.Layers)))

	if err := WriteConfigLock(projectPath, locked); err != nil {
		return nil, fmt.Errorf("writing %s: %w", AgentsLockFile, err)
	}
	return snap, nil
}

// emitFieldEvents emits config.field.overridden for every effective field that
// more than one layer set (the higher-precedence layer wins) and
// config.field.protection_violation for every recorded protected-field drop.
// It reads only the produced snapshot, so it never affects merge semantics.
func emitFieldEvents(trace auditTrace, snap *Snapshot) {
	for _, name := range snap.FieldNames() {
		fp := snap.Provenance[name]
		// Collect the layers that actually contributed a value, in precedence
		// order. An override exists only when two or more layers set the field.
		var setLayers []LayerValue
		for _, lv := range fp.Layers {
			if lv.Value != nil {
				setLayers = append(setLayers, lv)
			}
		}
		if len(setLayers) < 2 {
			continue
		}
		winner := setLayers[len(setLayers)-1]
		prev := setLayers[len(setLayers)-2]
		trace.emit(fieldOverriddenEvent(name, prev.Layer, winner.Layer, winner.Value))
	}
	for _, w := range snap.Warnings {
		if w.Outcome == "dropped" {
			trace.emit(protectionViolationEvent(w.FieldPath, w.AttemptedByLayer))
		}
	}
}

// repoLayerID returns the effective repo_id for the terminal effective-produced
// event, or "" when the resolved manifest declares none.
func repoLayerID(snap *Snapshot) string {
	return snap.Effective.RepoID
}

func (r *LayeredResolver) productDefaults() map[string]any {
	if r.flat.ProductDefaults == nil {
		return map[string]any{}
	}
	return r.flat.ProductDefaults
}

// loadRepoLayer loads the required repo-local manifest, returning both the
// ResolvedLayer (for the stack) and its raw object (to read `sources`/`extends`).
func (r *LayeredResolver) loadRepoLayer(projectPath string) (ResolvedLayer, map[string]any, error) {
	repoPath := filepath.Join(projectPath, AgentsRCFile)
	raw, ok, err := decodeObjectFile(repoPath)
	if err != nil {
		return ResolvedLayer{}, nil, fmt.Errorf("parsing repo-local %s: %w", repoPath, err)
	}
	if !ok {
		return ResolvedLayer{}, nil, fmt.Errorf("no %s found at %s", AgentsRCFile, projectPath)
	}
	return ResolvedLayer{ID: LayerRepoLocal, Present: true, Raw: raw}, raw, nil
}

// loadUserLayer loads the optional user-local manifest.
func (r *LayeredResolver) loadUserLayer() (ResolvedLayer, bool, error) {
	userPath := r.flat.userLocalPath
	if userPath == "" {
		userPath = filepath.Join(AgentsHome(), AgentsRCFile)
	}
	raw, ok, err := decodeObjectFile(userPath)
	if err != nil {
		return ResolvedLayer{}, false, fmt.Errorf("parsing user-local %s: %w", userPath, err)
	}
	if !ok {
		return ResolvedLayer{}, false, nil
	}
	return ResolvedLayer{ID: LayerUserLocal, Present: true, Raw: raw}, true, nil
}

// extendsResult is one layer's fetch outcome, collected per-index so the
// precedence order of the imported stack and the warning sequence stay
// deterministic regardless of which goroutine finishes first.
type extendsResult struct {
	layer ResolvedLayer
	lock  LockedLayer
	warns []ProvenanceWarning
	err   error
}

// resolveExtends walks the repo-local manifest's `extends` array left-to-right,
// fetching + validating each layer, and returns the imported ResolvedLayers (in
// precedence order), the lockfile entries to persist, and any non-fatal
// warnings. The repo manifest is parsed into a typed AgentsRC to read its typed
// sources/extends rather than re-walking the generic map.
//
// Each layer fetch is independent network/IO work, so the fetches run
// concurrently with a bounded worker pool; the .agentsrc.lock write stays a
// single serialized flush downstream (spec §7.4 "parallel resolution,
// serialized write"). Results are reduced in entry order afterwards so the
// imported stack, the warning sequence, and the first non-optional failure are
// identical to a sequential walk.
func (r *LayeredResolver) resolveExtends(trace auditTrace, projectPath string, repoRaw map[string]any) ([]ResolvedLayer, map[string]LockedLayer, []ProvenanceWarning, error) {
	rc, err := decodeEffective(repoRaw)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decoding repo manifest: %w", err)
	}
	locked := map[string]LockedLayer{}
	if len(rc.Extends) == 0 {
		return nil, locked, nil, nil
	}

	sources := indexSources(rc.Sources)
	var prevLocked map[string]LockedLayer
	if r.offline {
		if prevLocked, err = readLockedLayers(projectPath); err != nil {
			return nil, nil, nil, err
		}
	}

	// Fetch all layers concurrently. Each goroutine writes its own result slot
	// (no shared mutation); sources and prevLocked are read-only.
	results := make([]extendsResult, len(rc.Extends))
	sem := make(chan struct{}, resolveConcurrency(len(rc.Extends)))
	var wg sync.WaitGroup
	for i, entry := range rc.Extends {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, entry LayerRef) {
			defer wg.Done()
			defer func() { <-sem }()
			layer, lock, warns, err := r.resolveOneLayer(trace, entry, sources, prevLocked)
			results[i] = extendsResult{layer: layer, lock: lock, warns: warns, err: err}
		}(i, entry)
	}
	wg.Wait()

	// Reduce in entry order: deterministic precedence, warnings, and the first
	// non-optional failure (matching the prior sequential semantics).
	imported := make([]ResolvedLayer, 0, len(rc.Extends))
	warnings := []ProvenanceWarning{}
	for i, entry := range rc.Extends {
		res := results[i]
		warnings = append(warnings, res.warns...)
		if res.err != nil {
			trace.emit(importFailedEvent(asImportError(entry.Ref, res.err), entry.Optional))
			if entry.Optional {
				warnings = append(warnings, optionalSkipWarning(entry.Ref, res.err))
				continue
			}
			return nil, nil, nil, res.err
		}
		imported = append(imported, res.layer)
		locked[entry.Ref] = res.lock
	}
	return imported, locked, warnings, nil
}

// resolveConcurrency bounds the extends-fetch worker pool. Layer fetches are
// network/IO-bound, so the cap oversubscribes the CPU count (always >=4 since
// GOMAXPROCS is >=1), clamped to the number of layers — never spawn more
// workers than there is work.
func resolveConcurrency(n int) int {
	if n < 1 {
		return 1
	}
	limit := runtime.GOMAXPROCS(0) * 4
	if n < limit {
		return n
	}
	return limit
}

// resolveOneLayer resolves a single extends entry: parse ref, locate source,
// enforce the tier constraint, fetch (cache/TTL/offline), validate, and produce
// the ResolvedLayer + lockfile entry. Errors are *ImportError so callers can map
// to config.import.failed with the right reason.
func (r *LayeredResolver) resolveOneLayer(trace auditTrace, entry LayerRef, sources map[string]Source, prevLocked map[string]LockedLayer) (ResolvedLayer, LockedLayer, []ProvenanceWarning, error) {
	parts, err := ParseLayerRef(entry.Ref)
	if err != nil {
		return ResolvedLayer{}, LockedLayer{}, nil, &ImportError{Ref: entry.Ref, Reason: ReasonSchema, Err: err}
	}
	src, ok := sources[parts.SourceID]
	if !ok {
		return ResolvedLayer{}, LockedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonNotFound, Err: fmt.Errorf("source %q not declared", parts.SourceID)}
	}
	// Tier constraint (spec §4): extends must reference git|http|local — oci is
	// packages-only. Enforced before any fetch so the error surfaces early.
	fetcher, err := r.fetcherFor(src.Type)
	if err != nil {
		return ResolvedLayer{}, LockedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}

	cacheDir := layerCacheDir(parts.SourceID, parts.LayerPath)
	fetched, warns, err := r.fetchLayer(trace, parts, entry, src, fetcher, cacheDir, prevLocked)
	if err != nil {
		return ResolvedLayer{}, LockedLayer{}, warns, err
	}

	raw, err := decodeLayerBytes(entry.Ref, fetched.Data)
	if err != nil {
		return ResolvedLayer{}, LockedLayer{}, warns, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}
	sanitized, schemaWarns, err := validateLayer(entry.Ref, raw)
	if err != nil {
		return ResolvedLayer{}, LockedLayer{}, warns, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}
	warns = append(warns, schemaWarns...)

	layer := ResolvedLayer{ID: entry.Ref, Present: true, Raw: sanitized}
	lock := r.lockEntry(src, fetched.ResolvedSHA)
	// The layer is validated and admitted to the stack: record its resolution
	// with the number of top-level fields it contributes and its resolved SHA.
	trace.emit(layerResolveEvent(entry.Ref, fetched.ResolvedSHA, len(sanitized)))
	return layer, lock, warns, nil
}

// fetchLayer performs the cache/TTL/offline-aware fetch for one layer. In
// offline mode it serves the last resolved SHA from the lockfile cache (with a
// cache_hit_offline warning) and never contacts the source.
func (r *LayeredResolver) fetchLayer(trace auditTrace, parts LayerRefParts, entry LayerRef, src Source, fetcher Fetcher, cacheDir string, prevLocked map[string]LockedLayer) (FetchedLayer, []ProvenanceWarning, error) {
	if r.offline {
		prev, ok := prevLocked[entry.Ref]
		if !ok || prev.ResolvedSHA == "" {
			return FetchedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("offline and no resolved SHA in lockfile")}
		}
		data, ok := readCachedLayer(cacheDir, prev.ResolvedSHA)
		if !ok {
			return FetchedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("offline and SHA %s not in cache", prev.ResolvedSHA)}
		}
		warns := []ProvenanceWarning{{FieldPath: entry.Ref, AttemptedByLayer: entry.Ref, Outcome: "cache_hit_offline"}}
		trace.emit(sourceFetchEvent(parts.SourceID, prev.ResolvedSHA, true))
		return FetchedLayer{Data: data, ResolvedSHA: prev.ResolvedSHA, CacheHit: true}, warns, nil
	}
	fetched, err := fetcher.Fetch(src, parts, cacheDir)
	if err != nil {
		return FetchedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: err}
	}
	trace.emit(sourceFetchEvent(parts.SourceID, fetched.ResolvedSHA, fetched.CacheHit))
	return fetched, nil, nil
}

// lockEntry builds the lockfile entry for a resolved layer, computing the TTL
// expiry from the source cache_ttl. An unparseable or absent cache_ttl yields no
// TTL (never auto-re-check), matching spec §7 "absent means never re-check".
func (r *LayeredResolver) lockEntry(src Source, sha string) LockedLayer {
	now := r.clock()
	entry := LockedLayer{ResolvedSHA: sha, FetchedAt: now.UTC().Format(time.RFC3339)}
	if src.CacheTTL != "" {
		if d, err := time.ParseDuration(src.CacheTTL); err == nil && d > 0 {
			entry.TTLExpiresAt = now.Add(d).UTC().Format(time.RFC3339)
		}
	}
	return entry
}

// indexSources maps source id → Source for ref lookup. Sources without an id
// are skipped (they cannot be referenced by an extends ref).
func indexSources(srcs []Source) map[string]Source {
	m := make(map[string]Source, len(srcs))
	for _, s := range srcs {
		if s.ID != "" {
			m[s.ID] = s
		}
	}
	return m
}

// asImportError coerces a resolve error into the *ImportError it almost always
// already is, so config.import.failed events carry the structured reason. Errors
// that are not (or do not wrap) an *ImportError — which the resolver does not
// currently produce on the import path — are wrapped with ReasonContent so the
// event still has a valid reason from the taxonomy rather than an empty one.
func asImportError(ref string, err error) *ImportError {
	var ie *ImportError
	if errors.As(err, &ie) {
		return ie
	}
	return &ImportError{Ref: ref, Reason: ReasonContent, Err: err}
}

// optionalSkipWarning records that an optional extends entry was skipped after a
// fetch failure (spec §11).
func optionalSkipWarning(ref string, cause error) ProvenanceWarning {
	return ProvenanceWarning{FieldPath: ref, AttemptedByLayer: ref, Outcome: "optional_skipped: " + cause.Error()}
}
