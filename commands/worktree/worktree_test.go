package worktree

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/AGOrcha/dot-agents/internal/gitwt"
)

func TestNewCmdWiring(t *testing.T) {
	cmd := NewCmd()
	if cmd.Name() != "worktree" {
		t.Fatalf("Use=%q, want worktree", cmd.Name())
	}
	subs := map[string]*cobra.Command{}
	for _, c := range cmd.Commands() {
		subs[c.Name()] = c
	}
	for _, want := range []struct {
		name  string
		flags []string
	}{
		{"create", []string{"name", "path", "base-branch", "purpose", "parent-pr"}},
		{"merge-back", []string{"name", "onto"}},
	} {
		sub, ok := subs[want.name]
		if !ok {
			t.Fatalf("missing subcommand %q", want.name)
		}
		for _, fl := range want.flags {
			if sub.Flags().Lookup(fl) == nil {
				t.Fatalf("subcommand %q missing flag --%s", want.name, fl)
			}
		}
	}
}

// TestRequiredFlagsEnforced proves the create/merge-back required flags are
// enforced before RunE (so no git repo is even touched).
func TestRequiredFlagsEnforced(t *testing.T) {
	for _, args := range [][]string{{"create"}, {"merge-back"}} {
		cmd := NewCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs(args)
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("%v with no flags succeeded, want required-flag error", args)
		}
		if !strings.Contains(err.Error(), "required") {
			t.Fatalf("%v error=%q, want a required-flag error", args, err)
		}
	}
}

func TestJSONOut(t *testing.T) {
	withFlag := &cobra.Command{Use: "x"}
	withFlag.Flags().Bool("json", false, "")
	if jsonOut(withFlag) {
		t.Fatal("jsonOut=true with default flag, want false")
	}
	if err := withFlag.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json: %v", err)
	}
	if !jsonOut(withFlag) {
		t.Fatal("jsonOut=false after --json set, want true")
	}
	if jsonOut(&cobra.Command{Use: "y"}) {
		t.Fatal("jsonOut=true when no json flag present, want false")
	}
}

// TestCreateMergeBackE2E drives the full CLI path: create records the base,
// merge-back reads that recorded base and fast-forwards the parent to the sub
// tip.
func TestCreateMergeBackE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	out := execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--purpose", "wt3 slice", "--parent-pr", "409")
	if !strings.Contains(out, "recorded base") {
		t.Fatalf("create output=%q, want 'recorded base'", out)
	}
	if got := branchRef(t, repoDir, "sub"); got != base {
		t.Fatalf("sub branch at %s, want base %s", got, base)
	}

	subHead := advanceWorktree(t, repoDir, wtDir)
	if subHead == base {
		t.Fatal("sub-branch did not advance")
	}

	out = execWorktree(t, "merge-back", "--name", "sub", "--onto", "parent")
	if !strings.Contains(out, "fast-forwarded") {
		t.Fatalf("merge-back output=%q, want 'fast-forwarded'", out)
	}
	if got := branchRef(t, repoDir, "parent"); got != subHead {
		t.Fatalf("parent tip after merge-back=%s, want sub tip %s", got, subHead)
	}
}

// TestMergeBackStaleBaseE2E proves the CLI surfaces the stale-base guard: after
// the parent advances, merge-back errors instead of silently re-deriving.
func TestMergeBackStaleBaseE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	execWorktree(t, "create", "--name", "sub", "--path", wtDir, "--base-branch", "parent")
	advanceWorktree(t, repoDir, wtDir)

	// Parent advances after the base was recorded.
	advanced := commitOnMain(t, repoDir, "adv.txt", "moved\n", "advance parent")
	setBranchRef(t, repoDir, "parent", advanced)

	cmd := NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"merge-back", "--name", "sub", "--onto", "parent"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("merge-back onto an advanced parent succeeded, want stale-base error (out=%q)", buf.String())
	}
	if got := branchRef(t, repoDir, "parent"); got != advanced {
		t.Fatalf("parent moved to %s despite stale-base failure, want %s", got, advanced)
	}
}

// --- helpers ---

func initRepo(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	dir := filepath.Join(root, "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.User.Name = "Test"
	cfg.User.Email = "t@example.com"
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}
	h := commitOnMain(t, dir, "README.md", "hello\n", "initial")
	return dir, h
}

func commitOnMain(t *testing.T, repoDir, name, content, msg string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	h, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return h
}

func setBranchRef(t *testing.T, repoDir, name string, h plumbing.Hash) {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), h)
	if err := repo.Storer.SetReference(ref); err != nil {
		t.Fatalf("set branch %s: %v", name, err)
	}
}

func branchRef(t *testing.T, repoDir, name string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(name), true)
	if err != nil {
		t.Fatalf("resolve branch %s: %v", name, err)
	}
	return ref.Hash()
}

func advanceWorktree(t *testing.T, repoDir, wtDir string) plumbing.Hash {
	t.Helper()
	mgr, err := gitwt.NewManager(repoDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	wt, err := mgr.Open(wtDir)
	if err != nil {
		t.Fatalf("Open worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "work.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}
	if err := wt.Stage("work.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	h, err := wt.Commit("wip", &gitwt.CommitOptions{AuthorName: "W", AuthorEmail: "w@x"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return h
}

func execWorktree(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v (out=%q)", args, err, buf.String())
	}
	return buf.String()
}
