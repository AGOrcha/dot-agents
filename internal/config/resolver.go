package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// features, kg.
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
