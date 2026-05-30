package config

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

// fakeGit is a hermetic GitRepo: no real repo or network. It models the states
// the local-source bootstrap cares about — "not a repo", "init'd but unborn",
// "has a commit" — plus scripted dirtiness and a resolve error, so tests
// exercise every branch deterministically.
type fakeGit struct {
	isRepo     bool
	initErr    error
	initCalls  int
	head       string // "" => unborn branch (Resolve returns empty commit)
	resolveErr error
	dirty      bool
}

func (f *fakeGit) IsRepo(string) bool { return f.isRepo }

func (f *fakeGit) Init(string) error {
	f.initCalls++
	if f.initErr != nil {
		return f.initErr
	}
	f.isRepo = true
	return nil
}

func (f *fakeGit) Resolve(string) (string, bool, error) {
	if f.resolveErr != nil {
		return "", false, f.resolveErr
	}
	return f.head, f.dirty, nil
}

const testHead = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

func TestEnsureBootstrapped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		root      string
		git       *fakeGit
		wantInit  bool
		wantErr   bool
		wantCalls int
	}{
		{
			name:      "fresh repo is initialized",
			root:      "/tmp/agents",
			git:       &fakeGit{isRepo: false},
			wantInit:  true,
			wantCalls: 1,
		},
		{
			name:      "already a repo is left untouched",
			root:      "/tmp/agents",
			git:       &fakeGit{isRepo: true},
			wantInit:  false,
			wantCalls: 0,
		},
		{
			name:    "empty root errors",
			root:    "",
			git:     &fakeGit{},
			wantErr: true,
		},
		{
			name:    "init failure propagates",
			root:    "/tmp/agents",
			git:     &fakeGit{isRepo: false, initErr: errors.New("permission denied")},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewLocalSource(tc.root, tc.git)
			gotInit, err := s.EnsureBootstrapped()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotInit != tc.wantInit {
				t.Fatalf("initialized = %v, want %v", gotInit, tc.wantInit)
			}
			if tc.git.initCalls != tc.wantCalls {
				t.Fatalf("init calls = %d, want %d", tc.git.initCalls, tc.wantCalls)
			}
		})
	}
}

func TestResolvedRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		git     *fakeGit
		want    string
		wantErr bool
	}{
		{
			name: "clean repo resolves to head commit",
			git:  &fakeGit{isRepo: true, head: testHead},
			want: testHead,
		},
		{
			name: "dirty repo gets dirty suffix",
			git:  &fakeGit{isRepo: true, head: testHead, dirty: true},
			want: testHead + dirtySuffix,
		},
		{
			name: "unborn branch resolves to empty-tree ref",
			git:  &fakeGit{isRepo: true, head: ""},
			want: emptyTreeRef,
		},
		{
			name: "unborn but dirty still versions deterministically",
			git:  &fakeGit{isRepo: true, head: "", dirty: true},
			want: emptyTreeRef + dirtySuffix,
		},
		{
			name:    "resolve error propagates",
			git:     &fakeGit{isRepo: true, resolveErr: errors.New("boom")},
			wantErr: true,
		},
		{
			name:    "non-repo errors",
			git:     &fakeGit{isRepo: false},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewLocalSource("/tmp/agents", tc.git)
			got, err := s.ResolvedRef()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ref = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLockKey(t *testing.T) {
	t.Parallel()
	// A slash-style root resolves identically on POSIX and Windows because
	// relPath slash-normalizes both sides before the lexical containment check.
	root := "/home/u/.agents"
	tests := []struct {
		name     string
		unitPath string
		want     string
	}{
		{
			name:     "repo-relative path",
			unitPath: "skills/foo/SKILL.md",
			want:     "local:skills/foo/SKILL.md@" + testHead,
		},
		{
			name:     "path under root is made relative",
			unitPath: root + "/agents/bar.md",
			want:     "local:agents/bar.md@" + testHead,
		},
		{
			name:     "path outside root kept as cleaned given",
			unitPath: "/elsewhere/baz.md",
			want:     "local:/elsewhere/baz.md@" + testHead,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewLocalSource(root, &fakeGit{isRepo: true, head: testHead})
			got, err := s.LockKey(tc.unitPath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("key = %q, want %q", got, tc.want)
			}
			if strings.ContainsRune(got, '\\') {
				t.Fatalf("lock key leaked a backslash separator: %q", got)
			}
		})
	}
}

// TestLockKeyOSNativeSeparators is the cross-OS regression for the Windows-only
// failure: an OS-native absolute path (built with filepath.Join, so it uses `\`
// on Windows and `/` on POSIX) under an OS-native root must yield a key whose
// path component is slash-delimited and root-relative on every platform.
func TestLockKeyOSNativeSeparators(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "dot-agents")
	unitPath := filepath.Join(root, "agents", "team", "bar.md")
	s := NewLocalSource(root, &fakeGit{isRepo: true, head: testHead})
	got, err := s.LockKey(unitPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "local:agents/team/bar.md@" + testHead
	if got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, '\\') {
		t.Fatalf("lock key leaked a backslash separator: %q", got)
	}
}

// TestLockKeyUnderRootEdges covers underRoot's exact-match and degenerate-root
// branches that the main table does not reach.
func TestLockKeyUnderRootEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		root     string
		unitPath string
		want     string
	}{
		{
			name:     "path equal to root resolves to dot",
			root:     "/home/u/.agents",
			unitPath: "/home/u/.agents",
			want:     "local:.@" + testHead,
		},
		{
			name:     "dot root never claims a path",
			root:     ".",
			unitPath: "skills/x.md",
			want:     "local:skills/x.md@" + testHead,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewLocalSource(tc.root, &fakeGit{isRepo: true, head: testHead})
			got, err := s.LockKey(tc.unitPath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("key = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLockKeyResolveError(t *testing.T) {
	t.Parallel()
	s := NewLocalSource("/tmp/agents", &fakeGit{isRepo: false})
	if _, err := s.LockKey("skills/x.md"); err == nil {
		t.Fatalf("expected error from unresolvable local source")
	}
}

func TestNewLocalSourceNilRepoDefaults(t *testing.T) {
	t.Parallel()
	s := NewLocalSource("/tmp/agents", nil)
	if s.Git == nil {
		t.Fatalf("expected default repo, got nil")
	}
	if _, ok := s.Git.(goGitRepo); !ok {
		t.Fatalf("expected in-process go-git repo, got %T", s.Git)
	}
}

func TestEnsureProvenanceGitignore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewLocalSource(root, &fakeGit{isRepo: true, head: testHead})

	// First write: managed block created from unsorted/duplicate input.
	if err := s.EnsureProvenanceGitignore([]string{"cache/", "agents/remote/", "cache/"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first := readFile(t, filepath.Join(root, gitignoreFileName))
	if !strings.Contains(first, gitignoreBlockBegin) || !strings.Contains(first, gitignoreBlockEnd) {
		t.Fatalf("managed markers missing:\n%s", first)
	}
	if strings.Index(first, "agents/remote/") > strings.Index(first, "cache/") {
		t.Fatalf("paths not sorted:\n%s", first)
	}
	if strings.Count(first, "cache/") != 1 {
		t.Fatalf("duplicate path not collapsed:\n%s", first)
	}

	// Idempotent: same inputs => identical bytes.
	if err := s.EnsureProvenanceGitignore([]string{"cache/", "agents/remote/"}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got := readFile(t, filepath.Join(root, gitignoreFileName)); got != first {
		t.Fatalf("not idempotent:\nfirst:\n%s\nsecond:\n%s", first, got)
	}
}

func TestEnsureProvenanceGitignorePreservesUserContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, gitignoreFileName)
	if err := os.WriteFile(path, []byte("# user\nnode_modules/\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := NewLocalSource(root, &fakeGit{isRepo: true, head: testHead})
	if err := s.EnsureProvenanceGitignore([]string{"cache/"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "node_modules/") || !strings.Contains(got, "# user") {
		t.Fatalf("user content not preserved:\n%s", got)
	}
	if !strings.Contains(got, "cache/") {
		t.Fatalf("managed path missing:\n%s", got)
	}

	// Rewriting the managed block must not duplicate user content.
	if err := s.EnsureProvenanceGitignore([]string{"cache/", "blobs/"}); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got = readFile(t, path)
	if strings.Count(got, "node_modules/") != 1 {
		t.Fatalf("user content duplicated on rewrite:\n%s", got)
	}
	if !strings.Contains(got, "blobs/") {
		t.Fatalf("new managed path missing:\n%s", got)
	}
}

func TestEnsureProvenanceGitignoreEmptyRemovesBlock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, gitignoreFileName)
	s := NewLocalSource(root, &fakeGit{isRepo: true, head: testHead})
	if err := s.EnsureProvenanceGitignore([]string{"cache/"}); err != nil {
		t.Fatalf("seed managed block: %v", err)
	}
	// Empty set, but user content present: block removed, user content kept.
	if err := os.WriteFile(path, []byte("# user\nlogs/\n"+gitignoreBlockBegin+"\ncache/\n"+gitignoreBlockEnd+"\n"), 0o644); err != nil {
		t.Fatalf("seed mixed: %v", err)
	}
	if err := s.EnsureProvenanceGitignore(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got := readFile(t, path)
	if strings.Contains(got, gitignoreBlockBegin) {
		t.Fatalf("managed block not removed:\n%s", got)
	}
	if !strings.Contains(got, "logs/") {
		t.Fatalf("user content lost:\n%s", got)
	}
}

func TestEnsureProvenanceGitignoreEmptyEverything(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewLocalSource(root, &fakeGit{isRepo: true})
	// Only blank-string paths and no prior file => empty result file.
	if err := s.EnsureProvenanceGitignore([]string{"", "   "}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readFile(t, filepath.Join(root, gitignoreFileName)); got != "" {
		t.Fatalf("expected empty gitignore, got %q", got)
	}
}

func TestEnsureProvenanceGitignoreEmptyRoot(t *testing.T) {
	t.Parallel()
	s := NewLocalSource("", &fakeGit{})
	if err := s.EnsureProvenanceGitignore([]string{"cache/"}); err == nil {
		t.Fatalf("expected error for empty root")
	}
}

func TestEnsureProvenanceGitignoreReadError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Make the .gitignore a directory so os.ReadFile fails with a non-NotExist error.
	if err := os.MkdirAll(filepath.Join(root, gitignoreFileName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	s := NewLocalSource(root, &fakeGit{isRepo: true})
	if err := s.EnsureProvenanceGitignore([]string{"cache/"}); err == nil {
		t.Fatalf("expected read error when .gitignore is a directory")
	}
}

func TestSplitLinesEmpty(t *testing.T) {
	t.Parallel()
	if got := splitLines(""); got != nil {
		t.Fatalf("splitLines(\"\") = %v, want nil", got)
	}
	if got := splitLines("a\nb\n"); len(got) != 2 {
		t.Fatalf("splitLines trailing newline = %v, want 2 elems", got)
	}
}

// TestGoGitRepoRoundTrip exercises the in-process go-git GitRepo through its
// full lifecycle on a real on-disk repo (no `git` binary, no PATH, no network):
// not-a-repo → init → unborn resolve → commit → committed resolve → dirty
// resolve. go-git is cross-platform, so this runs identically on every OS.
func TestGoGitRepoRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := NewGoGitRepo()
	if r.IsRepo(root) {
		t.Fatalf("fresh dir should not be a repo")
	}
	if err := r.Init(root); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !r.IsRepo(root) {
		t.Fatalf("dir should be a repo after init")
	}

	// Unborn branch: empty commit, not dirty, no error => empty-tree ref.
	s := NewLocalSource(root, r)
	if ref, err := s.ResolvedRef(); err != nil || ref != emptyTreeRef {
		t.Fatalf("unborn ResolvedRef = (%q, %v), want (%q, nil)", ref, err, emptyTreeRef)
	}

	// Make a commit: Resolve must report the full 40-char hash, clean.
	hash := commitFile(t, root, "README.md", "hello")
	commit, dirty, err := r.Resolve(root)
	if err != nil {
		t.Fatalf("resolve after commit: %v", err)
	}
	if commit != hash.String() {
		t.Fatalf("commit = %q, want %q", commit, hash.String())
	}
	if dirty {
		t.Fatalf("clean tree reported dirty")
	}
	if got, _ := s.ResolvedRef(); got != hash.String() {
		t.Fatalf("ResolvedRef = %q, want committed hash %q", got, hash.String())
	}

	// Now dirty the tree: Resolve reports dirty and ResolvedRef gets the suffix.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("dirty write: %v", err)
	}
	if _, dirty, _ := r.Resolve(root); !dirty {
		t.Fatalf("modified tree not reported dirty")
	}
	if got, _ := s.ResolvedRef(); got != hash.String()+dirtySuffix {
		t.Fatalf("dirty ResolvedRef = %q, want %q", got, hash.String()+dirtySuffix)
	}
}

func TestGoGitRepoResolveNonRepo(t *testing.T) {
	t.Parallel()
	r := NewGoGitRepo()
	if _, _, err := r.Resolve(t.TempDir()); err == nil {
		t.Fatalf("expected open error resolving a non-repo dir")
	}
}

func TestGoGitRepoInitAlreadyExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := NewGoGitRepo()
	if err := r.Init(root); err != nil {
		t.Fatalf("first init: %v", err)
	}
	// Re-initializing an existing repo makes go-git's PlainInit fail, exercising
	// Init's git-init error branch (mkdir succeeds, init does not).
	if err := r.Init(root); err == nil {
		t.Fatalf("expected error re-initializing an existing repo")
	}
}

func TestGoGitRepoResolveZeroHashHead(t *testing.T) {
	t.Parallel()
	// go-git resolves an unparseable HEAD to an all-zero reference (not an
	// error), so headCommitHash's zero-hash branch maps it to "" and Resolve
	// falls back to the deterministic empty-tree ref rather than emitting 40
	// literal zeros as a "real" commit.
	root := t.TempDir()
	r := NewGoGitRepo()
	if err := r.Init(root); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("garbage-not-a-ref\n"), 0o644); err != nil {
		t.Fatalf("corrupt HEAD: %v", err)
	}
	commit, _, err := r.Resolve(root)
	if err != nil {
		t.Fatalf("resolve zero-hash HEAD: %v", err)
	}
	if commit != "" {
		t.Fatalf("zero-hash HEAD commit = %q, want empty", commit)
	}
	if ref, _ := NewLocalSource(root, r).ResolvedRef(); ref != emptyTreeRef {
		t.Fatalf("ResolvedRef = %q, want %q", ref, emptyTreeRef)
	}
}

func TestGoGitRepoResolveBareRepoIsClean(t *testing.T) {
	t.Parallel()
	// A bare repo has no worktree, so worktreeStatus errors and worktreeDirty
	// conservatively reports clean. Resolve must still succeed with a non-dirty
	// (empty-tree) result rather than failing.
	root := t.TempDir()
	if _, err := git.PlainInit(root, true); err != nil {
		t.Fatalf("bare init: %v", err)
	}
	commit, dirty, err := NewGoGitRepo().Resolve(root)
	if err != nil {
		t.Fatalf("resolve bare repo: %v", err)
	}
	if dirty {
		t.Fatalf("bare repo (no worktree) must not report dirty")
	}
	if commit != "" {
		t.Fatalf("unborn bare repo commit = %q, want empty", commit)
	}
}

func TestGoGitRepoInitMkdirFailure(t *testing.T) {
	t.Parallel()
	// Place the would-be repo dir under a regular file so MkdirAll fails,
	// exercising Init's mkdir-error branch with no real git involved.
	fileBlock := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(fileBlock, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	r := NewGoGitRepo()
	if err := r.Init(filepath.Join(fileBlock, "repo")); err == nil {
		t.Fatalf("expected mkdir failure under a file path")
	}
}

// commitFile stages and commits a single file into the repo at root, returning
// the new commit hash. It uses go-git's in-process worktree API.
func commitFile(t *testing.T, root, name, content string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("add: %v", err)
	}
	hash, err := wt.Commit("seed", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@x", When: time.Unix(0, 0)},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return hash
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
