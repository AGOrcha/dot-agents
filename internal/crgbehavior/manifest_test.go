package crgbehavior

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "manifest.json")
	want := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		GeneratedAt:   "2026-08-12T00:00:00Z",
		GeneratedFrom: "origin/master",
		Head:          "abc123",
		Tasks: []Task{{
			Commit:       "deadbeefdeadbeef",
			Subject:      "fix: thing",
			ChangedFiles: []string{"a.go"},
			Identifiers:  []string{"Thing"},
		}},
	}
	if err := want.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Head != want.Head || len(got.Tasks) != 1 || got.Tasks[0].Commit != want.Tasks[0].Commit {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got.Tasks[0].Identifiers[0] != "Thing" || got.Tasks[0].ChangedFiles[0] != "a.go" {
		t.Fatalf("round trip lost query inputs: %+v", got.Tasks[0])
	}
}

func TestLoadManifestRejectsMissingAndMalformed(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadManifest(filepath.Join(dir, "absent.json")); err == nil {
		t.Fatal("a missing manifest must be an error")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(bad); err == nil {
		t.Fatal("a malformed manifest must be an error")
	}
	stale := filepath.Join(dir, "stale.json")
	if err := os.WriteFile(stale, []byte(`{"schema_version":0,"tasks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(stale); err == nil {
		t.Fatal("an older schema_version must be rejected, not silently misread")
	}
}

func TestManifestValidate(t *testing.T) {
	cases := []struct {
		name string
		m    Manifest
		want string
	}{
		{"wrong version", Manifest{SchemaVersion: 99, Tasks: []Task{{Commit: "a"}}}, "schema_version"},
		{"no tasks", Manifest{SchemaVersion: ManifestSchemaVersion}, "no tasks"},
		{"task without commit", Manifest{SchemaVersion: ManifestSchemaVersion, Tasks: []Task{{}}}, "no commit"},
	}
	for _, tc := range cases {
		err := tc.m.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want one mentioning %q", tc.name, err, tc.want)
		}
	}
	ok := Manifest{SchemaVersion: ManifestSchemaVersion, Tasks: []Task{{Commit: "a"}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a well-formed manifest must validate: %v", err)
	}
}

func TestManifestSaveReportsUnwritablePath(t *testing.T) {
	// A path whose parent is an existing FILE cannot be created as a directory.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := Manifest{SchemaVersion: ManifestSchemaVersion, Tasks: []Task{{Commit: "a"}}}
	if err := m.Save(filepath.Join(file, "sub", "manifest.json")); err == nil {
		t.Fatal("saving under a file path must fail")
	}
}
