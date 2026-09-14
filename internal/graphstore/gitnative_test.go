package graphstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// These tests pin the changed-file contract `update` depends on. The behaviour
// under test is specifically THREE-DOT: `git diff base...HEAD` diffs the merge
// base against HEAD, so work that landed on base after the branch point is not
// reported. A two-dot substitution changes the operator-visible changed-file
// set the moment base moves on, which is why it gets its own test rather than
// being assumed equivalent.

// commitFiles writes files into repo and commits them, returning nothing: the
// fixtures assert on paths, not hashes.
func commitFiles(t *testing.T, repo, message string, files map[string]string) {
	t.Helper()
	writeFiles(t, repo, files)
	runGitFixture(t, repo, "add", "-A")
	runGitFixture(t, repo, "commit", "--quiet", "-m", message)
}

// branchedFixture builds the shape that makes three-dot semantics observable:
//
//	main:    base ── mainOnly.go
//	           └── feature ── featureOnly.go  (HEAD)
//
// merge-base(main, HEAD) is `base`, so a three-dot diff reports featureOnly.go
// and NOT mainOnly.go; a two-dot diff would report neither correctly.
func branchedFixture(t *testing.T) string {
	t.Helper()
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"base.go": "package base\n"})
	runGitFixture(t, repo, "checkout", "--quiet", "-b", "feature")
	commitFiles(t, repo, "feature work", map[string]string{"featureOnly.go": "package f\n"})
	runGitFixture(t, repo, "checkout", "--quiet", "main")
	commitFiles(t, repo, "main work", map[string]string{"mainOnly.go": "package m\n"})
	runGitFixture(t, repo, "checkout", "--quiet", "feature")
	return repo
}

// TestChangedFilesAgainst_UsesMergeBaseNotTwoDot is the core contract.
func TestChangedFilesAgainst_UsesMergeBaseNotTwoDot(t *testing.T) {
	repo := branchedFixture(t)

	files, err := changedFilesAgainst(repo, "main")
	if err != nil {
		t.Fatalf("changedFilesAgainst: %v", err)
	}
	if strings.Join(files, ",") != "featureOnly.go" {
		t.Errorf("changed files = %v, want [featureOnly.go]: a three-dot diff starts at the "+
			"merge base, so main's own commit must not appear", files)
	}
}

// TestChangedFilesAgainst_ExcludesDeletionsOnly pins the ACMRTUXB filter. Every
// letter except D has a destination path in a tree-to-tree diff, so the rule is
// "report the destination, drop the deletion".
func TestChangedFilesAgainst_ExcludesDeletionsOnly(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{
		"keep.go":   "package keep\n",
		"gone.go":   "package gone\n",
		"change.go": "package change\n",
	})
	writeFiles(t, repo, map[string]string{
		"change.go": "package change\n\nfunc F() {}\n",
		"added.go":  "package added\n",
	})
	runGitFixture(t, repo, "rm", "--quiet", "gone.go")
	runGitFixture(t, repo, "add", "-A")
	runGitFixture(t, repo, "commit", "--quiet", "-m", "add, modify, delete")

	files, err := changedFilesAgainst(repo, "HEAD~1")
	if err != nil {
		t.Fatalf("changedFilesAgainst: %v", err)
	}
	if strings.Join(files, ",") != "added.go,change.go" {
		t.Errorf("changed files = %v, want [added.go change.go]: additions and modifications "+
			"are reported, the deletion is not, and an untouched file never appears", files)
	}
}

// TestChangedFilesAgainst_RenameReportsDestination: `--name-only` prints a
// rename's destination, and rename detection is on by default in git's own
// `git diff`. Reporting the source instead would point the graph update at a
// file that no longer exists.
func TestChangedFilesAgainst_RenameReportsDestination(t *testing.T) {
	requireGit(t)
	body := "package widget\n\n// Widget is large enough that similarity detection is unambiguous.\n" +
		"func Widget() string { return \"widget\" }\n"
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"old/widget.go": body})
	// Content-identical delete + add: an exact rename, which is what git's own
	// rename detection reports as R100 rather than as a D plus an A pair.
	writeFiles(t, repo, map[string]string{"new/widget.go": body})
	runGitFixture(t, repo, "rm", "--quiet", "old/widget.go")
	runGitFixture(t, repo, "add", "-A")
	runGitFixture(t, repo, "commit", "--quiet", "-m", "move widget")

	files, err := changedFilesAgainst(repo, "HEAD~1")
	if err != nil {
		t.Fatalf("changedFilesAgainst: %v", err)
	}
	if strings.Join(files, ",") != "new/widget.go" {
		t.Errorf("changed files = %v, want [new/widget.go]", files)
	}
}

// TestChangedFilesAgainst_CopyReportsDestination: a copy keeps its source
// intact, so both the new path and (if it changed) the old one are reported —
// never neither.
func TestChangedFilesAgainst_CopyReportsDestination(t *testing.T) {
	requireGit(t)
	body := "package widget\n\nfunc Widget() string { return \"widget\" }\n"
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a/widget.go": body})
	commitFiles(t, repo, "copy widget", map[string]string{"b/widget.go": body})

	files, err := changedFilesAgainst(repo, "HEAD~1")
	if err != nil {
		t.Fatalf("changedFilesAgainst: %v", err)
	}
	if strings.Join(files, ",") != "b/widget.go" {
		t.Errorf("changed files = %v, want [b/widget.go]", files)
	}
}

// TestChangedFilesAgainst_PathsAreRepoRelativeSlashed: the paths feed a CRG
// changed-file list and an operator-visible report, so they must be stable
// repository-relative slash paths on every platform.
func TestChangedFilesAgainst_PathsAreRepoRelativeSlashed(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
	commitFiles(t, repo, "nested", map[string]string{
		"pkg/sub/deep.go": "package sub\n",
		"pkg/top.go":      "package pkg\n",
	})

	files, err := changedFilesAgainst(repo, "HEAD~1")
	if err != nil {
		t.Fatalf("changedFilesAgainst: %v", err)
	}
	if strings.Join(files, ",") != "pkg/sub/deep.go,pkg/top.go" {
		t.Errorf("changed files = %v, want sorted repo-relative slash paths", files)
	}
	if filepath.Separator != '/' {
		for _, f := range files {
			if strings.ContainsRune(f, filepath.Separator) {
				t.Errorf("%q carries an OS separator; paths must always be slashed", f)
			}
		}
	}
}

// TestChangedFilesAgainst_DetachedHEAD: HEAD is a direct hash reference in a
// detached checkout and must resolve like any other.
func TestChangedFilesAgainst_DetachedHEAD(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
	commitFiles(t, repo, "second", map[string]string{"b.go": "package b\n"})
	runGitFixture(t, repo, "checkout", "--quiet", "--detach")

	files, err := changedFilesAgainst(repo, "HEAD~1")
	if err != nil {
		t.Fatalf("changedFilesAgainst on a detached HEAD: %v", err)
	}
	if strings.Join(files, ",") != "b.go" {
		t.Errorf("changed files = %v, want [b.go]", files)
	}
}

// TestChangedFilesAgainst_Errors covers the states git itself refuses rather
// than reporting an empty diff: no parent to diff against, an unknown base, an
// unrelated history with no merge base, and a directory that is not a
// repository. Each must be an error, because "no changed files" and "I could
// not tell you" drive completely different operator decisions.
func TestChangedFilesAgainst_Errors(t *testing.T) {
	requireGit(t)
	t.Run("HEAD has no parent", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
		if _, err := changedFilesAgainst(repo, "HEAD~1"); err == nil {
			t.Fatal("a single-commit repository has no HEAD~1 to diff against")
		}
	})
	t.Run("unknown base", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
		_, err := changedFilesAgainst(repo, "no-such-ref")
		if err == nil || !strings.Contains(err.Error(), "no-such-ref") {
			t.Fatalf("expected an error naming the missing base, got %v", err)
		}
	})
	t.Run("no merge base", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
		// An orphan branch shares no history with main.
		runGitFixture(t, repo, "checkout", "--quiet", "--orphan", "orphan")
		runGitFixture(t, repo, "rm", "--quiet", "-rf", ".")
		commitFiles(t, repo, "orphan root", map[string]string{"z.go": "package z\n"})
		_, err := changedFilesAgainst(repo, "main")
		if err == nil || !strings.Contains(err.Error(), "no merge base") {
			t.Fatalf("expected a no-merge-base error, got %v", err)
		}
	})
	t.Run("empty repository", func(t *testing.T) {
		repo := filepath.Join(t.TempDir(), "empty")
		writeFiles(t, repo, map[string]string{"a.go": "package a\n"})
		runGitFixture(t, repo, "init", "--quiet", "--initial-branch=main")
		if _, err := changedFilesAgainst(repo, "HEAD~1"); err == nil {
			t.Fatal("a repository with no commits cannot resolve HEAD")
		}
	})
	t.Run("not a repository", func(t *testing.T) {
		_, err := changedFilesAgainst(t.TempDir(), "HEAD~1")
		if err == nil || !strings.Contains(err.Error(), "open git repository") {
			t.Fatalf("expected an open-repository error, got %v", err)
		}
	})
}

// TestGitChangedFiles_DefaultBaseAndErrorSpelling: the bridge defaults to
// HEAD~1 and keeps the `base...HEAD` spelling in its error so an operator can
// still see which three-dot range was evaluated.
func TestGitChangedFiles_DefaultBaseAndErrorSpelling(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
	commitFiles(t, repo, "second", map[string]string{"b.go": "package b\n"})
	bridge := &CRGBridge{RepoRoot: repo}

	files, err := bridge.gitChangedFiles("")
	if err != nil {
		t.Fatalf("gitChangedFiles(\"\"): %v", err)
	}
	if strings.Join(files, ",") != "b.go" {
		t.Errorf("default base must be %s: got %v", defaultDiffBase, files)
	}

	_, err = (&CRGBridge{RepoRoot: t.TempDir()}).gitChangedFiles("origin/main")
	if err == nil || !strings.Contains(err.Error(), "git diff origin/main...HEAD") {
		t.Fatalf("expected the three-dot range in the error, got %v", err)
	}
}

// ── index ground truth ──────────────────────────────────────────────────────

// setIndex replaces repo's index with the given entries. Writing the index
// directly is the only way to produce the malformed shapes the enumeration has
// to defend against — git itself will not create an escaping or duplicated
// gitlink, but a merge of someone else's superproject can leave one behind.
func setIndex(t *testing.T, repoRoot string, entries ...*index.Entry) {
	t.Helper()
	repo, err := git.PlainOpen(repoRoot)
	if err != nil {
		t.Fatalf("open %s: %v", repoRoot, err)
	}
	if err := repo.Storer.SetIndex(&index.Index{Version: 2, Entries: entries}); err != nil {
		t.Fatalf("write index: %v", err)
	}
}

// gitlinkEntry is one submodule index entry at the given path and stage.
func gitlinkEntry(path string, stage index.Stage) *index.Entry {
	return &index.Entry{
		Name:  path,
		Mode:  filemode.Submodule,
		Hash:  plumbing.NewHash("1111111111111111111111111111111111111111"),
		Stage: stage,
	}
}

// TestGitlinkPaths_RejectsEscapingDuplicateAndSelfEntries pins the three
// index-hygiene rules discovery depends on. Each rejected shape would
// otherwise become a build working directory, a CRG `--repo` argument, and a
// database ATTACH target:
//
//   - `../evil` and an absolute path escape the checkout entirely.
//   - `.` resolves to the superproject, which would be built and merged a
//     second time as if it were a separate repository.
//   - a conflicted gitlink carries one entry per merge stage, and three
//     identical roots would be walked, built, and merged three times.
func TestGitlinkPaths_RejectsEscapingDuplicateAndSelfEntries(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
	setIndex(t, repo,
		gitlinkEntry("vendor/lib", 1),
		gitlinkEntry("vendor/lib", 2),
		gitlinkEntry("vendor/lib", 3),
		gitlinkEntry("../evil", 0),
		gitlinkEntry("/etc/passwd", 0),
		gitlinkEntry(".", 0),
		gitlinkEntry("", 0),
		gitlinkEntry("vendor/other", 0),
	)

	got, err := gitlinkPaths(repo)
	if err != nil {
		t.Fatalf("gitlinkPaths: %v", err)
	}
	if strings.Join(got, ",") != "vendor/lib,vendor/other" {
		t.Errorf("gitlinkPaths = %v, want the two contained paths exactly once each", got)
	}
}

// TestReadIndex_CorruptIndexIsAnError: an index git cannot parse must surface
// as an error, never as "this repository has no submodules and no files" —
// that false negative is the whole failure mode this package exists to close.
func TestReadIndex_CorruptIndexIsAnError(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
	if err := os.WriteFile(filepath.Join(repo, ".git", "index"), []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := DiscoverSubmodules(repo); err == nil || !strings.Contains(err.Error(), "read git index") {
		t.Errorf("DiscoverSubmodules over a corrupt index = %v, want a read-index error", err)
	}
	if _, err := EnumerateTrackedFiles(repo, false); err == nil || !strings.Contains(err.Error(), "read git index") {
		t.Errorf("EnumerateTrackedFiles over a corrupt index = %v, want a read-index error", err)
	}
}

// ── object-store failures ───────────────────────────────────────────────────

// removeObject deletes one loose object from repo's store. A fresh fixture
// repository has never been packed, so every object is a loose file — which
// makes "the object store cannot produce this" a reproducible state rather
// than an untestable defensive branch.
func removeObject(t *testing.T, repoRoot string, hash plumbing.Hash) {
	t.Helper()
	h := hash.String()
	path := filepath.Join(repoRoot, ".git", "objects", h[:2], h[2:])
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove object %s: %v", h, err)
	}
}

// headCommit opens repo and returns the commit HEAD points at.
func headCommit(t *testing.T, repoRoot string) *object.Commit {
	t.Helper()
	repo, err := git.PlainOpen(repoRoot)
	if err != nil {
		t.Fatalf("open %s: %v", repoRoot, err)
	}
	commit, err := resolveCommit(repo, "HEAD")
	if err != nil {
		t.Fatalf("resolve HEAD: %v", err)
	}
	return commit
}

// TestChangedFilesAgainst_UnreadableObjectStore: a commit, tree, or subtree the
// object store cannot produce is an error. Reporting an empty changed-file set
// instead would tell `update` there was nothing to do.
func TestChangedFilesAgainst_UnreadableObjectStore(t *testing.T) {
	requireGit(t)
	t.Run("missing HEAD tree", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
		commitFiles(t, repo, "second", map[string]string{"b.go": "package b\n"})
		removeObject(t, repo, headCommit(t, repo).TreeHash)

		_, err := changedFilesAgainst(repo, "HEAD~1")
		if err == nil || !strings.Contains(err.Error(), "read tree of") {
			t.Fatalf("expected a tree-read error, got %v", err)
		}
	})
	t.Run("missing merge-base tree", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
		base := headCommit(t, repo)
		commitFiles(t, repo, "second", map[string]string{"b.go": "package b\n"})
		removeObject(t, repo, base.TreeHash)

		_, err := changedFilesAgainst(repo, "HEAD~1")
		if err == nil || !strings.Contains(err.Error(), "read tree of") {
			t.Fatalf("expected a tree-read error, got %v", err)
		}
	})
	t.Run("cancelled walk is an error, not a short answer", func(t *testing.T) {
		// The diff runs under the package request timeout. An already-expired
		// context must surface as an error: a partially walked tree reported
		// as the complete changed-file set would tell `update` there is less
		// to do than there is.
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"a.go": "package a\n"})
		commitFiles(t, repo, "second", map[string]string{"b.go": "package b\n"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := changedPaths(ctx, headCommit(t, repo), headCommit(t, repo))
		if err == nil || !strings.Contains(err.Error(), "diff trees") {
			t.Fatalf("expected a tree-diff error for a cancelled walk, got %v", err)
		}
	})
	t.Run("missing ancestor breaks the merge-base walk", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"), map[string]string{"base.go": "package base\n"})
		root := headCommit(t, repo)
		runGitFixture(t, repo, "checkout", "--quiet", "-b", "feature")
		commitFiles(t, repo, "feature work", map[string]string{"f.go": "package f\n"})
		runGitFixture(t, repo, "checkout", "--quiet", "main")
		commitFiles(t, repo, "main work", map[string]string{"m.go": "package m\n"})
		runGitFixture(t, repo, "checkout", "--quiet", "feature")
		// Both tips still resolve; only their shared ancestor is gone, so the
		// failure is specifically the merge-base computation.
		removeObject(t, repo, root.Hash)

		_, err := changedFilesAgainst(repo, "main")
		if err == nil || !strings.Contains(err.Error(), "merge base of main and HEAD") {
			t.Fatalf("expected a merge-base error, got %v", err)
		}
	})
}
