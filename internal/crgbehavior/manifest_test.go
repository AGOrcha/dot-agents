package crgbehavior

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ciTStage writes body to path, creating the parent directory. It is how the
// loader tests put a REAL file on disk rather than stubbing the read.
func ciTStage(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("stage dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("stage %s: %v", path, err)
	}
}

// ciTJSON renders v the way a checked-in artifact is encoded, so a loader test
// feeds the loader exactly the bytes a real file would carry.
func ciTJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode staged artifact: %v", err)
	}
	return string(data)
}

// ciTLoadCase is one unusable on-disk artifact. An empty body means the file is
// never created at all.
type ciTLoadCase struct {
	name string
	body string
	want string
}

// ciTStagedPath returns the path a load case's artifact lives at, having
// written it when the case supplies a body.
func ciTStagedPath(t *testing.T, name string, tc ciTLoadCase) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if tc.body != "" {
		ciTStage(t, path, tc.body)
	}
	return path
}

// ciTWantError fails unless err names the expected failure.
func ciTWantError(t *testing.T, what string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s accepted an unusable artifact, want a failure naming %q", what, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want one naming %q", what, err, want)
	}
}

// ciTManifest is a minimal manifest that passes Validate.
func ciTManifest() Manifest {
	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		GeneratedAt:   "2026-03-01T12:00:00Z",
		GeneratedFrom: DefaultRef,
		Head:          strings.Repeat("f", 40),
		Window:        DefaultCommitWindow,
		Release:       PinnedVersion,
		Tasks: []Task{{
			Commit:       strings.Repeat("a", 40),
			Subject:      "feat: add Handle",
			ChangedFiles: []string{"pkg/a.go"},
			Identifiers:  []string{"Handle"},
			Languages:    []string{"go"},
		}},
	}
}

// A manifest the gate cannot read is not a corpus. Each failure mode has to
// name itself: silently returning the zero Manifest would let a gate run report
// a verdict over no pinned commits at all.
func TestCiTLoadManifestRejectsUnusableFiles(t *testing.T) {
	stale := ciTManifest()
	stale.SchemaVersion = 1
	cases := []ciTLoadCase{
		{name: "no such file", want: "read manifest"},
		{name: "malformed json", body: "{\"tasks\":", want: "parse manifest"},
		{name: "stale schema", body: ciTJSON(t, stale), want: "schema_version 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadManifest(ciTStagedPath(t, "manifest.json", tc))
			ciTWantError(t, "LoadManifest", err, tc.want)
		})
	}
}

// Regeneration is an explicit command whose output a human reviews as a diff,
// so the written manifest must be pretty-printed, newline-terminated, and read
// back by LoadManifest as exactly the manifest that was saved.
func TestCiTManifestSaveRoundTrips(t *testing.T) {
	m := ciTManifest()
	// The parent directory is created: -regen writes into a fresh tree.
	path := filepath.Join(t.TempDir(), "testdata", "crg-behavior", "manifest.json")
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the saved manifest: %v", err)
	}
	if !strings.Contains(string(data), "\n  \"generated_from\": \""+DefaultRef+"\",") {
		t.Fatalf("the saved manifest is not indented JSON:\n%s", data)
	}
	if !strings.HasSuffix(string(data), "}\n") || strings.HasSuffix(string(data), "}\n\n") {
		t.Fatalf("the saved manifest is not terminated by exactly one newline: %q", data)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("reload the saved manifest: %v", err)
	}
	if !reflect.DeepEqual(loaded, m) {
		t.Fatalf("round-tripped manifest = %+v, want %+v", loaded, m)
	}
}

// A regeneration that cannot persist its corpus must say so. Reporting success
// would leave the checked-in manifest stale while the run claims it was
// rewritten, and each failure is attributed to its own step.
func TestCiTWriteJSONReportsDestinationFailures(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	ciTStage(t, blocker, "occupied")

	cases := []struct {
		name  string
		path  string
		value any
		want  string
	}{
		{
			name:  "value cannot be encoded",
			path:  filepath.Join(dir, "unencodable.json"),
			value: make(chan int),
			want:  "encode manifest",
		},
		{
			name:  "a parent path component is a file",
			path:  filepath.Join(blocker, "nested", "manifest.json"),
			value: ciTManifest(),
			want:  "create manifest dir",
		},
		{
			name:  "the destination is an existing directory",
			path:  dir,
			value: ciTManifest(),
			want:  "write manifest",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciTWantError(t, "writeJSON", writeJSON(tc.path, tc.value), tc.want)
			if _, err := os.Stat(tc.path); err == nil && tc.path != dir {
				t.Fatalf("a failed write left %s behind", tc.path)
			}
		})
	}
}
