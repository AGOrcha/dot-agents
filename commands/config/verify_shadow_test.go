package config

import (
	"reflect"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// TestVerifyLayerShadows covers the shadow check's classification: a repo-local
// key that restates what the stack below supplies is REDUNDANT (warn), one that
// differs is an OVERRIDE (pass, both values named), and one no lower layer
// supplies is not reported at all.
func TestVerifyLayerShadows(t *testing.T) {
	tests := []struct {
		name string
		// repo/below are the raw repo-local and user-local layer objects.
		repo  map[string]any
		below map[string]any
		// wantNames are the expected check names, in order.
		wantNames []string
		// wantStatus maps a check name to its expected status.
		wantStatus map[string]string
		// wantDetail maps a check name to substrings its detail must contain.
		wantDetail map[string][]string
	}{
		{
			name:       "no repo-local layer at all reports nothing",
			repo:       nil,
			below:      map[string]any{"settings": true},
			wantNames:  nil,
			wantStatus: map[string]string{},
		},
		{
			name:       "clean manifest reports a single pass",
			repo:       map[string]any{"version": float64(2), "project": "fixture"},
			below:      map[string]any{"settings": true},
			wantNames:  []string{"layer-shadows"},
			wantStatus: map[string]string{"layer-shadows": verifyPass},
		},
		{
			name:       "redundant shadow of an identical false warns",
			repo:       map[string]any{"settings": false},
			below:      map[string]any{"settings": false},
			wantNames:  []string{"shadow:settings"},
			wantStatus: map[string]string{"shadow:settings": verifyWarn},
			wantDetail: map[string][]string{
				"shadow:settings": {"REDUNDANT", "settings=false", cfg.LayerUserLocal, "remove the key"},
			},
		},
		{
			name:       "repo false over a layer true is an informational override naming both values",
			repo:       map[string]any{"settings": false},
			below:      map[string]any{"settings": true},
			wantNames:  []string{"shadow:settings"},
			wantStatus: map[string]string{"shadow:settings": verifyPass},
			wantDetail: map[string][]string{
				"shadow:settings": {"OVERRIDE", "settings=false", "settings=true", cfg.LayerUserLocal},
			},
		},
		{
			name:       "a key no lower layer supplies is not reported",
			repo:       map[string]any{"settings": false},
			below:      map[string]any{},
			wantNames:  []string{"layer-shadows"},
			wantStatus: map[string]string{"layer-shadows": verifyPass},
		},
		{
			name:       "structural and protected keys are exempt even when both layers carry them",
			repo:       map[string]any{"version": float64(2), "$schema": "s", "repo_id": "github.com/acme/r", "project": "p"},
			below:      map[string]any{"version": float64(2), "$schema": "s", "repo_id": "github.com/acme/r", "project": "p"},
			wantNames:  []string{"layer-shadows"},
			wantStatus: map[string]string{"layer-shadows": verifyPass},
		},
		{
			name:       "non-scalar keys are exempt because they combine rather than shadow",
			repo:       map[string]any{"skills": []any{"a"}, "features": map[string]any{"beta": "on"}, "sources": []any{map[string]any{"type": "local"}}},
			below:      map[string]any{"skills": []any{"a"}, "features": map[string]any{"beta": "on"}, "sources": []any{map[string]any{"type": "local"}}},
			wantNames:  []string{"layer-shadows"},
			wantStatus: map[string]string{"layer-shadows": verifyPass},
		},
		{
			name:      "multiple shadows are reported per key in sorted order",
			repo:      map[string]any{"hooks": false, "mcp": false, "settings": false},
			below:     map[string]any{"hooks": false, "mcp": true, "settings": false},
			wantNames: []string{"shadow:hooks", "shadow:mcp", "shadow:settings"},
			wantStatus: map[string]string{
				"shadow:hooks":    verifyWarn,
				"shadow:mcp":      verifyPass,
				"shadow:settings": verifyWarn,
			},
		},
		{
			name:       "an object-valued scalar key compares structurally",
			repo:       map[string]any{"locks": map[string]any{"value_locks": map[string]any{"hooks": true}}},
			below:      map[string]any{"locks": map[string]any{"value_locks": map[string]any{"hooks": true}}},
			wantNames:  []string{"shadow:locks"},
			wantStatus: map[string]string{"shadow:locks": verifyWarn},
		},
		{
			name:       "an unmarshalable repo value degrades to an override rather than claiming redundancy",
			repo:       map[string]any{"settings": make(chan int)},
			below:      map[string]any{"settings": make(chan int)},
			wantNames:  []string{"shadow:settings"},
			wantStatus: map[string]string{"shadow:settings": verifyPass},
			wantDetail: map[string][]string{"shadow:settings": {"OVERRIDE"}},
		},
		{
			name:       "an unmarshalable layer value degrades the same way",
			repo:       map[string]any{"settings": false},
			below:      map[string]any{"settings": make(chan int)},
			wantNames:  []string{"shadow:settings"},
			wantStatus: map[string]string{"shadow:settings": verifyPass},
			wantDetail: map[string][]string{"shadow:settings": {"OVERRIDE", "settings=false"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := verifyLayerShadows(shadowSnapshot(tc.repo, tc.below))
			assertCheckNames(t, got, tc.wantNames)
			assertCheckStatuses(t, got, tc.wantStatus)
			assertCheckDetails(t, got, tc.wantDetail)
		})
	}
}

// TestLayersBelowRepoLocal pins the filter that answers "what would apply if
// this manifest declared nothing": repo-local is dropped and every other layer,
// including the higher-precedence-than-imports project-local overlay, is kept.
func TestLayersBelowRepoLocal(t *testing.T) {
	if got := layersBelowRepoLocal(nil); got != nil {
		t.Errorf("nil snapshot: got %v, want nil", got)
	}

	snap := &cfg.Snapshot{Layers: []cfg.ResolvedLayer{
		{ID: cfg.LayerProductDefaults},
		{ID: cfg.LayerUserLocal},
		{ID: "acme:base.json"},
		{ID: cfg.LayerProjectLocal},
		{ID: cfg.LayerRepoLocal},
	}}
	var ids []string
	for _, layer := range layersBelowRepoLocal(snap) {
		ids = append(ids, layer.ID)
	}
	want := []string{cfg.LayerProductDefaults, cfg.LayerUserLocal, "acme:base.json", cfg.LayerProjectLocal}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("layers: got %v, want %v", ids, want)
	}
}

// TestVerifyLayerShadows_InReport is the end-to-end pass through
// `da config verify` over a real on-disk manifest pair: a repo manifest that
// carries `"settings": false` beside an extends-style layered setup, exactly
// the shape a repo hit by the injection bug is left in. A redundant shadow must
// surface in the report and must NOT flip OK.
func TestVerifyLayerShadows_InReport(t *testing.T) {
	tests := []struct {
		name       string
		repoBody   string
		userBody   string
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "injected-identical shadow reports REDUNDANT",
			repoBody:   `{"version": 2, "project": "fixture", "settings": false}`,
			userBody:   `{"version": 2, "settings": false}`,
			wantStatus: verifyWarn,
			wantDetail: []string{"REDUNDANT", "settings=false"},
		},
		{
			name:       "layer true with repo false reports OVERRIDE with both values",
			repoBody:   `{"version": 2, "project": "fixture", "settings": false}`,
			userBody:   `{"version": 2, "settings": true}`,
			wantStatus: verifyPass,
			wantDetail: []string{"OVERRIDE", "settings=false", "settings=true"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := withRepoLayer(t, tc.repoBody, tc.userBody)
			report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))

			check, ok := findCheck(report.Checks, "shadow:settings")
			if !ok {
				t.Fatalf("shadow:settings check missing from report: %+v", report.Checks)
			}
			if check.Status != tc.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tc.wantStatus, check.Detail)
			}
			assertDetailContains(t, check, tc.wantDetail)
			if !report.OK {
				t.Error("a shadow finding must never fail the report")
			}
		})
	}
}

// shadowSnapshot builds a two-layer stack: a user-local layer standing in for
// everything below, and the repo-local layer under inspection. A nil repo map
// omits the repo-local layer entirely.
func shadowSnapshot(repo, below map[string]any) *cfg.Snapshot {
	layers := []cfg.ResolvedLayer{{ID: cfg.LayerUserLocal, Present: true, Raw: below}}
	if repo != nil {
		layers = append(layers, cfg.ResolvedLayer{ID: cfg.LayerRepoLocal, Present: true, Raw: repo})
	}
	return &cfg.Snapshot{Layers: layers}
}

// assertCheckNames compares the emitted check names, in order, against want.
func assertCheckNames(t *testing.T, got []VerifyCheck, want []string) {
	t.Helper()
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("check names: got %v, want %v", names, want)
	}
}

// assertCheckStatuses verifies each named check's status.
func assertCheckStatuses(t *testing.T, got []VerifyCheck, want map[string]string) {
	t.Helper()
	for name, status := range want {
		check, ok := findCheck(got, name)
		if !ok {
			t.Errorf("check %q missing", name)
			continue
		}
		if check.Status != status {
			t.Errorf("check %q: got status %q, want %q (detail: %s)", name, check.Status, status, check.Detail)
		}
	}
}

// assertCheckDetails verifies each named check's detail carries every expected
// substring — the operator-facing contract that both values and the losing
// layer are named.
func assertCheckDetails(t *testing.T, got []VerifyCheck, want map[string][]string) {
	t.Helper()
	for name, fragments := range want {
		check, ok := findCheck(got, name)
		if !ok {
			t.Errorf("check %q missing", name)
			continue
		}
		assertDetailContains(t, check, fragments)
	}
}

// assertDetailContains fails for every fragment missing from a check's detail.
func assertDetailContains(t *testing.T, check VerifyCheck, fragments []string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(check.Detail, fragment) {
			t.Errorf("check %q detail %q must contain %q", check.Name, check.Detail, fragment)
		}
	}
}

// TestRenderShadowValue covers the one-line value rendering, including the
// fallback for a value encoding/json cannot represent.
func TestRenderShadowValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "bool", value: false, want: "false"},
		{name: "string", value: "on", want: `"on"`},
		{name: "object", value: map[string]any{"b": 1, "a": 2}, want: `{"a":2,"b":1}`},
		{name: "unmarshalable falls back to %v", value: func() {}, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderShadowValue(tc.value)
			if tc.want == "" {
				if got == "" || strings.HasPrefix(got, "{") {
					t.Errorf("unmarshalable value: got %q, want a non-JSON fallback rendering", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
