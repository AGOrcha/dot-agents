package config

// source_plan.go exports the PROVENANCE-PRESERVING resource-source plan that
// `da install` materializes a project's skills/agents from.
//
// Why a plan and not `Snapshot.Effective.Sources`:
//
// `sources` is CategoryOrderedReplace (resolver.go/fieldCategories) — the
// highest-precedence layer that declares the key replaces the array WHOLESALE.
// The effective array therefore answers "which sources does the winning layer
// name", never "which source roots did this layer stack make reachable, and who
// declared each one". Substituting it for the raw repo list would silently
// (a) DROP every source an org/team layer declared whenever the repo declares
// any of its own, and (b) ADOPT user-local sources as project resource roots
// when the repo declares none — exactly the user-resource projection invariant
// install must not violate.
//
// The plan is built from the PER-LAYER raw declarations the snapshot already
// carries (Snapshot.Layers), so source ancestry survives the merge: each entry
// records the layer that declared it, whether it arrived through an imported
// `extends` layer, and whether it may supply PROJECT-scope resources.

import (
	"encoding/json"
	"fmt"
)

// Ineligibility reasons recorded on a ResourceSource that the project resource
// materialization pass must not search. They are stable strings so callers can
// explain a skipped root without re-deriving the classification.
const (
	// IneligibleUserLayer marks a source declared by the user-local or
	// product-defaults layer. Those layers describe the MACHINE, not the
	// project: resources they own reach each platform through the canonical
	// user/global scoped projection (internal/platform resolveScopedFile), so
	// re-linking them into ~/.agents/<bucket>/<project> would duplicate a user
	// resource into project scope.
	IneligibleUserLayer = "declared by a user-scope layer"
	// IneligibleDefaultHome marks the path-less `{"type":"local"}` sentinel.
	// It resolves to config.AgentsHome() rather than to anything the project
	// owns, and install already searches the home store unconditionally, so
	// admitting it as a declared root only lets a user-home resource SHADOW a
	// project/layer resource of the same name.
	IneligibleDefaultHome = "synthesized default user-home root"
)

// ResourceSource is one source root a resolved layer stack made visible to the
// project, carried together with the provenance install needs to decide whether
// it may supply project-scope resources.
type ResourceSource struct {
	// Source is the declaration exactly as authored in its layer.
	Source Source `json:"source"`
	// Layer is the identifier of the layer that DECLARED this source:
	// LayerRepoLocal, LayerProjectLocal, LayerUserLocal, LayerProductDefaults,
	// or an imported layer's extends ref (e.g. "team:policies/base.json").
	Layer string `json:"layer"`
	// Inherited is true when the declaring layer is an imported `extends`
	// layer rather than one of this project's own local layers — i.e. the
	// source reached the project transitively through the layer graph.
	Inherited bool `json:"inherited"`
	// ProjectEligible reports whether this root may supply PROJECT-scope
	// resource materialization. False entries stay in the plan (so callers can
	// explain them) but are excluded from ProjectSources.
	ProjectEligible bool `json:"project_eligible"`
	// IneligibleReason explains a false ProjectEligible; empty when eligible.
	IneligibleReason string `json:"ineligible_reason,omitempty"`
}

// ResourceSourcePlan is the ordered, de-duplicated, provenance-preserving set
// of source roots one resolve made reachable. Sources are ordered by SEARCH
// precedence — highest-precedence declaring layer first — so the first match
// for a resource name is the one the layer stack says wins.
type ResourceSourcePlan struct {
	Sources []ResourceSource `json:"sources"`
}

// ProjectSources returns, in search order, the declarations that may supply
// project-scope resource materialization. This is the ONLY view install
// resolves roots from.
func (p ResourceSourcePlan) ProjectSources() []Source {
	out := make([]Source, 0, len(p.Sources))
	for _, rs := range p.Sources {
		if rs.ProjectEligible {
			out = append(out, rs.Source)
		}
	}
	return out
}

// ResourceSourcePlan derives the plan from the snapshot's per-layer raw
// declarations. It works identically for an online resolve and for a
// ResolveLocked/offline replay, because both assemble the same layer stack —
// the offline walk reconstructs transitively-locked layers from the cache, so
// an inherited org source survives an offline install.
//
// Layers are walked highest precedence first. A source ID declared by more than
// one layer is admitted ONCE, from the highest-precedence declaration: a
// repo-local `{"id":"team", …}` overrides the same id inherited from a layer,
// mirroring sourceEnv.child's layer-local shadowing. An ID-less v1 source
// de-duplicates structurally (type+url+ref+path) so the same root declared by
// two layers is not searched twice.
func (s *Snapshot) ResourceSourcePlan() (ResourceSourcePlan, error) {
	var plan ResourceSourcePlan
	seen := map[string]struct{}{}
	for i := len(s.Layers) - 1; i >= 0; i-- {
		layer := s.Layers[i]
		declared, err := layerSources(layer)
		if err != nil {
			return ResourceSourcePlan{}, err
		}
		for _, rs := range classifyResourceSources(layer.ID, declared) {
			key := resourceSourceKey(rs.Source)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			plan.Sources = append(plan.Sources, rs)
		}
	}
	return plan, nil
}

// RepoResourceSourcePlan is the single-layer plan for a repo manifest that has
// no resolved snapshot to read — `da install --dry-run` on a project whose lock
// has never been written. It applies the same eligibility rules to the repo's
// own declarations, so a preview never resolves a root the real install would
// refuse.
func RepoResourceSourcePlan(rc *AgentsRC) ResourceSourcePlan {
	if rc == nil {
		return ResourceSourcePlan{}
	}
	return ResourceSourcePlan{Sources: classifyResourceSources(LayerRepoLocal, rc.Sources)}
}

// classifyResourceSources stamps provenance and project eligibility onto one
// layer's declared sources.
func classifyResourceSources(layerID string, declared []Source) []ResourceSource {
	out := make([]ResourceSource, 0, len(declared))
	for _, src := range declared {
		rs := ResourceSource{
			Source:          src,
			Layer:           layerID,
			Inherited:       isImportedLayer(layerID),
			ProjectEligible: true,
		}
		switch {
		case isUserScopeLayer(layerID):
			rs.ProjectEligible = false
			rs.IneligibleReason = IneligibleUserLayer
		case src.IsDefaultHomeLocal():
			rs.ProjectEligible = false
			rs.IneligibleReason = IneligibleDefaultHome
		}
		out = append(out, rs)
	}
	return out
}

// isUserScopeLayer reports whether a layer describes the machine rather than
// the project. Its sources are never project resource roots.
func isUserScopeLayer(layerID string) bool {
	return layerID == LayerUserLocal || layerID == LayerProductDefaults
}

// isImportedLayer reports whether a layer arrived through `extends`. The four
// local layer identifiers are fixed; every other identifier in a resolved stack
// is an imported layer's ref.
func isImportedLayer(layerID string) bool {
	switch layerID {
	case LayerProductDefaults, LayerUserLocal, LayerRepoLocal, LayerProjectLocal:
		return false
	default:
		return true
	}
}

// layerSources decodes one layer's raw `sources` array. An absent key yields no
// sources; a malformed one is a hard error rather than a silently dropped root,
// so an unreadable declaration can never shrink the search path unnoticed.
func layerSources(layer ResolvedLayer) ([]Source, error) {
	raw, ok := layer.Raw["sources"]
	if !ok || raw == nil {
		return nil, nil
	}
	// raw came from a decoded JSON object, so re-encoding cannot fail (same
	// impossible-marshal convention as appendLayerProfiles/WriteUnitsLock).
	encoded, _ := json.Marshal(raw)
	var srcs []Source
	if err := json.Unmarshal(encoded, &srcs); err != nil {
		return nil, fmt.Errorf("decoding %q layer sources: %w", layer.ID, err)
	}
	return srcs, nil
}

// resourceSourceKey is the de-duplication identity: the declared ID when there
// is one (the config model's stable local identifier), else the root the source
// actually points at.
func resourceSourceKey(src Source) string {
	if src.ID != "" {
		return "id:" + src.ID
	}
	return "root:" + src.Type + "\x00" + src.URL + "\x00" + src.Ref + "\x00" + src.Path
}
