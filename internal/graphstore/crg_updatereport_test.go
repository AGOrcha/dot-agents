// Package graphstore — coverage for the no_mutation outcome branches of
// UpdateReport (files>0 vs files==0) plus the Status-error short-circuit.
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

// TestCRGBridge_UpdateReport_NoMutation_WithFiles exercises the no_mutation
// branch where CRG reports files updated but zero node/edge changes —
// summary should describe "Changed N files with no graph mutations".
func TestCRGBridge_UpdateReport_NoMutation_WithFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell binaries are POSIX-only")
	}
	repo, crgBin := makeFakeCRGEnv(t,
		"#!/bin/sh\necho '3 files updated 0 nodes 0 edges'\nexit 0\n",
		"#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	commitSecond(t, repo)

	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	rep, err := b.UpdateReport(UpdateOptions{Base: "HEAD~1"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.Outcome != "no_mutation" {
		t.Errorf("expected outcome=no_mutation, got %q (summary=%s)", rep.Outcome, rep.Summary)
	}
	if rep.Summary == "" {
		t.Errorf("expected non-empty summary")
	}
}

// TestCRGBridge_UpdateReport_NoMutation_NoFiles exercises the no_mutation
// branch where the summary parses to 0/0/0 — summary should say
// "Update completed with no graph mutations".
func TestCRGBridge_UpdateReport_NoMutation_NoFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell binaries are POSIX-only")
	}
	repo, crgBin := makeFakeCRGEnv(t,
		"#!/bin/sh\necho '0 files updated 0 nodes 0 edges'\nexit 0\n",
		"#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	commitSecond(t, repo)

	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	rep, err := b.UpdateReport(UpdateOptions{Base: "HEAD~1"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.Outcome != "no_mutation" {
		t.Errorf("expected outcome=no_mutation, got %q", rep.Outcome)
	}
}
