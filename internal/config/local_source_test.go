package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit is a hermetic GitRunner: no real git, repo, or network. It models the
// three states the local-source bootstrap cares about — "not a repo", "init'd
// but unborn", "has a commit" — plus scripted dirtiness, so tests exercise every
// branch deterministically.
type fakeGit struct {
	isRepo    bool
	initErr   error
	initCalls int
	head      string // "" => unborn branch (rev-parse HEAD fails)
	headErr   error
	dirty     bool
	statusErr error
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

func (f *fakeGit) Run(_ string, args ...string) (string, error) {
	switch strings.Join(args, " ") {
	case "rev-parse HEAD":
		if f.headErr != nil {
			return "", f.headErr
		}
		return f.head, nil
	case "status --porcelain":
		if f.statusErr != nil {
			return "", f.statusErr
		}
		if f.dirty {
			return " M skills/x/SKILL.md\n", nil
		}
		return "", nil
	default:
		return "", errors.New("unexpected git args: " + strings.Join(args, " "))
	}
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
			git:  &fakeGit{isRepo: true, head: "", headErr: errors.New("unborn")},
			want: emptyTreeRef,
		},
		{
			name: "unborn but dirty still versions deterministically",
			git:  &fakeGit{isRepo: true, headErr: errors.New("unborn"), dirty: true},
			want: emptyTreeRef + dirtySuffix,
		},
		{
			name: "status error treated as clean",
			git:  &fakeGit{isRepo: true, head: testHead, statusErr: errors.New("boom")},
			want: testHead,
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
			name:     "absolute path under root is made relative",
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

func TestNewLocalSourceNilRunnerDefaults(t *testing.T) {
	t.Parallel()
	s := NewLocalSource("/tmp/agents", nil)
	if s.Git == nil {
		t.Fatalf("expected default runner, got nil")
	}
	if _, ok := s.Git.(execGitRunner); !ok {
		t.Fatalf("expected exec-backed runner, got %T", s.Git)
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

func TestExecGitRunnerRoundTrip(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	r := NewExecGitRunner()
	if r.IsRepo(root) {
		t.Fatalf("fresh dir should not be a repo")
	}
	if err := r.Init(root); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !r.IsRepo(root) {
		t.Fatalf("dir should be a repo after init")
	}
	// rev-parse HEAD on an unborn branch fails => ResolvedRef falls back.
	s := NewLocalSource(root, r)
	ref, err := s.ResolvedRef()
	if err != nil {
		t.Fatalf("resolved ref: %v", err)
	}
	if ref != emptyTreeRef && !strings.HasPrefix(ref, emptyTreeRef) {
		t.Fatalf("unexpected ref for unborn repo: %q", ref)
	}
	// A failing git subcommand surfaces an error via Run.
	if _, err := r.Run(root, "rev-parse", "--verify", "definitely-not-a-ref"); err == nil {
		t.Fatalf("expected error for bad ref")
	}
}

func TestExecGitRunnerInitMkdirFailure(t *testing.T) {
	t.Parallel()
	// Place the would-be repo dir under a regular file so MkdirAll fails before
	// git runs, exercising Init's mkdir-error branch.
	parent := t.TempDir()
	fileBlock := filepath.Join(parent, "blocker")
	if err := os.WriteFile(fileBlock, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	r := NewExecGitRunner()
	if err := r.Init(filepath.Join(fileBlock, "repo")); err == nil {
		t.Fatalf("expected mkdir failure under a file path")
	}
}

func TestExecGitRunnerExitErrorStderr(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	r := NewExecGitRunner()
	if err := r.Init(root); err != nil {
		t.Fatalf("init: %v", err)
	}
	// A non-zero git exit carries stderr; gitError must surface it.
	_, err := r.Run(root, "rev-parse", "--verify", "no-such-ref")
	if err == nil {
		t.Fatalf("expected non-zero exit error")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected wrapped *exec.ExitError, got %v", err)
	}
}

func TestExecGitRunnerNonExitError(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := NewExecGitRunner()
	// A non-existent working directory makes exec fail to start the process —
	// a non-ExitError failure, exercising gitError's fallback branch.
	_, err := r.Run(filepath.Join(t.TempDir(), "does-not-exist"), "status")
	if err == nil {
		t.Fatalf("expected start failure for missing working dir")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("expected a non-ExitError failure, got ExitError: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
