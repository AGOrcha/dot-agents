package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sampleProfile is a representative execution_profile covering all three facets
// for two app_types (go-cli + ideation), used across the round-trip and
// classification tests.
func sampleProfile() *ExecutionProfile {
	return &ExecutionProfile{
		DefaultClass: "situational",
		ByAppType: map[string]AppTypeProfile{
			"go-cli": {
				Relevance: map[string]RelevanceClasses{
					"orchestrate": {
						Core:  []string{"orchestrator-session-start", "loop-worker"},
						Noise: []string{"article-extract", "playwright"},
					},
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
			"ideation": {
				Topology: Topology{Executors: 3, VerifiersPerExecutor: 0, Reviewers: "0"},
				Lenses:   Lenses{LensSet: []string{"acceptance-invariants"}, LensConcurrency: "parallel"},
			},
		},
	}
}

// TestExecutionProfile_ClassOf is the table-driven positive + negative coverage
// for the relevance-class helper, including the default-class fall-through that
// guarantees no unit is silently dropped.
func TestExecutionProfile_ClassOf(t *testing.T) {
	p := sampleProfile()
	tests := []struct {
		name              string
		appType, stage, u string
		want              string
	}{
		// positive: explicit listings win.
		{"core unit", "go-cli", "orchestrate", "loop-worker", "core"},
		{"noise unit", "go-cli", "orchestrate", "playwright", "noise"},
		{"situational unit", "go-cli", "review", "self-review", "situational"},
		// negative / default fall-through: unlisted unit is situational, never dropped.
		{"unlisted unit defaults situational", "go-cli", "orchestrate", "never-heard-of-it", "situational"},
		{"unknown stage defaults", "go-cli", "deploy", "loop-worker", "situational"},
		{"unknown app_type defaults", "rust-svc", "orchestrate", "loop-worker", "situational"},
		{"app_type with no relevance map defaults", "ideation", "orchestrate", "anything", "situational"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.ClassOf(tc.appType, tc.stage, tc.u); got != tc.want {
				t.Errorf("ClassOf(%q,%q,%q)=%q, want %q", tc.appType, tc.stage, tc.u, got, tc.want)
			}
		})
	}
}

// TestExecutionProfile_ClassOf_ConservativeWins proves that when a unit is
// (mis)listed in more than one class, the most conservative class wins so an
// operator suppressing a unit is never overridden by a stale core listing.
func TestExecutionProfile_ClassOf_ConservativeWins(t *testing.T) {
	p := &ExecutionProfile{
		ByAppType: map[string]AppTypeProfile{
			"go-cli": {Relevance: map[string]RelevanceClasses{
				"review": {
					Core:        []string{"dup"},
					Situational: []string{"dup"},
					Noise:       []string{"dup"},
				},
			}},
		},
	}
	if got := p.ClassOf("go-cli", "review", "dup"); got != "noise" {
		t.Errorf("conflicting listing should resolve noise-first, got %q", got)
	}
}

// TestExecutionProfile_EffectiveDefaultClass covers the default-class resolution
// including the nil-profile and empty-default safe fallbacks.
func TestExecutionProfile_EffectiveDefaultClass(t *testing.T) {
	tests := []struct {
		name string
		p    *ExecutionProfile
		want string
	}{
		{"nil profile", nil, DefaultRelevanceClass},
		{"empty default", &ExecutionProfile{}, DefaultRelevanceClass},
		{"explicit default", &ExecutionProfile{DefaultClass: "noise"}, "noise"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.EffectiveDefaultClass(); got != tc.want {
				t.Errorf("EffectiveDefaultClass()=%q, want %q", got, tc.want)
			}
		})
	}
}

// TestExecutionProfile_ClassOf_NilReceiver proves a nil profile classifies every
// unit as the safe default rather than panicking.
func TestExecutionProfile_ClassOf_NilReceiver(t *testing.T) {
	var p *ExecutionProfile
	if got := p.ClassOf("go-cli", "review", "anything"); got != DefaultRelevanceClass {
		t.Errorf("nil profile ClassOf=%q, want %q", got, DefaultRelevanceClass)
	}
}

// TestExecutionProfile_ClassOf_NilByAppType covers a non-nil profile with no
// by_app_type map (the wholesale-default path).
func TestExecutionProfile_ClassOf_NilByAppType(t *testing.T) {
	p := &ExecutionProfile{DefaultClass: "core"}
	if got := p.ClassOf("go-cli", "review", "anything"); got != "core" {
		t.Errorf("ClassOf with nil ByAppType=%q, want %q", got, "core")
	}
}

// TestContains exercises the small slice-membership helper directly for the
// empty-slice and absent-element negative branches.
func TestContains(t *testing.T) {
	if contains(nil, "x") {
		t.Error("contains(nil, x) should be false")
	}
	if !contains([]string{"a", "b"}, "b") {
		t.Error("contains([a b], b) should be true")
	}
	if contains([]string{"a", "b"}, "c") {
		t.Error("contains([a b], c) should be false")
	}
}

// TestExecutionProfile_JSONRoundTrip proves marshal/unmarshal preserves every
// facet of the profile — the load-bearing guarantee that nothing is dropped on
// disk round-trip.
func TestExecutionProfile_JSONRoundTrip(t *testing.T) {
	orig := sampleProfile()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rt ExecutionProfile
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	gc := rt.ByAppType["go-cli"]
	if gc.Topology.Executors != 1 || gc.Topology.VerifiersPerExecutor != 2 {
		t.Errorf("topology round-trip lost: %+v", gc.Topology)
	}
	if gc.Topology.Reviewers != "per_verifier" {
		t.Errorf("reviewers round-trip lost: %q", gc.Topology.Reviewers)
	}
	if len(gc.Topology.VerifierSequence) != 2 || gc.Topology.VerifierSequence[0] != "unit" {
		t.Errorf("verifier_sequence round-trip lost: %v", gc.Topology.VerifierSequence)
	}
	if gc.Lenses.LensConcurrency != "gated" || len(gc.Lenses.LensSet) != 2 {
		t.Errorf("lenses round-trip lost: %+v", gc.Lenses)
	}
	if rt.ClassOf("go-cli", "orchestrate", "playwright") != "noise" {
		t.Errorf("relevance round-trip lost noise classification")
	}
	if rt.DefaultClass != "situational" {
		t.Errorf("default_class round-trip lost: %q", rt.DefaultClass)
	}

	// JSON keys must be snake_case per the config-v2 wire format.
	for _, key := range []string{"by_app_type", "default_class", "verifiers_per_executor",
		"verifier_sequence", "lens_set", "lens_concurrency"} {
		if !contains(jsonKeys(t, data), key) {
			t.Errorf("expected snake_case key %q in marshaled output: %s", key, data)
		}
	}
}

// jsonKeys recursively collects every object key in a JSON document so a
// round-trip test can assert wire-format key names without coupling to nesting.
func jsonKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("jsonKeys parse: %v", err)
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				out = append(out, k)
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(doc)
	return out
}

// TestAgentsRC_ExecutionProfile_TypedField proves the AgentsRC wiring routes
// execution_profile to the typed field (NOT ExtraFields) and preserves it on a
// full AgentsRC round-trip — the [[schema-usage]] six-place guarantee.
func TestAgentsRC_ExecutionProfile_TypedField(t *testing.T) {
	raw := []byte(`{
  "version": 2,
  "hooks": false,
  "mcp": false,
  "settings": false,
  "sources": [{"type": "local"}],
  "execution_profile": {
    "default_class": "situational",
    "by_app_type": {
      "go-cli": {
        "topology": {"executors": 1, "verifiers_per_executor": 2, "reviewers": "per_verifier"},
        "relevance": {"review": {"noise": ["playwright"]}}
      }
    }
  }
}`)
	var rc AgentsRC
	if err := json.Unmarshal(raw, &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc.ExecutionProfile == nil {
		t.Fatal("execution_profile must populate the typed field")
	}
	if _, leaked := rc.ExtraFields["execution_profile"]; leaked {
		t.Error("execution_profile must NOT land in ExtraFields (typed field expected)")
	}
	if got := rc.ExecutionProfile.ClassOf("go-cli", "review", "playwright"); got != "noise" {
		t.Errorf("typed field lost classification, got %q", got)
	}

	out, err := json.Marshal(&rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(jsonKeys(t, out), "execution_profile") {
		t.Errorf("re-marshal dropped execution_profile: %s", out)
	}

	var rt AgentsRC
	if err := json.Unmarshal(out, &rt); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if rt.ExecutionProfile == nil || rt.ExecutionProfile.ByAppType["go-cli"].Topology.VerifiersPerExecutor != 2 {
		t.Errorf("full AgentsRC round-trip lost execution_profile topology")
	}
}

// TestAgentsRC_ExecutionProfile_Absent confirms an AgentsRC without
// execution_profile marshals without the key (omitempty) and leaves the typed
// field nil — the additive byte-stability contract.
func TestAgentsRC_ExecutionProfile_Absent(t *testing.T) {
	rc := &AgentsRC{Version: 2, Sources: []Source{{Type: "local"}}}
	if rc.ExecutionProfile != nil {
		t.Fatal("ExecutionProfile should default nil")
	}
	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if contains(jsonKeys(t, out), "execution_profile") {
		t.Errorf("absent execution_profile must not emit the key: %s", out)
	}
}

// TestExecutionProfile_LayerSettable proves execution_profile is NOT a forbidden
// or protected layer field — it must merge by scope precedence as a kind=layer.
func TestExecutionProfile_LayerSettable(t *testing.T) {
	if _, forbidden := layerForbiddenFields["execution_profile"]; forbidden {
		t.Error("execution_profile must be layer-settable (not in layerForbiddenFields)")
	}
	if _, protected := protectedSet["execution_profile"]; protected {
		t.Error("execution_profile must not be a protected identity field")
	}
	sanitized, warnings, err := validateLayer("acme:org/base", map[string]any{
		"execution_profile": map[string]any{"default_class": "situational"},
	})
	if err != nil {
		t.Fatalf("validateLayer rejected execution_profile layer: %v", err)
	}
	if _, kept := sanitized["execution_profile"]; !kept {
		t.Error("validateLayer dropped execution_profile from a layer payload")
	}
	for _, w := range warnings {
		if w.FieldPath == "execution_profile" {
			t.Errorf("execution_profile must not raise a provenance warning: %+v", w)
		}
	}
}

// TestExecutionProfile_SchemaSync asserts the agentsrc schema declares
// execution_profile (and its core nested keys) so a marshaled AgentsRC carrying
// the typed field does not drift from the schema under additionalProperties:false.
func TestExecutionProfile_SchemaSync(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "schemas", "agentsrc.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, ok := doc.Properties["execution_profile"]; !ok {
		t.Error("schema missing top-level execution_profile property (struct↔schema drift)")
	}
	for _, def := range []string{"appTypeProfile", "relevanceClasses", "topology", "lenses"} {
		if _, ok := doc.Defs[def]; !ok {
			t.Errorf("schema missing $defs/%s for execution_profile", def)
		}
	}

	// Compile + validate a real execution_profile-bearing manifest against the
	// shipped schema to prove the wire form is structurally accepted.
	sch := compileAgentsRCSchema(t)
	manifest := map[string]any{
		"version":  2,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources":  []any{map[string]any{"type": "local"}},
		"execution_profile": map[string]any{
			"default_class": "situational",
			"by_app_type": map[string]any{
				"go-cli": map[string]any{
					"topology": map[string]any{
						"executors":              1,
						"verifiers_per_executor": 2,
						"reviewers":              "per_verifier",
						"verifier_sequence":      []any{"unit", "cli-runner"},
					},
					"lenses": map[string]any{
						"lens_set":         []any{"adversarial"},
						"lens_concurrency": "gated",
					},
					"relevance": map[string]any{
						"review": map[string]any{"noise": []any{"playwright"}},
					},
				},
			},
		},
	}
	if err := sch.Validate(manifest); err != nil {
		t.Fatalf("execution_profile manifest must validate against schema: %v", err)
	}
}

// TestExecutionProfile_SchemaRejectsUnknownNested proves the schema's
// additionalProperties:false guard catches a typo'd nested key, so future
// struct↔schema drift fails loudly.
func TestExecutionProfile_SchemaRejectsUnknownNested(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	bad := map[string]any{
		"version":  2,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources":  []any{map[string]any{"type": "local"}},
		"execution_profile": map[string]any{
			"by_app_type": map[string]any{
				"go-cli": map[string]any{
					"topology": map[string]any{"executorz": 1}, // typo'd key
				},
			},
		},
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("schema must reject unknown nested key in topology (additionalProperties:false)")
	}
}

// TestExecutionProfile_SchemaRejectsBadDefaultClass proves the default_class
// enum is enforced by the schema.
func TestExecutionProfile_SchemaRejectsBadDefaultClass(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	bad := map[string]any{
		"version":           2,
		"hooks":             false,
		"mcp":               false,
		"settings":          false,
		"sources":           []any{map[string]any{"type": "local"}},
		"execution_profile": map[string]any{"default_class": "irrelevant"},
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("schema must reject default_class outside the core|situational|noise enum")
	}
}
