// Package graphstore — coverage for the bridge's parsing of upstream's
// `update` CLI output: the incremental line, the fell-back-to-full line, and
// the nothing-changed case.
package graphstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// commitSecond adds and commits a "b.go" file to extend the repo to two
// commits so HEAD~1 resolves.
func commitSecond(t *testing.T, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "b.go")
	cmd.Dir = repo
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "--quiet", "-m", "c2")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// fakeUpdateBridge returns a bridge whose CRG binary prints exactly the given
// line, so the parser is exercised against upstream's real output format.
func fakeUpdateBridge(t *testing.T, line string) *CRGBridge {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell binaries are POSIX-only")
	}
	repo, crgBin := makeFakeCRGEnv(t,
		"#!/bin/sh\necho '"+line+"'\nexit 0\n",
		"#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	commitSecond(t, repo)
	return &CRGBridge{RepoRoot: repo, Bin: crgBin}
}

// TestCRGBridge_UpdateReport_ParsesIncrementalLine pins the counters the
// bridge recovers from upstream's incremental line. The bridge cannot report
// the derived-view counters the CLI never prints, so they stay ABSENT.
func TestCRGBridge_UpdateReport_ParsesIncrementalLine(t *testing.T) {
	b := fakeUpdateBridge(t, "Incremental: 3 files updated, 12 nodes, 7 edges (postprocess=full)")
	rep, err := b.UpdateReport(UpdateOptions{Base: "HEAD~1"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.BuildType != crgBuildTypeIncremental {
		t.Errorf("build_type = %q, want %q", rep.BuildType, crgBuildTypeIncremental)
	}
	if rep.FilesUpdated == nil || *rep.FilesUpdated != 3 {
		t.Errorf("files_updated = %v, want 3", rep.FilesUpdated)
	}
	if rep.TotalNodes == nil || *rep.TotalNodes != 12 || rep.TotalEdges == nil || *rep.TotalEdges != 7 {
		t.Errorf("counters = %v/%v, want 12/7", rep.TotalNodes, rep.TotalEdges)
	}
	if rep.PostprocessLevel != PostprocessFull {
		t.Errorf("postprocess_level = %q, want %q", rep.PostprocessLevel, PostprocessFull)
	}
	if rep.FlowsDetected != nil {
		t.Error("flows_detected reported although the CLI never prints it")
	}
}

// TestCRGBridge_UpdateReport_NothingChanged pins upstream's summary for an
// update that re-parsed nothing: it is fully determined by the counters, so
// the bridge reports the release's own sentence rather than the CLI line.
func TestCRGBridge_UpdateReport_NothingChanged(t *testing.T) {
	b := fakeUpdateBridge(t, "Incremental: 0 files updated, 0 nodes, 0 edges (postprocess=minimal)")
	rep, err := b.UpdateReport(UpdateOptions{Base: "HEAD~1"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.Summary != crgNoChangesSummary {
		t.Errorf("summary = %q, want %q", rep.Summary, crgNoChangesSummary)
	}
	if rep.PostprocessLevel != PostprocessMinimal {
		t.Errorf("postprocess_level = %q, want %q", rep.PostprocessLevel, PostprocessMinimal)
	}
}

// TestCRGBridge_UpdateReport_FullRebuildFallback pins the line upstream
// prints when an automatic update found no usable incremental base: the
// report must read as a FULL build, not an incremental one.
func TestCRGBridge_UpdateReport_FullRebuildFallback(t *testing.T) {
	b := fakeUpdateBridge(t,
		"Full rebuild (no usable incremental base): 4 files, 16 nodes, 29 edges (postprocess=full)")
	rep, err := b.UpdateReport(UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.BuildType != crgBuildTypeFull {
		t.Fatalf("build_type = %q, want %q", rep.BuildType, crgBuildTypeFull)
	}
	if rep.FilesParsed == nil || *rep.FilesParsed != 4 {
		t.Errorf("files_parsed = %v, want 4", rep.FilesParsed)
	}
	if rep.BaseResolved == nil || rep.BaseResolved.Valid {
		t.Errorf("base_resolved = %#v, want a present null", rep.BaseResolved)
	}
	if rep.Summary != FullBuildSummary(4, 16, 29) {
		t.Errorf("summary = %q, want upstream's full-build sentence", rep.Summary)
	}
}

// TestCRGBridge_UpdateReport_ForcedFullRebuildUsesTheBuildCommand pins the
// FullRebuild option: it must route to `build`, not `update --base`.
func TestCRGBridge_UpdateReport_ForcedFullRebuildUsesTheBuildCommand(t *testing.T) {
	b := fakeUpdateBridge(t, "Full build: 2 files, 5 nodes, 3 edges (postprocess=none)")
	rep, err := b.UpdateReport(UpdateOptions{FullRebuild: true, Postprocess: PostprocessNone})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.BuildType != crgBuildTypeFull || rep.FilesUpdated != nil {
		t.Fatalf("report = %+v, want a full build with no files_updated", rep)
	}
}
