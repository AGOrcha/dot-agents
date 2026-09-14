package crgbehavior

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// hashOf builds a deterministic 40-hex commit hash from a digit.
func hashOf(digit string) plumbing.Hash {
	return plumbing.NewHash(strings.Repeat(digit, 40))
}

// fakeRepo answers the corpus builder's repository reads from a canned script,
// so every failure branch is reachable without staging a repository.
type fakeRepo struct {
	head    plumbing.Hash
	order   []plumbing.Hash
	subject map[plumbing.Hash]string
	changes map[plumbing.Hash][]FileChange
	fail    map[string]error
}

func (r fakeRepo) Resolve(string) (plumbing.Hash, error) {
	if err := r.fail["resolve"]; err != nil {
		return plumbing.ZeroHash, err
	}
	return r.head, nil
}

func (r fakeRepo) Window(_ string, count int) ([]plumbing.Hash, error) {
	if err := r.fail["window"]; err != nil {
		return nil, err
	}
	if count < len(r.order) {
		return r.order[:count], nil
	}
	return r.order, nil
}

func (r fakeRepo) Subject(hash plumbing.Hash) (string, error) {
	if err := r.fail["subject"]; err != nil {
		return "", err
	}
	return r.subject[hash], nil
}

func (r fakeRepo) Changes(hash plumbing.Hash) ([]FileChange, error) {
	if err := r.fail["changes"]; err != nil {
		return nil, err
	}
	return r.changes[hash], nil
}

// added is a file the commit created.
func added(path string, lines ...string) FileChange {
	return FileChange{Path: path, After: lines}
}

// The previous builder recognized only the Go and Python declaration forms, so
// a TypeScript, Rust, Java or Ruby commit was pinned with an EMPTY identifier
// list and silently left the FTS search surface unexercised.
func TestScanChangesExtractsIdentifiersPerLanguage(t *testing.T) {
	rel := testRelease()
	rel.Languages = map[string]string{
		".go": "go", ".ts": "typescript", ".rs": "rust", ".java": "java",
		".rb": "ruby", ".yml": "yaml",
	}
	changes := []FileChange{
		added("pkg/a.go", "package pkg", "func Handle() {}", "type Widget struct{}"),
		added("web/app.ts", "export function render() {}", "export const useThing = (): T => {}", "interface Props {}"),
		added("src/lib.rs", "pub async fn spawn() {}", "pub struct Engine {}"),
		added("src/Main.java", "public final class Main {", "    public static void launch(String[] a) {"),
		added("lib/thing.rb", "def perform", "class Worker"),
		added("docs/README.md", "func NotIndexed() {}"),
		added("ci/config.yml", "jobs:"),
	}

	files, languages, identifiers := scanChanges(changes, rel)
	if containsString(files, "docs/README.md") {
		t.Fatalf("an unindexed file entered the task: %v", files)
	}
	if !containsString(files, "ci/config.yml") {
		t.Fatalf("a release-indexed file was dropped as docs-only: %v", files)
	}
	for _, lang := range []string{"go", "typescript", "rust", "java", "ruby", "yaml"} {
		if !containsString(languages, lang) {
			t.Fatalf("languages = %v, want %q recorded", languages, lang)
		}
	}
	for _, want := range []string{
		"Handle", "Widget", // go
		"render", "useThing", "Props", // typescript
		"spawn", "Engine", // rust
		"Main", "launch", // java
		"perform", "Worker", // ruby
	} {
		if !containsString(identifiers, want) {
			t.Fatalf("identifiers = %v, want %q extracted", identifiers, want)
		}
	}
	if containsString(identifiers, "NotIndexed") {
		t.Fatalf("a declaration from an unindexed file leaked in: %v", identifiers)
	}
}

// Identifiers are the declaration SETS' symmetric difference, computed from
// content. Deriving them from diff hunks instead would make the corpus depend
// on which of several equally valid alignments git's xdiff happened to pick —
// and no other differ reproduces that choice.
func TestChangedDeclarationsAreASetDifferenceNotADiffReading(t *testing.T) {
	change := FileChange{
		Path:   "pkg/a.go",
		Before: []string{"package pkg", "func Kept() {}", "func Removed() {}"},
		After:  []string{"package pkg", "func Kept() {}", "func Added() {}"},
	}
	got := changedDeclarations(change, "go")
	if !equalStrings(got, []string{"Added", "Removed"}) {
		t.Fatalf("changed declarations = %v, want exactly the added and removed ones", got)
	}
	// A file whose declarations are untouched contributes nothing, even though
	// its body changed.
	body := FileChange{
		Path:   "pkg/a.go",
		Before: []string{"func Kept() {", "  return 1", "}"},
		After:  []string{"func Kept() {", "  return 2", "}"},
	}
	if got := changedDeclarations(body, "go"); len(got) != 0 {
		t.Fatalf("a body-only change reported %v, want no declaration change", got)
	}
}

// A language the release indexes but the builder cannot extract declarations
// for is REPORTED, because such a commit can never contribute an FTS search
// input and that has to be visible when judging surface coverage.
func TestExtractorCoverageReportsUncoveredLanguages(t *testing.T) {
	rel := testRelease()
	rel.Languages = map[string]string{".go": "go", ".xyz": "madeup"}
	covered, uncovered := ExtractorCoverage(rel)
	if !containsString(covered, "go") {
		t.Fatalf("covered = %v, want go", covered)
	}
	if !containsString(uncovered, "madeup") {
		t.Fatalf("uncovered = %v, want the extractor-less language reported", uncovered)
	}
}

// Each pinned commit costs a full release build, so the corpus is a sample. A
// newest-N sample of this repository is almost entirely one language, which is
// how a run ends up never exercising the release's behavior for any other.
func TestSelectTasksPinsLanguageCoverageFirst(t *testing.T) {
	candidates := []Task{
		{Commit: "a", Languages: []string{"go"}},
		{Commit: "b", Languages: []string{"go"}},
		{Commit: "c", Languages: []string{"go"}},
		{Commit: "d", Languages: []string{"rust"}},
	}
	var commits []string
	for _, task := range selectTasks(candidates, 2) {
		commits = append(commits, task.Commit)
	}
	if !equalStrings(commits, []string{"a", "d"}) {
		t.Fatalf("selected %v, want the newest Go commit plus the only Rust one", commits)
	}
	if all := selectTasks(candidates, 10); len(all) != len(candidates) {
		t.Fatalf("selected %d of %d when the cap exceeds the window", len(all), len(candidates))
	}
}

// A regenerated manifest records the release and the scanned window, because
// both decide which commits were eligible and what coverage the sample can
// claim.
func TestBuildManifestRecordsReleaseAndWindow(t *testing.T) {
	indexed, docsOnly := hashOf("1"), hashOf("2")
	r := fakeRepo{
		head:  hashOf("f"),
		order: []plumbing.Hash{indexed, docsOnly},
		subject: map[plumbing.Hash]string{
			indexed: "feat: go change", docsOnly: "docs: nothing indexed",
		},
		changes: map[plumbing.Hash][]FileChange{
			indexed:  {added("pkg/a.go", "func Handle() {}")},
			docsOnly: {added("docs/README.md", "text")},
		},
	}
	m, err := buildManifest(r, "", 0, 0, testRelease())
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	if m.SchemaVersion != ManifestSchemaVersion || m.Release != PinnedVersion {
		t.Fatalf("manifest = schema %d release %q, want %d / %q",
			m.SchemaVersion, m.Release, ManifestSchemaVersion, PinnedVersion)
	}
	if m.Window != DefaultCommitWindow || m.GeneratedFrom != DefaultRef || m.Head != hashOf("f").String() {
		t.Fatalf("provenance = window %d from %s at %s", m.Window, m.GeneratedFrom, m.Head)
	}
	if len(m.Tasks) != 1 || m.Tasks[0].Commit != indexed.String() {
		t.Fatalf("tasks = %+v, want only the commit touching an indexed file", m.Tasks)
	}
	if !equalStrings(m.Tasks[0].Languages, []string{"go"}) ||
		!equalStrings(m.Tasks[0].Identifiers, []string{"Handle"}) {
		t.Fatalf("task = %+v, want its language and declaration recorded", m.Tasks[0])
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("a freshly built manifest failed validation: %v", err)
	}
}

// A corpus that can never supply an FTS search input is a corpus defect, not a
// gate result: it would leave a required surface permanently unexercised.
func TestBuildManifestRejectsAnInadequateCorpus(t *testing.T) {
	sha := hashOf("1")
	cases := map[string][]FileChange{
		"nothing indexed": {added("docs/README.md", "text")},
		"no declarations": {added("pkg/a.go", "// only a comment moved")},
	}
	for name, changes := range cases {
		t.Run(name, func(t *testing.T) {
			r := fakeRepo{
				head: sha, order: []plumbing.Hash{sha},
				subject: map[plumbing.Hash]string{sha: "x"},
				changes: map[plumbing.Hash][]FileChange{sha: changes},
			}
			if _, err := buildManifest(r, "ref", 5, 5, testRelease()); err == nil {
				t.Fatalf("buildManifest accepted a corpus with %s", name)
			}
		})
	}
}

// Every repository read the builder makes must surface its failure, not be
// swallowed into an empty corpus.
func TestBuildManifestPropagatesRepositoryFailures(t *testing.T) {
	sha := hashOf("1")
	for _, op := range []string{"resolve", "window", "subject", "changes"} {
		t.Run(op, func(t *testing.T) {
			r := fakeRepo{
				head: sha, order: []plumbing.Hash{sha},
				subject: map[plumbing.Hash]string{sha: "x"},
				changes: map[plumbing.Hash][]FileChange{sha: {added("pkg/a.go", "func Handle() {}")}},
				fail:    map[string]error{op: errors.New("staged failure")},
			}
			_, err := buildManifest(r, "ref", 1, 1, testRelease())
			if err == nil || !strings.Contains(err.Error(), "staged failure") {
				t.Fatalf("buildManifest error = %v, want the %s failure propagated", err, op)
			}
		})
	}
}

// A manifest from an older builder or another release cannot be replayed
// reproducibly, so it is rejected rather than silently misread.
func TestManifestValidateRejectsStaleCorpora(t *testing.T) {
	base := func() Manifest {
		return Manifest{SchemaVersion: ManifestSchemaVersion, Release: PinnedVersion,
			Tasks: []Task{{Commit: "sha", ChangedFiles: []string{"pkg/a.go"}}}}
	}
	cases := map[string]func(*Manifest){
		"older schema":   func(m *Manifest) { m.SchemaVersion = 1 },
		"other release":  func(m *Manifest) { m.Release = "2.2.0" },
		"no tasks":       func(m *Manifest) { m.Tasks = nil },
		"task no commit": func(m *Manifest) { m.Tasks[0].Commit = "" },
		"task no files":  func(m *Manifest) { m.Tasks[0].ChangedFiles = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := base()
			mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatalf("Validate accepted a manifest with %s", name)
			}
		})
	}
}

// The checked-in corpus must be replayable by THIS builder and this release,
// and it must cover more than one language — a single-language sample cannot
// exercise the release's parser, FTS or flow behavior for anything else.
func TestCheckedInManifestIsValid(t *testing.T) {
	m, err := LoadManifest("../../" + DefaultManifestPath)
	if err != nil {
		t.Fatalf("load corpus manifest: %v", err)
	}
	languages, withIdentifiers := map[string]bool{}, 0
	for _, task := range m.Tasks {
		for _, lang := range task.Languages {
			languages[lang] = true
		}
		if len(task.Identifiers) > 0 {
			withIdentifiers++
		}
	}
	if len(languages) < 2 {
		t.Fatalf("the pinned corpus covers only %v — a single-language corpus cannot exercise the release's parser surface", languages)
	}
	if withIdentifiers == 0 {
		t.Fatal("no pinned task changed a declaration, so fts_search can never be exercised")
	}
}

// ciTGitRepo stages a two-commit repository spanning two release languages.
// Staging is native (go-git in-process): the gate executes no git subprocess,
// and neither does its test bed, so the fixture depends on neither an ambient
// git binary nor the developer's global git configuration.
func ciTGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	// Distinct, increasing committer times: the window is ordered by committer
	// time, so equal timestamps would make "newest first" ambiguous.
	base := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	ciTCommit(t, repo, root, "pkg/a.go", "package pkg\n\nfunc Handle() {}\n", "feat: add Handle", base)
	ciTCommit(t, repo, root, "web/app.ts", "export function render() {}\n", "feat: add render",
		base.Add(time.Minute))
	return root
}

// ciTCommit writes one file and commits it at an explicit committer time.
func ciTCommit(t *testing.T, repo *git.Repository, root, name, body, subject string, when time.Time) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	sig := &object.Signature{Name: "Gate", Email: "gate@example.test", When: when}
	if _, err := wt.Commit(subject, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit %s: %v", subject, err)
	}
}

// ciTOneGoCommit is the smallest corpus the builder accepts: one commit that
// changes one extractable Go declaration.
func ciTOneGoCommit() fakeRepo {
	sha := hashOf("1")
	return fakeRepo{
		head: sha, order: []plumbing.Hash{sha},
		subject: map[plumbing.Hash]string{sha: "feat: add Handle"},
		changes: map[plumbing.Hash][]FileChange{sha: {added("pkg/a.go", "func Handle() {}")}},
	}
}

// BuildManifest is the entry point `crgbehaviorgate -regen` calls. It must
// derive the corpus from REAL history — resolving the ref, walking the window
// and diffing each commit against its parent — not merely from the injected
// seam the other builder tests drive.
func TestCiTBuildManifestReadsRealHistory(t *testing.T) {
	m, err := BuildManifest(ciTGitRepo(t), "HEAD", 5, 10, testRelease())
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if len(m.Tasks) != 2 {
		t.Fatalf("tasks = %+v, want both indexed commits pinned", m.Tasks)
	}
	if m.Tasks[0].Subject != "feat: add render" || m.Tasks[1].Subject != "feat: add Handle" {
		t.Fatalf("subjects = %q then %q, want the newest commit first",
			m.Tasks[0].Subject, m.Tasks[1].Subject)
	}
	if m.Head != m.Tasks[0].Commit {
		t.Fatalf("head %s is not the newest pinned commit %s", m.Head, m.Tasks[0].Commit)
	}
	if !equalStrings(m.Tasks[0].Identifiers, []string{"render"}) ||
		!equalStrings(m.Tasks[1].Identifiers, []string{"Handle"}) {
		t.Fatalf("identifiers = %v then %v, want each commit's own declaration",
			m.Tasks[0].Identifiers, m.Tasks[1].Identifiers)
	}
	if !equalStrings(m.Tasks[0].ChangedFiles, []string{"web/app.ts"}) {
		t.Fatalf("changed files = %v, want only the commit's own post-image file",
			m.Tasks[0].ChangedFiles)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("a manifest built from real history failed validation: %v", err)
	}
}

// A root that is not a repository, and a ref nothing resolves, are corpus
// defects the caller must see. Returning an empty manifest would let -regen
// overwrite the pinned corpus with nothing.
func TestCiTBuildManifestRejectsUnusableRepositories(t *testing.T) {
	t.Run("not a repository", func(t *testing.T) {
		_, err := BuildManifest(t.TempDir(), "HEAD", 5, 10, testRelease())
		ciTWantError(t, "BuildManifest", err, "open repository")
	})
	t.Run("unresolvable ref", func(t *testing.T) {
		_, err := BuildManifest(ciTGitRepo(t), "refs/heads/no-such-branch", 5, 10, testRelease())
		ciTWantError(t, "BuildManifest", err, "resolve refs/heads/no-such-branch")
	})
}

// The scan window is what the pinned sample was drawn from, so it has to scale
// with the corpus size: pinning 30 commits out of a 250-commit window would
// leave language coverage almost nothing to reserve from.
func TestCiTScanWindowScalesWithCorpusSize(t *testing.T) {
	cases := []struct {
		name   string
		count  int
		window int
		want   int
	}{
		{name: "defaults keep the floor", want: DefaultCommitWindow},
		{name: "a small corpus keeps the floor", count: 5, want: DefaultCommitWindow},
		{name: "a large corpus widens the window", count: 30, want: 300},
		{name: "an explicit window wins", count: 30, window: 7, want: 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := buildManifest(ciTOneGoCommit(), "ref", tc.count, tc.window, testRelease())
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			if m.Window != tc.want {
				t.Fatalf("recorded window = %d, want %d", m.Window, tc.want)
			}
		})
	}
}

// Language coverage is reserved BEFORE the newest-first remainder, and the
// result still reads chronologically. Filling newest-first alone would sample
// this repository almost entirely in one language, which is exactly how a run
// reports a verdict while never exercising the release's behavior elsewhere.
func TestCiTSelectTasksReservesLanguagesThenFillsNewestFirst(t *testing.T) {
	mixed := []Task{
		{Commit: "a", Languages: []string{"go"}},
		{Commit: "b", Languages: []string{"go"}},
		{Commit: "c", Languages: []string{"go"}},
		{Commit: "d", Languages: []string{"rust"}},
		{Commit: "e", Languages: []string{"python"}},
	}
	oneLanguage := mixed[:3]
	cases := []struct {
		name       string
		candidates []Task
		count      int
		want       []string
	}{
		{"reservation alone fills the budget", mixed, 2, []string{"a", "d"}},
		{"the remainder fills newest-first", mixed, 4, []string{"a", "b", "d", "e"}},
		{"a single-language window pins the newest", oneLanguage, 2, []string{"a", "b"}},
		{"a budget at the candidate count keeps everything", mixed, 5,
			[]string{"a", "b", "c", "d", "e"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, task := range selectTasks(tc.candidates, tc.count) {
				got = append(got, task.Commit)
			}
			if !equalStrings(got, tc.want) {
				t.Fatalf("selected %v, want %v", got, tc.want)
			}
		})
	}
}
