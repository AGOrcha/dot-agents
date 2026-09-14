package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestSource_IsDefaultHomeLocalClassifiesOwnership pins the discriminator the
// generation/install source contract rests on. Only the bare, path-less
// `{"type":"local"}` entry is the user-home sentinel; ANY field an author can
// write makes the source theirs, so a `local` source with a path, a cache TTL,
// an id, a scope or auth stays project-owned. Getting this wrong in the
// permissive direction would silently delete authored org-layer roots from
// committed manifests.
func TestSource_IsDefaultHomeLocalClassifiesOwnership(t *testing.T) {
	tests := []struct {
		name string
		src  Source
		want bool
	}{
		{"bare local is the user-home sentinel", Source{Type: "local"}, true},
		{"path-bearing local is an authored root", Source{Type: "local", Path: "../orglayer"}, false},
		{"identified local is an authored root", Source{Type: "local", ID: "acme"}, false},
		{"ttl-bearing local is an authored root", Source{Type: "local", CacheTTL: "4h"}, false},
		{"scoped local is an authored root", Source{Type: "local", Scope: SourceScopeOrg}, false},
		{"authenticated local is an authored root", Source{Type: "local", Auth: json.RawMessage(`{}`)}, false},
		{"git is never the sentinel", Source{Type: "git", URL: "https://example.test/o.git"}, false},
		{"http is never the sentinel", Source{Type: "http", URL: "https://example.test/o.tar"}, false},
		{"oci is never the sentinel", Source{Type: "oci", URL: "ghcr.io/acme/layer"}, false},
		{"an empty source is not a local declaration", Source{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.src.IsDefaultHomeLocal(); got != tc.want {
				t.Errorf("IsDefaultHomeLocal(%+v) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

// TestProjectOwnedSources_DropsOnlyTheUserHomeSentinel pins the list-level
// contract: order and identity of authored roots are untouched, every sentinel
// entry is removed wherever it sits, and an all-sentinel list collapses to nil
// so assigning the result keeps the `sources` key omitted on save.
func TestProjectOwnedSources_DropsOnlyTheUserHomeSentinel(t *testing.T) {
	org := Source{Type: "local", Path: "../orglayer"}
	remote := Source{Type: "git", URL: "https://example.test/o.git", Ref: "main"}

	tests := []struct {
		name string
		in   []Source
		want []Source
	}{
		{"nil stays nil", nil, nil},
		{"sentinel only collapses to nil", []Source{{Type: "local"}}, nil},
		{"repeated sentinels collapse to nil", []Source{{Type: "local"}, {Type: "local"}}, nil},
		{"leading sentinel is dropped", []Source{{Type: "local"}, org, remote}, []Source{org, remote}},
		{"interleaved sentinel is dropped", []Source{org, {Type: "local"}, remote}, []Source{org, remote}},
		{"authored-only list is returned verbatim", []Source{remote, org}, []Source{remote, org}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := (&AgentsRC{Sources: tc.in}).ProjectOwnedSources()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ProjectOwnedSources() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestGenerateAgentsRC_SerializesNoSourcesAndNoUserResources inspects the
// DECODED JSON a generated manifest produces, not just the in-memory struct:
// the committed artifact is what a teammate clones, so `sources` must be
// absent (the scan's only root is the generating user's ~/.agents home) and
// every scan-derived set must carry project scope alone. The fixture home holds
// a global-scope skill/agent/rule beside the project-scope ones precisely so a
// regression that re-captures user-level resources fails here.
func TestGenerateAgentsRC_SerializesNoSourcesAndNoUserResources(t *testing.T) {
	t.Setenv("AGENTS_HOME", agentsHomeFixture(t))

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}

	if v, ok := raw["sources"]; ok {
		t.Errorf("generated manifest must declare no sources, got %v\n%s", v, data)
	}

	scoped := map[string][]any{
		"skills": {"skill-proj"},
		"agents": {"agent-proj"},
		"rules":  {"project"},
	}
	for key, want := range scoped {
		got, ok := raw[key]
		if !ok {
			t.Errorf("%q missing from generated manifest\n%s", key, data)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v (global-scope entries auto-resolve at the user level and must not be captured)", key, got, want)
		}
	}
}
