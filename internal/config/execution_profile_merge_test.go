package config

import (
	"encoding/json"
	"testing"
)

// This file holds the Defect B (round-trip loss) validation for the
// lens-rejected `da config relevance --recompute --write` work (PR #21). It is
// written to FAIL if the proposed relevance-only profile-layer diff wipes the
// topology and lenses facets when merged over a base profile, then kept as a
// permanent regression test. See .agents/proposals/skill-relevance-filter.md §2
// ("the three facets ... each independently overridable") and §6.

// asLayerRaw marshals a typed ExecutionProfile into the generic
// map[string]any{"execution_profile": ...} a config layer carries, mirroring how
// the resolver sees a layer's Raw body.
func asLayerRaw(t *testing.T, p *ExecutionProfile) any {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal profile to generic: %v", err)
	}
	return out
}

// decodeMergedProfile decodes the merged generic execution_profile value back
// into a typed ExecutionProfile, the same round-trip resolveSnapshot performs.
func decodeMergedProfile(t *testing.T, merged any) *ExecutionProfile {
	t.Helper()
	data, err := json.Marshal(merged)
	if err != nil {
		t.Fatalf("marshal merged: %v", err)
	}
	var p ExecutionProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal merged profile: %v", err)
	}
	return &p
}

// TestExecutionProfileLayer_ProposedDiffPreservesTopologyAndLenses is the
// Defect B validation. A base profile carries all three facets for an app_type;
// a higher-precedence layer carries ONLY the relevance facet (exactly what the
// recompute proposed-layer builder emits). Merging the two through the real
// resolver merge path must PRESERVE the base topology and lenses — only the
// relevance facet should change. If execution_profile merges last-writer-wins
// wholesale, the proposed diff blows away topology + lenses and this test fails.
func TestExecutionProfileLayer_ProposedDiffPreservesTopologyAndLenses(t *testing.T) {
	base := &ExecutionProfile{
		ByAppType: map[string]AppTypeProfile{
			"go-cli": {
				Relevance: map[string]RelevanceClasses{
					"review": {
						Core:        []string{"review-pr"},
						Situational: []string{"self-review"},
					},
				},
				Topology: Topology{
					Executors:            1,
					VerifiersPerExecutor: 2,
					Reviewers:            "per_verifier",
					VerifierSequence:     []string{"unit", "cli-runner"},
				},
				Lenses: Lenses{
					LensSet:         []string{"architecture-standards", "adversarial"},
					LensConcurrency: "gated",
				},
			},
		},
		DefaultClass: "situational",
	}

	// The proposed diff: relevance-only, exactly as the recompute writer emits
	// it (topology and lenses are omitempty -> absent from the JSON entirely).
	proposed := &ExecutionProfile{
		ByAppType: map[string]AppTypeProfile{
			"go-cli": {
				Relevance: map[string]RelevanceClasses{
					"review": {
						Core: []string{"review-pr", "self-review"}, // promoted self-review
					},
				},
			},
		},
		DefaultClass: "situational",
	}

	merged := mergeField("execution_profile", asLayerRaw(t, base), asLayerRaw(t, proposed))
	got := decodeMergedProfile(t, merged)

	prof, ok := got.ByAppType["go-cli"]
	if !ok {
		t.Fatalf("merged profile lost the go-cli app_type entirely")
	}

	// Topology must survive the relevance-only diff.
	if prof.Topology.Executors != 1 ||
		prof.Topology.VerifiersPerExecutor != 2 ||
		prof.Topology.Reviewers != "per_verifier" ||
		len(prof.Topology.VerifierSequence) != 2 {
		t.Fatalf("DEFECT B reproduced: relevance-only diff wiped topology; got %+v", prof.Topology)
	}

	// Lenses must survive the relevance-only diff.
	if len(prof.Lenses.LensSet) != 2 || prof.Lenses.LensConcurrency != "gated" {
		t.Fatalf("DEFECT B reproduced: relevance-only diff wiped lenses; got %+v", prof.Lenses)
	}

	// And the relevance facet — the one thing the diff targets — must apply.
	reviewClasses := prof.Relevance["review"]
	if len(reviewClasses.Core) != 2 {
		t.Fatalf("expected the relevance diff to apply (self-review promoted to core); got core=%v",
			reviewClasses.Core)
	}
}

// modelLayer builds a single-app_type execution_profile layer carrying only the
// model facet — the shape a scope layer that pins nothing but model emits.
func modelLayer(appType, model string) *ExecutionProfile {
	return &ExecutionProfile{
		ByAppType: map[string]AppTypeProfile{appType: {Model: model}},
	}
}

// mergeExecutionLayers merges an ordered low→high precedence layer list through
// the real resolver merge path and decodes the result, so a layering assertion
// reads as one call instead of a nested mergeField chain.
func mergeExecutionLayers(t *testing.T, layers ...*ExecutionProfile) *ExecutionProfile {
	t.Helper()
	var merged any
	for _, l := range layers {
		merged = mergeField("execution_profile", merged, asLayerRaw(t, l))
	}
	return decodeMergedProfile(t, merged)
}

// modelOf returns the merged model route for an app_type, failing the test when
// the app_type was lost by the merge.
func modelOf(t *testing.T, ep *ExecutionProfile, appType string) string {
	t.Helper()
	prof, ok := ep.ByAppType[appType]
	if !ok {
		t.Fatalf("merged profile lost app_type %q entirely", appType)
	}
	return prof.ModelRef()
}

// TestExecutionProfileLayer_ModelFacetLayering is the facet-5 layering proof
// (.agents/proposals/model-facet-apptypeprofile.md §D3, rung 2): an org value is
// INHERITED by a higher-precedence layer that omits model, and OVERRIDDEN by one
// that sets it. Absence must not blank the value below — the same guarantee
// topology/lenses already carry.
func TestExecutionProfileLayer_ModelFacetLayering(t *testing.T) {
	org := modelLayer("go-cli", "sonnet")

	t.Run("omitted at higher scope inherits", func(t *testing.T) {
		// The repo layer touches only topology; model must survive from org.
		repo := &ExecutionProfile{ByAppType: map[string]AppTypeProfile{
			"go-cli": {Topology: Topology{Executors: 2}},
		}}
		if got := modelOf(t, mergeExecutionLayers(t, org, repo), "go-cli"); got != "sonnet" {
			t.Errorf("model = %q, want sonnet inherited from the org layer", got)
		}
	})

	t.Run("explicit at higher scope overrides", func(t *testing.T) {
		team := modelLayer("go-cli", "opus")
		if got := modelOf(t, mergeExecutionLayers(t, org, team), "go-cli"); got != "opus" {
			t.Errorf("model = %q, want opus from the higher-precedence layer", got)
		}
	})

	t.Run("unpinned app_type stays unpinned", func(t *testing.T) {
		merged := mergeExecutionLayers(t, org, modelLayer("docs", "haiku"))
		if got := modelOf(t, merged, "docs"); got != "haiku" {
			t.Errorf("docs model = %q, want haiku", got)
		}
		if got := modelOf(t, merged, "go-cli"); got != "sonnet" {
			t.Errorf("go-cli model = %q, want sonnet (a sibling app_type must not leak)", got)
		}
	})
}

// TestExecutionProfileLayer_ModelOnlyDiffPreservesOtherFacets is the Defect-B
// guarantee extended to facet 5: a layer carrying ONLY model must not wipe that
// app_type's topology, lenses, or graph_backend — each facet is independently
// scope-overridable.
func TestExecutionProfileLayer_ModelOnlyDiffPreservesOtherFacets(t *testing.T) {
	base := &ExecutionProfile{ByAppType: map[string]AppTypeProfile{
		"go-cli": {
			Topology:     Topology{Executors: 1, VerifierSequence: []string{"unit", "cli-runner"}},
			Lenses:       Lenses{LensSet: []string{"adversarial"}, LensConcurrency: "gated"},
			GraphBackend: "dotagents-builtin:graph/none@^1.0",
		},
	}}

	merged := mergeExecutionLayers(t, base, modelLayer("go-cli", "haiku"))
	prof := merged.ByAppType["go-cli"]

	if prof.ModelRef() != "haiku" {
		t.Errorf("model diff did not apply: %q", prof.ModelRef())
	}
	if prof.Topology.Executors != 1 || len(prof.Topology.VerifierSequence) != 2 {
		t.Errorf("model-only diff wiped topology: %+v", prof.Topology)
	}
	if prof.Lenses.LensConcurrency != "gated" || len(prof.Lenses.LensSet) != 1 {
		t.Errorf("model-only diff wiped lenses: %+v", prof.Lenses)
	}
	if prof.GraphBackendRef() != "dotagents-builtin:graph/none@^1.0" {
		t.Errorf("model-only diff wiped graph_backend: %q", prof.GraphBackendRef())
	}
}
