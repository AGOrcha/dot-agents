package crgbehavior

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/execabs"
)

// newCorpusRepo builds a throwaway git repository with one code commit and one
// docs-only commit, so the corpus builder's filtering is exercised against real
// git output rather than a stub.
func newCorpusRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.email", "gate@example.com")
	mustGit(t, dir, "config", "user.name", "Gate Test")
	writeRepoFile(t, dir, "pkg/thing.go", "package pkg\n\nfunc Thing() {}\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "feat: add Thing")
	writeRepoFile(t, dir, "pkg/thing.go", "package pkg\n\nfunc Thing() {}\n\ntype Widget struct{}\n\nfunc (w Widget) Do() {}\n")
	writeRepoFile(t, dir, "docs/notes.md", "notes\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "feat: add Widget")
	writeRepoFile(t, dir, "docs/more.md", "more\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "docs: only docs")
	return dir
}

func writeRepoFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := execabs.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func TestBuildManifestPinsRealHistory(t *testing.T) {
	repo := newCorpusRepo(t)
	m, err := BuildManifest(repo, "main", 10)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("generated manifest must validate: %v", err)
	}
	if m.GeneratedFrom != "main" || m.Head == "" {
		t.Fatalf("manifest must pin the generation ref and head: %+v", m)
	}
	if len(m.Tasks) != 2 {
		t.Fatalf("docs-only commit must not become a review task; got %d tasks", len(m.Tasks))
	}
	newest := m.Tasks[0]
	if newest.Subject != "feat: add Widget" {
		t.Fatalf("tasks must be newest-first, got %q", newest.Subject)
	}
	if len(newest.ChangedFiles) != 1 || newest.ChangedFiles[0] != "pkg/thing.go" {
		t.Fatalf("only graph-indexable files belong in the query input: %v", newest.ChangedFiles)
	}
	if strings.Join(newest.Identifiers, ",") != "Do,Widget" {
		t.Fatalf("changed identifiers = %v, want the added type and method", newest.Identifiers)
	}
}

func TestBuildManifestDefaultsAndFailures(t *testing.T) {
	repo := newCorpusRepo(t)
	if _, err := BuildManifest(repo, "does-not-exist", 5); err == nil {
		t.Fatal("an unknown ref must fail rather than pin an empty corpus")
	}
	if _, err := BuildManifest(t.TempDir(), "main", 5); err == nil {
		t.Fatal("a non-repository must fail")
	}
	docsOnly := t.TempDir()
	mustGit(t, docsOnly, "init", "-b", "main")
	mustGit(t, docsOnly, "config", "user.email", "gate@example.com")
	mustGit(t, docsOnly, "config", "user.name", "Gate Test")
	writeRepoFile(t, docsOnly, "README.md", "hi\n")
	mustGit(t, docsOnly, "add", ".")
	mustGit(t, docsOnly, "commit", "-m", "docs: readme")
	if _, err := BuildManifest(docsOnly, "main", 0); err == nil {
		t.Fatal("a history with no indexable file must fail loudly, not yield an empty corpus")
	}
}

func TestBuildManifestDefaultRefIsUsedWhenBlank(t *testing.T) {
	repo := newCorpusRepo(t)
	mustGit(t, repo, "branch", "-f", "master", "main")
	mustGit(t, repo, "remote", "add", "origin", repo)
	mustGit(t, repo, "update-ref", "refs/remotes/origin/master", "master")
	m, err := BuildManifest(repo, "", 0)
	if err != nil {
		t.Fatalf("blank ref must fall back to %s: %v", DefaultRef, err)
	}
	if m.GeneratedFrom != DefaultRef {
		t.Fatalf("GeneratedFrom = %q, want %q", m.GeneratedFrom, DefaultRef)
	}
}

// scriptedGit fails the nth git invocation, so every failure branch of the
// corpus builder is reachable without staging a broken repository.
type scriptedGit struct {
	real     gitRunner
	failOn   int
	calls    int
	failWith error
}

func (s *scriptedGit) run(args ...string) (string, error) {
	s.calls++
	if s.calls == s.failOn {
		return "", s.failWith
	}
	return s.real.run(args...)
}

func TestBuildManifestPropagatesEveryGitFailure(t *testing.T) {
	repo := newCorpusRepo(t)
	// 1: rev-parse, 2: rev-list, 3: show -s (subject), 4: show (patch).
	for _, failOn := range []int{1, 2, 3, 4} {
		g := &scriptedGit{real: execGit{repoRoot: repo}, failOn: failOn, failWith: errStub}
		if _, err := buildManifest(g, "main", 10); !errors.Is(err, errStub) {
			t.Fatalf("git call %d failure must propagate, got %v", failOn, err)
		}
	}
}

var errStub = errors.New("git unavailable")

func TestDiffFilesReadsPostImagePaths(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/pkg/a.go b/pkg/a.go",
		"--- a/pkg/a.go",
		"+++ b/pkg/a.go",
		"diff --git a/gone.go b/gone.go",
		"--- a/gone.go",
		"+++ /dev/null",
	}, "\n")
	got := diffFiles(diff)
	if len(got) != 1 || got[0] != "pkg/a.go" {
		t.Fatalf("diffFiles = %v, want only the surviving post-image path", got)
	}
}

func TestIndexableFilesDeduplicatesAndFilters(t *testing.T) {
	got := indexableFiles([]string{"b.go", "a.py", "b.go", "README.md", "x.yaml", "s.TS"})
	want := "a.py,b.go,s.TS"
	if strings.Join(got, ",") != want {
		t.Fatalf("indexableFiles = %v, want %s", got, want)
	}
}

func TestChangedIdentifiersCoversDeclarationForms(t *testing.T) {
	diff := strings.Join([]string{
		"+func Exported() {}",
		"-func (r Recv) Method() {}",
		"+type Shape struct{}",
		"+    def helper(self):",
		"+class Thing:",
		"+	callSite()",
		" func Untouched() {}",
		"+func Exported() {}",
	}, "\n")
	got := strings.Join(changedIdentifiers(diff), ",")
	want := "Exported,Method,Shape,Thing,helper"
	if got != want {
		t.Fatalf("changedIdentifiers = %q, want %q (declarations only, deduplicated, sorted)", got, want)
	}
	if len(changedIdentifiers("")) != 0 {
		t.Fatal("an empty diff must yield an empty identifier list, not nil surprises")
	}
}
