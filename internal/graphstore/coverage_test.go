package graphstore

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCoverageRoundTrip: a build's record of what it covered survives to the
// next status read, which is the whole point — status has no other way to tell
// "never looked at" from "looked and found nothing".
func TestCoverageRoundTrip(t *testing.T) {
	repo := t.TempDir()
	plan := WorkspacePlan{
		Roots: []WorkspaceRoot{
			{Path: "."},
			{Path: "vendor/lib"},
		},
		Skipped: []SkippedRoot{
			{Path: "vendor/excluded", Reason: SkipReasonExcluded},
			{Path: "vendor/uninit", Reason: SkipReasonUninitialized},
		},
	}
	if err := writeCoverage(repo, plan); err != nil {
		t.Fatalf("writeCoverage: %v", err)
	}

	got := readCoverage(repo)
	if !got.indexed("vendor/lib") {
		t.Error("an indexed submodule must be recorded")
	}
	if !got.excluded("vendor/excluded") {
		t.Error("an excluded submodule must be recorded")
	}
	// An uninitialized root is neither indexed nor excluded: it is missing, and
	// readiness must keep saying so.
	if got.indexed("vendor/uninit") || got.excluded("vendor/uninit") {
		t.Error("an uninitialized submodule must not be recorded as covered")
	}
	if got.indexed("vendor/unknown") || got.excluded("vendor/unknown") {
		t.Error("an unrecorded path must not match")
	}
}

// TestReadCoverage_TreatsUnusableRecordsAsAbsent: a missing, corrupt, or
// future-format sidecar degrades to "nothing known", never to a wrong answer
// about what was covered.
func TestReadCoverage_TreatsUnusableRecordsAsAbsent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		write   bool
	}{
		{name: "absent"},
		{name: "corrupt", content: "{not json", write: true},
		{name: "future schema", content: `{"schema_version":99,"indexed":["vendor/lib"]}`, write: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			if tc.write {
				writeRawCoverage(t, repo, tc.content)
			}
			if got := readCoverage(repo); got.indexed("vendor/lib") || got.excluded("vendor/lib") {
				t.Errorf("unusable record must read as empty, got %+v", got)
			}
		})
	}
}

// writeRawCoverage plants an arbitrary sidecar body, bypassing writeCoverage so
// malformed records can be exercised.
func writeRawCoverage(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(coveragePath(repo)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coveragePath(repo), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWriteCoverage_UnwritableLocation surfaces the failure rather than
// leaving a build silently unrecorded.
func TestWriteCoverage_UnwritableLocation(t *testing.T) {
	repo := t.TempDir()
	// Occupy the graph directory's path with a regular file so the record has
	// nowhere to go.
	if err := os.WriteFile(filepath.Dir(coveragePath(repo)), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCoverage(repo, WorkspacePlan{}); err == nil {
		t.Fatal("expected an error when the record cannot be written")
	}
}
