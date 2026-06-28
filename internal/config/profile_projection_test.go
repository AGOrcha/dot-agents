package config

import (
	"reflect"
	"testing"
)

func TestProjectProfileSnapshotIsIndependent(t *testing.T) {
	resolved := ResolvedProfile{
		Digest:       "abc123",
		Contributing: []string{"repo:b", "repo:a"},
		Bundle:       map[string]any{"tools": map[string]any{"allow": []any{"Edit"}}},
	}
	proj := ProjectProfile(resolved)
	if proj.SourceDigest != "abc123" {
		t.Fatalf("source digest = %q, want abc123", proj.SourceDigest)
	}
	if !reflect.DeepEqual(proj.Refs, []string{"repo:a", "repo:b"}) {
		t.Fatalf("refs = %v, want sorted [repo:a repo:b]", proj.Refs)
	}
	// The original ref slice must be untouched (sortedCopy does not reorder input).
	if !reflect.DeepEqual(resolved.Contributing, []string{"repo:b", "repo:a"}) {
		t.Fatalf("ProjectProfile mutated the source ref slice: %v", resolved.Contributing)
	}
	// Mutating the source bundle after projection must not change the projection.
	resolved.Bundle["tools"].(map[string]any)["allow"] = []any{"Write"}
	got := proj.Allowlist("tools.allow")
	if !reflect.DeepEqual(got, []string{"Edit"}) {
		t.Fatalf("projection deep-copy leaked source mutation: %v", got)
	}
}

func TestProjectProfileEmptyBundle(t *testing.T) {
	proj := ProjectProfile(ResolvedProfile{Digest: "d"})
	if proj.Bundle == nil {
		t.Fatal("projection bundle must be a non-nil empty map for an empty resolution")
	}
	if len(proj.Allowlist("tools.allow")) != 0 {
		t.Fatal("an empty bundle must project an empty allowlist")
	}
}

func TestProjectionAllowlist(t *testing.T) {
	proj := ProjectProfile(ResolvedProfile{Bundle: map[string]any{
		"skills": map[string]any{"allow": []any{"plan-wave-picker", "agent-start"}},
		"model":  "claude",                  // scalar, not an array
		"mixed":  []any{"keep", 7, "alsok"}, // non-string members are dropped
	}})
	cases := []struct {
		path string
		want []string
	}{
		{"skills.allow", []string{"agent-start", "plan-wave-picker"}}, // sorted
		{"model", nil},       // leaf is a scalar, not an array
		{"absent.path", nil}, // missing path
		{"mixed", []string{"alsok", "keep"}},
	}
	for _, tc := range cases {
		got := proj.Allowlist(tc.path)
		want := tc.want
		if want == nil {
			want = []string{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Allowlist(%q) = %v, want %v", tc.path, got, want)
		}
	}
}
