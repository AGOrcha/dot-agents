package workflow

import (
	"errors"
	"testing"
)

// fakeResolver is a non-git baseResolver used to prove the §4.4.2 seam is
// domain-agnostic. trunk is its no-layer base; layer decides layerability and
// the synthesized outputRef per dep, so a test can model a content-addressed
// domain (ref = a digest, no PR) or a non-composable domain (never layerable).
type fakeResolver struct {
	trunk string
	layer func(inFlightTask) (outputRef, bool)
}

func (f fakeResolver) Trunk() string                                  { return f.trunk }
func (f fakeResolver) LayerableBase(t inFlightTask) (outputRef, bool) { return f.layer(t) }

func TestPRBaseResolverTrunk(t *testing.T) {
	if got := (prBaseResolver{}).Trunk(); got != baseRefMaster {
		t.Fatalf("Trunk() = %q, want %q", got, baseRefMaster)
	}
}

func TestPRBaseResolverLayerableBase(t *testing.T) {
	cases := []struct {
		name      string
		in        inFlightTask
		wantRef   string
		wantPR    int
		layerable bool
	}{
		{"awaiting_review with branch", inFlightTask{Status: "awaiting_review", PRBranch: "feature/x", PRNumber: 9}, "feature/x", 9, true},
		{"awaiting_agent_review", inFlightTask{Status: "awaiting_agent_review", PRBranch: "feature/y", PRNumber: 1}, "feature/y", 1, true},
		{"awaiting_owner_review", inFlightTask{Status: "awaiting_owner_review", PRBranch: "feature/z", PRNumber: 2}, "feature/z", 2, true},
		{"in_progress not layerable", inFlightTask{Status: "in_progress", PRBranch: "feature/x"}, "", 0, false},
		{"completed not layerable", inFlightTask{Status: "completed", PRBranch: "feature/x"}, "", 0, false},
		{"awaiting but blank branch", inFlightTask{Status: "awaiting_review", PRBranch: "  "}, "", 0, false},
	}
	var r prBaseResolver
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, ok := r.LayerableBase(tc.in)
			if ok != tc.layerable {
				t.Fatalf("layerable = %v, want %v", ok, tc.layerable)
			}
			if ref.Ref != tc.wantRef || ref.PR != tc.wantPR {
				t.Fatalf("ref = %+v, want {Ref:%q PR:%d}", ref, tc.wantRef, tc.wantPR)
			}
		})
	}
}

func TestBaseResolutionInputResolverDefaultsToPR(t *testing.T) {
	if _, ok := (baseResolutionInput{}).resolver().(prBaseResolver); !ok {
		t.Fatalf("nil Resolver must default to prBaseResolver")
	}
	custom := fakeResolver{trunk: "x", layer: func(inFlightTask) (outputRef, bool) { return outputRef{}, false }}
	if _, ok := (baseResolutionInput{Resolver: custom}).resolver().(fakeResolver); !ok {
		t.Fatalf("injected Resolver must be returned verbatim")
	}
}

// TestResolveBaseCompletionGatesNonLayerableAdapter proves the §4.4.1 floor: a
// non-composable domain whose outputs are never layerable completion-gates to
// its own trunk even when deps are "ready" — no PR, no lineage.
func TestResolveBaseCompletionGatesNonLayerableAdapter(t *testing.T) {
	gated := fakeResolver{
		trunk: "main-line",
		layer: func(inFlightTask) (outputRef, bool) { return outputRef{}, false },
	}
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p",
		DependsOn: []string{"dep"},
		InFlight: map[string]inFlightTask{
			"p/dep": {Status: "awaiting_review", PRBranch: "feature/dep", PRNumber: 7},
		},
		Resolver: gated,
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatalf("resolveBase: %v", err)
	}
	if res.BaseBranch != "main-line" {
		t.Fatalf("non-layerable adapter must completion-gate to its trunk; got base %q", res.BaseBranch)
	}
	if res.BasePR != 0 || res.BaseTask != "" || res.Lineage != nil {
		t.Fatalf("completion-gate must carry no PR/lineage; got %+v", res)
	}
}

// TestResolveBaseDepNotInFlightGatesToTrunk covers a dep declared in DependsOn
// but absent from the in-flight map (e.g. already merged and pruned): it is
// skipped and resolution completion-gates to the adapter trunk.
func TestResolveBaseDepNotInFlightGatesToTrunk(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p",
		DependsOn: []string{"ghost"},
		InFlight:  map[string]inFlightTask{}, // "p/ghost" not present
		Resolver:  fakeResolver{trunk: "trunk-x", layer: func(inFlightTask) (outputRef, bool) { return outputRef{}, true }},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatalf("resolveBase: %v", err)
	}
	if res.BaseBranch != "trunk-x" {
		t.Fatalf("absent dep must gate to trunk; got base %q", res.BaseBranch)
	}
}

// TestResolveBaseCustomAdapterLayersOnDigest proves an OutputRef need not be a
// git branch: a content-addressed domain layers a downstream task onto an
// upstream output's digest, with no PR number.
func TestResolveBaseCustomAdapterLayersOnDigest(t *testing.T) {
	digest := fakeResolver{
		trunk: "latest",
		layer: func(f inFlightTask) (outputRef, bool) {
			if f.Status != "published" {
				return outputRef{}, false
			}
			return outputRef{Ref: "sha256:" + f.PRBranch}, true
		},
	}
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p",
		DependsOn: []string{"dep"},
		InFlight: map[string]inFlightTask{
			"p/dep": {Status: "published", PRBranch: "abc123"},
		},
		Resolver: digest,
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatalf("resolveBase: %v", err)
	}
	if res.BaseBranch != "sha256:abc123" {
		t.Fatalf("content adapter base = %q, want sha256:abc123", res.BaseBranch)
	}
	if res.BaseTask != "p/dep" || res.BasePR != 0 {
		t.Fatalf("base task/pr = %q/%d, want p/dep/0", res.BaseTask, res.BasePR)
	}
}

// TestResolveBaseCustomAdapterMultiDistinctRefuses proves the §4.1 step-4b
// conflict path is adapter-agnostic: two distinct non-git output refs still
// refuse and demand explicit sequencing.
func TestResolveBaseCustomAdapterMultiDistinctRefuses(t *testing.T) {
	digest := fakeResolver{
		trunk: "latest",
		layer: func(f inFlightTask) (outputRef, bool) {
			return outputRef{Ref: "sha256:" + f.PRBranch}, true
		},
	}
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p",
		DependsOn: []string{"a", "b"},
		InFlight: map[string]inFlightTask{
			"p/a": {Status: "published", PRBranch: "aa"},
			"p/b": {Status: "published", PRBranch: "bb"},
		},
		Resolver: digest,
	}
	_, err := resolveBase(in)
	var conflict *multiDepConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("two distinct digest refs must refuse with multiDepConflict; got %v", err)
	}
}
