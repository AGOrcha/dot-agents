package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// snapWith builds a synthetic resolved Snapshot whose Effective config carries an
// execution_profile (app_type → verifier_sequence), stage_profiles.verifier, and
// a precondition_policies registry — the three surfaces the resolver walks.
func snapWith(seq []string, profiles map[string]StageProfile, registry map[string]PreconditionPolicySpec) *Snapshot {
	return &Snapshot{
		Effective: AgentsRC{
			ExecutionProfile: &ExecutionProfile{
				ByAppType: map[string]AppTypeProfile{
					"go-cli": {Topology: Topology{VerifierSequence: seq}},
				},
			},
			StageProfiles:        map[string]map[string]StageProfile{verifierStageKey: profiles},
			PreconditionPolicies: registry,
		},
	}
}

func TestResolvePreconditionPolicyFromSnapshot(t *testing.T) {
	strict := PreconditionPolicySpec{Predicates: []PredicateSpec{
		{Signal: "event.pr.open"},
		{Signal: "gate.quality.sonar", Args: map[string]string{"equals": "pass"}},
	}}

	cases := []struct {
		name     string
		snap     *Snapshot
		appType  string
		wantName string
		wantPred []ResolvedPredicate
	}{
		{
			name: "app_type → profile → named policy resolves predicates",
			snap: snapWith(
				[]string{"strict-go"},
				map[string]StageProfile{"strict-go": {PreconditionPolicy: "strict"}},
				map[string]PreconditionPolicySpec{"strict": strict},
			),
			appType:  "go-cli",
			wantName: "strict",
			wantPred: []ResolvedPredicate{
				{Signal: "event.pr.open"},
				{Signal: "gate.quality.sonar", Args: map[string]string{"equals": "pass"}},
			},
		},
		{
			name: "first profile naming a policy wins over later ones",
			snap: snapWith(
				[]string{"plain", "strict-go"},
				map[string]StageProfile{
					"plain":     {},
					"strict-go": {PreconditionPolicy: "strict"},
				},
				map[string]PreconditionPolicySpec{"strict": strict},
			),
			appType:  "go-cli",
			wantName: "strict",
			wantPred: []ResolvedPredicate{
				{Signal: "event.pr.open"},
				{Signal: "gate.quality.sonar", Args: map[string]string{"equals": "pass"}},
			},
		},
		{
			name:     "nil snapshot → default",
			snap:     nil,
			appType:  "go-cli",
			wantName: defaultPolicyName,
		},
		{
			name: "no verifier_sequence → default",
			snap: snapWith(
				nil,
				map[string]StageProfile{"strict-go": {PreconditionPolicy: "strict"}},
				map[string]PreconditionPolicySpec{"strict": strict},
			),
			appType:  "go-cli",
			wantName: defaultPolicyName,
		},
		{
			name: "unknown app_type → default",
			snap: snapWith(
				[]string{"strict-go"},
				map[string]StageProfile{"strict-go": {PreconditionPolicy: "strict"}},
				map[string]PreconditionPolicySpec{"strict": strict},
			),
			appType:  "rust-lib",
			wantName: defaultPolicyName,
		},
		{
			name: "profile names no policy → default",
			snap: snapWith(
				[]string{"plain"},
				map[string]StageProfile{"plain": {}},
				map[string]PreconditionPolicySpec{"strict": strict},
			),
			appType:  "go-cli",
			wantName: defaultPolicyName,
		},
		{
			name: "profile names a MISSING policy → default (Slice B5 hard-errors)",
			snap: snapWith(
				[]string{"ghost"},
				map[string]StageProfile{"ghost": {PreconditionPolicy: "absent"}},
				map[string]PreconditionPolicySpec{"strict": strict},
			),
			appType:  "go-cli",
			wantName: defaultPolicyName,
		},
		{
			name: "named-but-empty policy resolves to that name with no predicates",
			snap: snapWith(
				[]string{"empty-go"},
				map[string]StageProfile{"empty-go": {PreconditionPolicy: "empty"}},
				map[string]PreconditionPolicySpec{"empty": {}},
			),
			appType:  "go-cli",
			wantName: "empty",
			wantPred: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePreconditionPolicyFromSnapshot(tc.snap, tc.appType)
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if !reflect.DeepEqual(got.Predicates, tc.wantPred) {
				t.Errorf("Predicates = %#v, want %#v", got.Predicates, tc.wantPred)
			}
		})
	}
}

// TestResolvePreconditionPolicyLockfile exercises the full lockfile-backed path:
// a flat project (no extends) whose committed manifest declares the execution
// profile + stage profile + policy registry. ResolveLocked degrades to the FLAT
// layer set, so ResolvePreconditionPolicy reads the same merged config a real
// verifier would.
func TestResolvePreconditionPolicyLockfile(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"execution_profile": {
			"by_app_type": {
				"go-cli": {"topology": {"verifier_sequence": ["strict-go"]}}
			}
		},
		"stage_profiles": {
			"verifier": {
				"strict-go": {"precondition_policy": "strict"}
			}
		},
		"precondition_policies": {
			"strict": {
				"predicates": [
					{"signal": "event.pr.open"},
					{"signal": "signal.ci.rollup", "args": {"equals": "GREEN"}}
				]
			}
		}
	}`)

	got, err := ResolvePreconditionPolicy(repo, "go-cli")
	if err != nil {
		t.Fatalf("ResolvePreconditionPolicy: %v", err)
	}
	if got.Name != "strict" {
		t.Errorf("Name = %q, want %q", got.Name, "strict")
	}
	want := []ResolvedPredicate{
		{Signal: "event.pr.open"},
		{Signal: "signal.ci.rollup", Args: map[string]string{"equals": "GREEN"}},
	}
	if !reflect.DeepEqual(got.Predicates, want) {
		t.Errorf("Predicates = %#v, want %#v", got.Predicates, want)
	}
}

// TestResolvePreconditionPolicyLockfileDefault confirms a flat project with no
// precondition wiring resolves to the built-in default (never an error).
func TestResolvePreconditionPolicyLockfileDefault(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"version": 2}`)

	got, err := ResolvePreconditionPolicy(repo, "go-cli")
	if err != nil {
		t.Fatalf("ResolvePreconditionPolicy: %v", err)
	}
	if got.Name != defaultPolicyName {
		t.Errorf("Name = %q, want %q", got.Name, defaultPolicyName)
	}
	if got.Predicates != nil {
		t.Errorf("Predicates = %#v, want nil", got.Predicates)
	}
}

// TestResolvePreconditionPolicyLockError confirms a ResolveLocked failure
// (here: a malformed user-local manifest) propagates as an error rather than
// silently degrading to the default.
func TestResolvePreconditionPolicyLockError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, AgentsRCFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	writeManifest(t, repo, `{"version": 2}`)

	if _, err := ResolvePreconditionPolicy(repo, "go-cli"); err == nil {
		t.Fatal("expected error from malformed user-local manifest, got nil")
	}
}

func TestConvertPredicatesEmpty(t *testing.T) {
	if got := convertPredicates(nil); got != nil {
		t.Errorf("convertPredicates(nil) = %#v, want nil", got)
	}
}
