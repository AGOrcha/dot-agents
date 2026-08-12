package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	"stage_profiles":        CategoryMapMerge,
	"verifier_profiles":     CategoryMapMerge,
	"reviewer_profiles":     CategoryMapMerge,
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

	// Project-local overlay (§7A.1, Axis A): the gitignored .agentsrc.local.json
	// merges ABOVE repo-local committed (highest local precedence below runtime),
	// so it is the last layer appended. Optional — an absent overlay is the common
	// case; a present-but-corrupt overlay is fatal.
	if overlay, ok, err := loadProjectLocalOverlay(projectPath); err != nil {
		return nil, err
	} else if ok {
		layers = append(layers, overlay)
	}
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

	// §15 D1a policy-authority pass (Phase 1): apply the AUTHORITY-RANK total
	// order, the source-authority registry write-guard, and any surviving
	// value-locks / deny-locks BEFORE the effective config is decoded. It runs
	// here so BOTH the flat and layered resolvers honor it from one path; it is
	// additive (a no-op when no layer declares `locks` or `authority_grants`),
	// so shipped value-merge behavior is unchanged. A self-blessing grant or a
	// force-allow lock aborts the resolve fail-closed.
	collisions, authViols, err := applyAuthority(layers, merged)
	if err != nil {
		return nil, err
	}

	effective, err := decodeEffective(merged)
	if err != nil {
		return nil, err
	}

	snap := &Snapshot{
		Effective:           effective,
		Provenance:          map[string]FieldProvenance{},
		Layers:              layers,
		Warnings:            warnings,
		LockCollisions:      normalizeCollisions(collisions),
		AuthorityViolations: normalizeViolations(authViols),
	}
	snap.populateProvenance()
	return snap, nil
}

// normalizeCollisions guarantees a non-nil slice so the Snapshot marshals
// lock_collisions to [] not null.
func normalizeCollisions(c []LockCollision) []LockCollision {
	if c == nil {
		return []LockCollision{}
	}
	return c
}

// normalizeViolations guarantees a non-nil slice so the Snapshot marshals
// authority_violations to [] not null.
func normalizeViolations(v []AuthorityViolation) []AuthorityViolation {
	if v == nil {
		return []AuthorityViolation{}
	}
	return v
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
	// CacheKey is the effective content cache key the layer resolved at
	// (config-distribution-model §7A.4), derived from the source's cache_keys
	// override and the kind default via EffectiveCacheKey. On a later resolve it is
	// compared against the freshly-derived key (CacheKeyStale): a mismatch — or the
	// AlwaysRevalidate sentinel from a `--refresh` / always_revalidate force escape
	// — means the SHA-addressed cache may no longer be served and the upstream must
	// be re-checked. Omitted for a pre-cache-key lock (treated as stale on read).
	CacheKey string `json:"cache_key,omitempty"`
	// Transitive marks a layer resolved because another layer's OWN `extends`
	// named it (config-transitive-layering), not because this project's own
	// manifest declared it directly. Set by extendsGraphState.walk based on
	// which loop admitted the entry (root rc.Extends vs a recursed childRC.Extends)
	// and carried into LockedUnit.Transitive by writeUnitsLock, so staleness's
	// declared-set comparison (§7A.3) — which only knows the manifest's own
	// top-level extends/packages — does not require a transitively-pulled layer
	// to also appear there.
	Transitive bool
}

// cacheKeyInputs reconstructs the minimal CacheKeyInputs an offline re-derive
// needs from the recorded SHA, so re-running EffectiveCacheKey against the same
// lock yields the same kind-default key (config-distribution-model §7A.4). The
// recorded SHA is the git commit (git), content hash (http/local), or manifest
// digest (oci); it is mirrored into every kind's default field so whichever kind
// reads it gets the recorded value. Offline has no fresh validators or selector
// facts, so override selectors that depend on live inputs derive their "absent"
// contribution — only a force escape (handled separately) flips the result.
func (l LockedLayer) cacheKeyInputs() CacheKeyInputs {
	return CacheKeyInputs{
		ResolvedCommit: l.ResolvedSHA,
		ContentDigest:  l.ResolvedSHA,
		OCIDigest:      l.ResolvedSHA,
	}
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

// readLockedLayersFromUnits is the single offline read of last-resolved layer
// SHAs after the §7A cutover (section-7a-units-lock-wiring). It reads through
// ReadUnits — the one-time on-read migration entry point: a lockfile that
// already carries the authoritative "units" section is read directly; a legacy
// "config"-only lock is transparently upgraded to the units model on this first
// read (migrateLegacyUnits) and then read from units. There is NO permanent
// dual-read of the legacy section — units is the steady state, and the legacy
// section's only remaining reader is the migration step inside ReadUnits.
//
// It is the offline / cache source of last-resolved SHAs for ResolveLocked,
// VerifyLayerLocks, and the offline branch of resolveExtends.
func readLockedLayersFromUnits(projectPath string) (map[string]LockedLayer, error) {
	units, err := ReadUnits(projectPath)
	if err != nil {
		return nil, err
	}
	return layersFromUnits(units.Units), nil
}

// layersFromUnits projects the §7A units map into the LockedLayer shape the
// offline resolver/verify reader consumes, keeping only UnitKindLayer entries
// (artifacts are not extends layers). The unit's content digest is the resolved
// SHA the offline cache is keyed by, and FetchedAt + CacheKey carry through — so
// the §7A.4 cache-key gate (CacheKeyStaleForLayer) keeps working on a units-only
// lock. The clock-based TTLExpiresAt is intentionally NOT carried: §7A staleness
// is content-hash driven (the §7A redesign retired the cache-TTL clock), so a
// units-sourced layer has no TTL.
func layersFromUnits(units map[string]LockedUnit) map[string]LockedLayer {
	layers := make(map[string]LockedLayer, len(units))
	for ref, u := range units {
		if u.Kind != UnitKindLayer {
			continue
		}
		layers[ref] = LockedLayer{
			ResolvedSHA: u.Digest,
			FetchedAt:   u.FetchedAt,
			CacheKey:    u.CacheKey,
		}
	}
	return layers
}

// --- LayeredResolver --------------------------------------------------------

// LayeredResolver extends the FLAT layer set with tier-1 `extends` imports
// (spec §6 pass 1): product defaults → user-local → extends[] (left-to-right,
// fetched over git/http/local/oci) → repo-local. It resolves each extends ref to
// a source (any source type may supply a layer — config-distribution-model §15
// D13), fetches + caches the layer content-addressed by SHA, validates it
// against the layer schema, and records the resolved SHAs to .agentsrc.lock.
type LayeredResolver struct {
	flat *FlatResolver
	// fetchers overrides the per-source-type Fetcher (test seam). When a type
	// is absent, the default SelectFetcher impl is used.
	fetchers map[string]Fetcher
	// offline forces use of the last resolved SHA from the lockfile instead of
	// contacting any source; a cache_hit_offline warning is emitted per layer.
	offline bool
	// refresh is the per-resolve `--refresh` force escape (config-distribution-
	// model §7A.4 / R6): when true every source's effective cache key becomes the
	// AlwaysRevalidate sentinel, so the offline cache may not be served and a fresh
	// resolve re-checks upstream regardless of the recorded key.
	refresh bool
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

// WithRefresh toggles the per-resolve `--refresh` force escape: when true every
// source's effective cache key becomes AlwaysRevalidate, so the offline cache is
// never served and an online resolve re-checks upstream unconditionally
// (config-distribution-model §7A.4 / R6). Returns the receiver for chaining.
func (r *LayeredResolver) WithRefresh(refresh bool) *LayeredResolver {
	r.refresh = refresh
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

	// Project-local overlay (§7A.1, Axis A): merges above repo-local committed and
	// the imported extends layers — the highest local precedence below runtime —
	// so it is appended last.
	if overlay, ok, err := loadProjectLocalOverlay(projectPath); err != nil {
		return nil, err
	} else if ok {
		stack = append(stack, overlay)
	}

	snap, err := resolveSnapshot(stack)
	if err != nil {
		return nil, err
	}
	snap.Warnings = append(snap.Warnings, importWarnings...)

	// Source-qualified prompt files (stage-profile-and-routing-consolidation
	// § Source-Qualified Prompt Files): the effective config is now known, so the
	// prompt files its stage_profiles pin to a config source are fetched HERE —
	// on the one network-touching path — and pinned as UnitKindPrompt units below.
	// This is what makes `da workflow resolve-prompt` able to stay offline while
	// still resolving a team-layer prompt: the bytes are already cached and the
	// digest is already in the lock.
	promptUnits, promptWarnings, err := r.promptUnits(trace, projectPath, snap)
	if err != nil {
		return nil, err
	}
	snap.Warnings = append(snap.Warnings, promptWarnings...)

	// Field-level audit derives from the produced snapshot so the shared merge
	// core (resolveSnapshot) stays unchanged: overrides come from the provenance
	// stacks, protection violations from the recorded warnings.
	emitFieldEvents(trace, snap)
	trace.emit(effectiveProducedEvent(repoLayerID(snap), len(snap.Layers)))

	// §7A units-lock cutover (section-7a-units-lock-wiring): the units section +
	// inputs_digest is now the AUTHORITATIVE lock. A single resolve writes it
	// from one path here in Resolve — not split across EnsureResolved — so EVERY
	// resolve caller (`da config sync`, `da install`, and the EnsureResolved
	// stale-repair path that drives this same Resolve) emits the §7A lock model
	// uniformly. Writing it here is what removes the stale-repair split-brain:
	// staleness reads the units section + inputs_digest, so the resolve that
	// repairs a stale lock must write exactly those. The legacy §7 `config`
	// section is no longer written — units-only is the steady state; a legacy
	// config-only lock is upgraded once on read (see ReadUnits).
	if err := r.writeUnitsLock(projectPath, snap, locked, promptUnits); err != nil {
		return nil, fmt.Errorf("writing %s units: %w", AgentsLockFile, err)
	}
	return snap, nil
}

// writeUnitsLock writes the authoritative §7A lock — the units section +
// inputs_digest — from the resolved-layer set Resolve just produced. The units
// map mirrors every resolved `extends` layer as a UnitKindLayer (its resolved
// SHA → digest, its fetch timestamps carried verbatim); inputs_digest is the
// whole-normalized hash of the local config scopes computed against the
// resolver's own user-local seam, so a later staleness check compares
// like-for-like. A flat/local-only project (no extends) still gets a lockfile
// carrying a non-empty inputs_digest and an empty units map — the property §7A
// wires in.
func (r *LayeredResolver) writeUnitsLock(projectPath string, snap *Snapshot, locked map[string]LockedLayer, promptUnits map[string]LockedUnit) error {
	digest, err := ComputeInputsDigest(projectPath, r.effectiveUserLocalPath())
	if err != nil {
		return err
	}
	layerUnits := make(map[string]LockedUnit, len(locked))
	for ref, l := range locked {
		layerUnits[ref] = LockedUnit{
			Kind:       UnitKindLayer,
			Digest:     l.ResolvedSHA,
			CacheKey:   l.CacheKey,
			Transitive: l.Transitive,
		}
	}
	// kind:profile units (R2): the resolved profile fragments are recorded as
	// first-class lock units so a profile resolution reproduces from the lock
	// without re-resolving. They are derived from the SAME snapshot the resolve just
	// produced (no divergent re-derivation) and timestamped on the resolver's clock
	// seam (r.clock(), test-overridable) — not a fresh time.Now() — so the lock is
	// byte-stable under a fixed clock. A malformed layering_policy / authored profile
	// fails the resolve closed here (R9), exactly as `da config explain` does, rather
	// than silently writing a lock that omits the unit.
	profileUnits, err := ProfileUnitsForSnapshot(snap, r.clock())
	if err != nil {
		return err
	}
	// Cross-pass lock atomicity + lost-update fix (package-artifact-install t3
	// review #3): pass 1 resolves ONLY layers/profiles, but it must NOT drop the
	// kind:artifact units the packages pass (pass 2, EnsureResolved's caller)
	// recorded — and it must read them UNDER THE SAME LOCK it writes under, so a
	// pass 2 that committed artifact units between our resolve and this write is
	// not clobbered by a stale snapshot. agentslock.Update holds the advisory
	// lock across the read-modify-write; the artifact units read here are the
	// latest committed, and pass 2's own combined write (commitArtifactLock) is
	// symmetrically serialized, so interleaved pass-1/pass-2 writers preserve
	// each other's keys instead of losing them.
	return agentslock.Update(AgentsLockPath(projectPath), func(lf *agentslock.Lockfile) error {
		existing := map[string]LockedUnit{}
		if _, err := lf.Section(LockSectionUnits, &existing); err != nil {
			return err
		}
		merged := mergeLockUnits(existing, layerUnits, promptUnits, profileUnits)
		if err := lf.SetSection(LockSectionUnits, merged); err != nil {
			return err
		}
		lf.SetInputsDigest(digest)
		return nil
	})
}

// mergeLockUnits builds the merged §7A units map: it preserves the
// kind:artifact units a concurrent packages pass committed (read under the
// same lock), overwrites with the freshly resolved layer units, then the
// source-qualified prompt units, and finally applies the profile units. A
// profile key never collides with a layer/artifact ref; on the impossible
// collision the profile entry wins (mirrors UnitsLock.allUnits).
//
// promptUnits is pass 1's OWN derived set (from the effective config's
// stage_profiles), so it replaces the previous prompt set wholesale — a
// prompt_files entry deleted from the config drops out of the lock. Entries the
// resolver could not re-fetch were already carried forward into promptUnits by
// promptUnits(), so a transient failure does not un-pin a working prompt.
func mergeLockUnits(existing, layerUnits, promptUnits, profileUnits map[string]LockedUnit) map[string]LockedUnit {
	merged := make(map[string]LockedUnit, len(layerUnits)+len(promptUnits)+len(profileUnits))
	for ref, u := range existing {
		if u.Kind == UnitKindArtifact {
			merged[ref] = u
		}
	}
	for ref, u := range layerUnits {
		merged[ref] = u
	}
	for ref, u := range promptUnits {
		merged[ref] = u
	}
	for key, u := range profileUnits {
		merged[key] = u
	}
	return merged
}

// effectiveUserLocalPath returns the user-local manifest path the resolver
// resolves against (the test seam when set, else the default
// <AgentsHome>/.agentsrc.json), so inputs_digest is computed over the exact
// same user-local scope the layer stack merged — staleness and resolution can
// never disagree on which user-local file is authoritative.
func (r *LayeredResolver) effectiveUserLocalPath() string {
	return r.flat.userLocalPath
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

// resolveExtends resolves the repo-local manifest's `extends` array into the
// imported layer stack, expanding each layer's OWN declared sources/extends
// transitively (org→team→repo-local). Returns the imported ResolvedLayers in
// precedence order (children before parents), the lockfile entries for every
// transitive unit, and non-fatal warnings. Thin wrapper over the recursive
// engine so the Resolve() caller is unchanged.
func (r *LayeredResolver) resolveExtends(trace auditTrace, projectPath string, repoRaw map[string]any) ([]ResolvedLayer, map[string]LockedLayer, []ProvenanceWarning, error) {
	return r.resolveExtendsGraph(trace, projectPath, repoRaw)
}

// sourceEnv is a layer-local source environment: the sources visible to a layer
// and, by inheritance, its descendants. A child env is the parent env overlaid
// with the layer's own declared `sources`, so a team layer can name a private
// org source the consuming repo never declares (org-config-resolution §6.6). A
// layer-local declaration shadows an inherited same-id source.
type sourceEnv map[string]Source

func (e sourceEnv) child(layerSources []Source) sourceEnv {
	if len(layerSources) == 0 {
		return e
	}
	c := make(sourceEnv, len(e)+len(layerSources))
	for k, v := range e {
		c[k] = v
	}
	for k, v := range indexSources(layerSources) {
		c[k] = v
	}
	return c
}

// extendsGraphState is the mutable state threaded through the recursive
// children-first walk of the transitive extends graph.
type extendsGraphState struct {
	trace      auditTrace
	prevLocked map[string]LockedLayer
	imported   []ResolvedLayer
	locked     map[string]LockedLayer
	// digestByRef dedupes by canonical ref → resolved digest. A ref reached
	// twice at the SAME digest is admitted once (first/highest-precedence wins);
	// the same ref at a DIFFERENT digest is a hard error (ambiguous policy).
	digestByRef map[string]string
	// onStack is the active recursion stack for cycle detection (A→B→A).
	onStack  map[string]bool
	warnings []ProvenanceWarning
}

// resolveExtendsGraph is the recursive engine. It decodes the repo manifest,
// then walks each root `extends` entry depth-first, expanding every fetched
// layer's own sources/extends BEFORE admitting the layer itself, so the imported
// stack is post-order (org before team before repo-local) and every transitive
// unit is locked (org-config-resolution §6.6, payout platform-config-layering
// § Transitive Extends Api).
func (r *LayeredResolver) resolveExtendsGraph(trace auditTrace, projectPath string, repoRaw map[string]any) ([]ResolvedLayer, map[string]LockedLayer, []ProvenanceWarning, error) {
	rc, err := decodeEffective(repoRaw)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decoding repo manifest: %w", err)
	}
	st := &extendsGraphState{
		trace:       trace,
		locked:      map[string]LockedLayer{},
		digestByRef: map[string]string{},
		onStack:     map[string]bool{},
	}
	if len(rc.Extends) == 0 {
		return nil, st.locked, nil, nil
	}
	// Read the prior lock on every resolve (online consults recorded cache keys;
	// offline serves the locked SHA). A missing/legacy lock yields an empty map.
	prevLocked, err := readLockedLayersFromUnits(projectPath)
	if err != nil {
		return nil, nil, nil, err
	}
	st.prevLocked = prevLocked
	rootEnv := sourceEnv(indexSources(rc.Sources))
	for _, entry := range rc.Extends {
		if err := st.walk(r, entry, rootEnv, true); err != nil {
			trace.emit(importFailedEvent(asImportError(entry.Ref, err), entry.Optional))
			if entry.Optional {
				st.warnings = append(st.warnings, optionalSkipWarning(entry.Ref, err))
				continue
			}
			return nil, nil, nil, err
		}
	}
	return st.imported, st.locked, st.warnings, nil
}

// walk resolves one extends entry and, children-first, its transitive extends.
// The layer is appended to the imported stack AFTER its own extends, giving
// post-order precedence (a declared org layer lands before the team layer that
// extends it). Dedupe and cycle detection guard against divergent-digest
// re-resolution and extends loops.
//
// isRoot is true only for entries admitted from the outer loop in
// resolveExtendsGraph — i.e. named directly in THIS project's manifest — and
// false for every entry reached by recursing into a childRC.Extends (a layer
// pulled in transitively because another layer's own extends named it). It is
// stamped onto the LockedLayer as Transitive (=!isRoot) so the lock records
// which units the manifest actually declares (config-transitive-layering);
// staleness's declared-set comparison reads that back to avoid requiring a
// transitively-pulled layer to also appear in the manifest's own extends list.
func (st *extendsGraphState) walk(r *LayeredResolver, entry LayerRef, env sourceEnv, isRoot bool) error {
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
	layer, lock, warns, err := r.resolveOneLayer(st.trace, entry, env, st.prevLocked)
	st.warnings = append(st.warnings, warns...)
	if err != nil {
		return err
	}
	lock.Transitive = !isRoot
	if prev, seen := st.digestByRef[key]; seen {
		if prev != lock.ResolvedSHA {
			return &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonContent, Err: fmt.Errorf("ref %q resolves to conflicting digests (%s vs %s); refusing to merge ambiguous policy", entry.Ref, prev, lock.ResolvedSHA)}
		}
		// Already satisfied at the same digest: first occurrence keeps precedence.
		return nil
	}
	st.digestByRef[key] = lock.ResolvedSHA
	// Expand the layer's OWN sources/extends in a child env, children first.
	childRC, err := decodeEffective(layer.Raw)
	if err != nil {
		return &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonSchema, Err: fmt.Errorf("decoding layer %q sources/extends: %w", entry.Ref, err)}
	}
	childEnv := env.child(childRC.Sources)
	st.onStack[key] = true
	for _, child := range childRC.Extends {
		if err := st.walk(r, child, childEnv, false); err != nil {
			st.trace.emit(importFailedEvent(asImportError(child.Ref, err), child.Optional))
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
	st.locked[entry.Ref] = lock
	return nil
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
	// Source selection (config-distribution-model §15 D13): any source type —
	// git, http, local, or oci — may supply a config layer. An oci layer is
	// guarded by the config-layer media type inside its fetcher, not rejected
	// here. An unsupported source type still surfaces as a schema error.
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
	lock := r.lockEntry(src, fetched)
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
		// Cache-key staleness gate (config-distribution-model §7A.4). Offline cannot
		// re-derive a source's live validators (an http ETag, a local worktree
		// hash), so a recorded-vs-recomputed mismatch is unreliable here; only a
		// force escape — `--refresh` or the source's always_revalidate marker, both
		// of which make the effective key the AlwaysRevalidate sentinel — blocks an
		// offline serve. The caller explicitly asked to revalidate, and offline
		// cannot, so it fails loudly rather than silently serving stale content.
		if r.effectiveCacheKey(src, prev.cacheKeyInputs()) == AlwaysRevalidate {
			return FetchedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("offline and cache key revalidation required for %s", entry.Ref)}
		}
		data, ok := readCachedLayer(cacheDir, prev.ResolvedSHA)
		if !ok {
			return FetchedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("offline and SHA %s not in cache", prev.ResolvedSHA)}
		}
		warns := []ProvenanceWarning{{FieldPath: entry.Ref, AttemptedByLayer: entry.Ref, Outcome: "cache_hit_offline"}}
		trace.emit(sourceFetchEvent(parts.SourceID, prev.ResolvedSHA, true))
		return FetchedLayer{Data: data, ResolvedSHA: prev.ResolvedSHA, CacheHit: true, KeyInputs: prev.cacheKeyInputs()}, warns, nil
	}
	// Cache-key consumption online (config-distribution-model §7A.4 / R6): when a
	// previously-locked layer's recorded cache key is now stale — a `--refresh` /
	// always_revalidate force escape, or a cache_keys override edit that changed
	// the key shape — force the fetcher to bypass its SHA-addressed cache serve so
	// the upstream is actually re-validated. Without this the recorded key would be
	// written but never acted on online, leaving cache_keys a silent no-op on the
	// resolve path. A first resolve (no prior lock) has nothing to compare and
	// fetches normally.
	forceRefresh := false
	if prev, ok := prevLocked[entry.Ref]; ok && prev.ResolvedSHA != "" {
		forceRefresh = r.CacheKeyStaleForLayer(src, prev)
	}
	fetched, err := fetchWithRefresh(fetcher, src, parts, cacheDir, forceRefresh)
	if err != nil {
		// A fetcher that already classified the failure (e.g. the oci media-type
		// guard's schema error) keeps its reason; otherwise the cause is treated
		// as a transport failure.
		var ie *ImportError
		if errors.As(err, &ie) {
			return FetchedLayer{}, nil, err
		}
		return FetchedLayer{}, nil, &ImportError{Ref: entry.Ref, SourceID: parts.SourceID, Reason: ReasonTransport, Err: err}
	}
	trace.emit(sourceFetchEvent(parts.SourceID, fetched.ResolvedSHA, fetched.CacheHit))
	return fetched, nil, nil
}

// lockEntry builds the lockfile entry for a resolved layer, computing the TTL
// expiry from the source cache_ttl and the effective content cache key from the
// source's cache_keys override + the fetched facts. An unparseable or absent
// cache_ttl yields no TTL (never auto-re-check), matching spec §7 "absent means
// never re-check". The recorded CacheKey is what a later resolve compares against
// (CacheKeyStale) to decide whether the SHA-addressed cache may be served.
func (r *LayeredResolver) lockEntry(src Source, fetched FetchedLayer) LockedLayer {
	now := r.clock()
	entry := LockedLayer{
		ResolvedSHA: fetched.ResolvedSHA,
		FetchedAt:   now.UTC().Format(time.RFC3339),
		CacheKey:    r.effectiveCacheKey(src, withResolvedSHA(fetched.KeyInputs, fetched.ResolvedSHA)),
	}
	if src.CacheTTL != "" {
		if d, err := time.ParseDuration(src.CacheTTL); err == nil && d > 0 {
			entry.TTLExpiresAt = now.Add(d).UTC().Format(time.RFC3339)
		}
	}
	return entry
}

// effectiveCacheKey derives the source's effective content cache key
// (config-distribution-model §7A.4) from its declared cache_keys override, the
// kind default for its type, and the facts the fetcher observed, threading the
// resolver-level `--refresh` force escape through CacheKeyOptions. It is the
// single point the resolver consults EffectiveCacheKey, so the recorded key and
// the offline-serve staleness check derive identically.
//
// Before deriving, it gathers the live facts for the source's declared {env} and
// {dir} selectors (the fetcher cannot know which env vars / dirs a source pins),
// so an override that keys on a credential, feature flag, or marker dir actually
// re-checks when that value changes — the consumption point that makes a non-
// default cache_keys stop being a silent no-op.
func (r *LayeredResolver) effectiveCacheKey(src Source, in CacheKeyInputs) string {
	in = gatherOverrideFacts(src.CacheKeys, in)
	return EffectiveCacheKey(SourceKindOf(src.Type), src.CacheKeys, in, CacheKeyOptions{Refresh: r.refresh})
}

// withResolvedSHA backfills the kind-primary cache-key facts from the resolved
// SHA when the fetcher left them empty (config-distribution-model §7A.4). The
// ResolvedSHA IS the git commit / http+local content hash / oci manifest digest,
// so it is the authoritative content identity for every kind default; mirroring
// it into each kind's primary field lets the resolver derive a stable, non-empty
// key even from a fetcher that only reports ResolvedSHA. Already-populated facts
// (e.g. a fetcher's ETag or precise worktree hash) are preserved.
func withResolvedSHA(in CacheKeyInputs, sha string) CacheKeyInputs {
	if sha == "" {
		return in
	}
	if in.ResolvedCommit == "" {
		in.ResolvedCommit = sha
	}
	if in.ContentDigest == "" {
		in.ContentDigest = sha
	}
	if in.OCIDigest == "" {
		in.OCIDigest = sha
	}
	return in
}

// gatherOverrideFacts folds the live values of a source's declared {env} and
// {dir} cache-key selectors into the inputs, leaving already-populated facts
// (e.g. the fetcher's FileContents) untouched. Env values are read from the
// process environment and dir presence from the filesystem — the only override
// selectors whose facts the resolver, not the fetcher, must supply. A nil/empty
// override returns the inputs unchanged.
func gatherOverrideFacts(keys *CacheKeys, in CacheKeyInputs) CacheKeyInputs {
	if keys.IsZero() {
		return in
	}
	if len(keys.Env) > 0 {
		env := map[string]string{}
		for _, name := range keys.Env {
			if v, ok := os.LookupEnv(name); ok {
				env[name] = v
			}
		}
		in.EnvValues = env
	}
	if len(keys.Dir) > 0 {
		present := map[string]bool{}
		for _, dir := range keys.Dir {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				present[dir] = true
			}
		}
		in.DirPresent = present
	}
	return in
}

// CacheKeyStaleForLayer reports whether the lock's recorded cache key for a
// resolved layer is stale (config-distribution-model §7A.4) given the source's
// CURRENT declared cache_keys, without contacting the upstream. It re-derives the
// effective key from the recorded SHA facts plus the live override and compares
// it against the recorded key via CacheKeyStale, so it detects:
//
//   - a `--refresh` / always_revalidate force escape (AlwaysRevalidate is always
//     stale); and
//   - a cache_keys override edit that changes the key shape even from the same
//     SHA facts — e.g. adding an {env}/{git:tags} selector switches a source from
//     the kind default to a composite override key, which no longer matches.
//
// Live-validator drift an http ETag or a local worktree edit would cause is not
// detectable here (those facts are not in the lock); that axis is the online
// re-fetch's job. fetchLayer consults this on the online resolve path to force a
// cache-bypassing re-fetch when the recorded key is stale, and doctor/sync use it
// to nudge a re-resolve — it is the consumer that activates staleness.go's
// CacheKeyStale on the cache-key axis.
func (r *LayeredResolver) CacheKeyStaleForLayer(src Source, locked LockedLayer) bool {
	effective := r.effectiveCacheKey(src, locked.cacheKeyInputs())
	return CacheKeyStale(locked.CacheKey, effective)
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
