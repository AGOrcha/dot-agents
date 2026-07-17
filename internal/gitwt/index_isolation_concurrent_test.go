package gitwt

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
)

// concurrentCase is one goroutine's private slice of the workload: its own
// linked worktree (branch + path) and the single file it stages/commits.
type concurrentCase struct {
	branch string
	path   string
	file   string
	body   string
}

// TestConcurrentIndexIsolation is the concurrent counterpart to the sequential
// TestIndexIsolation. N goroutines each drive their OWN linked worktree
// (open -> write -> stage -> commit) simultaneously, then we prove per-worktree
// index/HEAD isolation held: a commit made in one worktree never appears in
// another worktree's committed tree, working dir, or status, nor in the main
// repo, and only that worktree's branch advanced.
//
// It is a genuine concurrency test — meaningful only under `go test -race`. All
// goroutines block on a shared start gate so they hit write/stage/commit at the
// same instant, maximizing overlap. If the implementation shared a single index
// or HEAD across worktrees (isolation broken), that aligned staging would
// cross-contaminate committed trees and branch tips (caught by the assertions)
// and mutate shared state without synchronization (caught by the race detector).
func TestConcurrentIndexIsolation(t *testing.T) {
	const n = 6
	f := newFixture(t)

	cases := setupConcurrentWorktrees(t, f, n)
	hashes, wts := runConcurrentCommits(t, f, cases)

	assertConcurrentBranchTips(t, f, cases, hashes)
	assertConcurrentTreeIsolation(t, f, cases, hashes)
	assertConcurrentWorktreeIsolation(t, cases, wts)
	assertConcurrentMainClean(t, f, cases)
}

// setupConcurrentWorktrees creates n linked worktrees serially. Worktree
// *creation* mutates shared repo admin state (refs/config) and is not the
// property under test, so it runs before the concurrent phase; the concurrency
// under test is the per-worktree write/stage/commit path itself.
func setupConcurrentWorktrees(t *testing.T, f *fixture, n int) []concurrentCase {
	t.Helper()
	cases := make([]concurrentCase, n)
	for i := range cases {
		branch := fmt.Sprintf("iso-c%d", i)
		cases[i] = concurrentCase{
			branch: branch,
			path:   f.wtPath(branch),
			file:   fmt.Sprintf("only-%d.txt", i),
			body:   fmt.Sprintf("worktree %d payload\n", i),
		}
		if err := f.mgr.AddBranch(branch, cases[i].path, f.base); err != nil {
			t.Fatalf("AddBranch %s: %v", branch, err)
		}
	}
	return cases
}

// runConcurrentCommits opens each worktree and drives write/stage/commit in
// parallel behind a shared start gate. Per-goroutine results are captured into
// disjoint slice slots (no shared mutable state), and done.Wait establishes the
// happens-before edge before the caller reads them.
func runConcurrentCommits(t *testing.T, f *fixture, cases []concurrentCase) ([]plumbing.Hash, []Worktree) {
	t.Helper()
	n := len(cases)
	hashes := make([]plumbing.Hash, n)
	wts := make([]Worktree, n)
	errs := make([]error, n)

	var ready, done sync.WaitGroup
	ready.Add(n)
	done.Add(n)
	start := make(chan struct{})

	for i := range cases {
		go func(i int) {
			defer done.Done()
			c := cases[i]
			wt, err := f.mgr.Open(c.path)
			wts[i] = wt
			ready.Done()
			<-start // release all goroutines at once
			if err != nil {
				errs[i] = fmt.Errorf("open: %w", err)
				return
			}
			hashes[i], errs[i] = commitOne(wt, c)
		}(i)
	}

	ready.Wait()
	close(start)
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d (%s): %v", i, cases[i].branch, err)
		}
	}
	return hashes, wts
}

// commitOne performs the isolated write -> stage -> commit for a single case.
func commitOne(wt Worktree, c concurrentCase) (plumbing.Hash, error) {
	if err := os.WriteFile(filepath.Join(c.path, c.file), []byte(c.body), 0o644); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("write %s: %w", c.file, err)
	}
	if err := wt.Stage(c.file); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("stage %s: %w", c.file, err)
	}
	h, err := wt.Commit("commit "+c.branch, &CommitOptions{
		AuthorName:  c.branch,
		AuthorEmail: c.branch + "@example.com",
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("commit: %w", err)
	}
	return h, nil
}

// assertConcurrentBranchTips checks every branch advanced to its own distinct,
// non-base commit and each branch ref points exactly at that commit — proving
// each goroutine's HEAD moved independently.
func assertConcurrentBranchTips(t *testing.T, f *fixture, cases []concurrentCase, hashes []plumbing.Hash) {
	t.Helper()
	seen := make(map[plumbing.Hash]int, len(hashes))
	for i, c := range cases {
		h := hashes[i]
		if h == plumbing.ZeroHash || h == f.base {
			t.Fatalf("%s did not advance past base (got %s)", c.branch, h)
		}
		if prev, dup := seen[h]; dup {
			t.Fatalf("%s and %s produced the same commit %s", cases[prev].branch, c.branch, h)
		}
		seen[h] = i

		ref, err := f.repo.Reference(plumbing.NewBranchReferenceName(c.branch), false)
		if err != nil {
			t.Fatalf("resolve branch %s: %v", c.branch, err)
		}
		if ref.Hash() != h {
			t.Fatalf("branch %s tip = %s, want its own commit %s", c.branch, ref.Hash(), h)
		}
	}
}

// assertConcurrentTreeIsolation checks each branch's committed tree carries ONLY
// its own new file (plus the shared README) — never any other goroutine's file.
// This is the committed-tree proof that no index cross-staging occurred.
func assertConcurrentTreeIsolation(t *testing.T, f *fixture, cases []concurrentCase, hashes []plumbing.Hash) {
	t.Helper()
	for i, c := range cases {
		assertOneCommitTreeIsolated(t, f, c, hashes[i], cases)
	}
}

// assertOneCommitTreeIsolated verifies one case's committed tree holds its own
// file and none of the other worktrees' files (no index cross-staging).
func assertOneCommitTreeIsolated(t *testing.T, f *fixture, c concurrentCase, hash plumbing.Hash, cases []concurrentCase) {
	t.Helper()
	commit, err := f.repo.CommitObject(hash)
	if err != nil {
		t.Fatalf("commit object %s: %v", c.branch, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("tree for %s: %v", c.branch, err)
	}
	names := make(map[string]bool, len(tree.Entries))
	for _, e := range tree.Entries {
		names[e.Name] = true
	}
	if !names[c.file] {
		t.Fatalf("%s tree missing its own %s", c.branch, c.file)
	}
	for _, other := range cases {
		if other.file == c.file {
			continue
		}
		if names[other.file] {
			t.Fatalf("%s tree leaked %s from another worktree", c.branch, other.file)
		}
	}
}

// assertConcurrentWorktreeIsolation checks each worktree's working directory and
// porcelain status: it holds only its own file, is clean after its commit, and
// no other goroutine's file ever appeared in its working tree or status.
func assertConcurrentWorktreeIsolation(t *testing.T, cases []concurrentCase, wts []Worktree) {
	t.Helper()
	for i, c := range cases {
		assertOneWorktreeIsolated(t, c, wts[i], cases)
	}
}

// assertOneWorktreeIsolated verifies one worktree is clean after its own commit
// and never saw another goroutine's file in its status or working dir.
func assertOneWorktreeIsolated(t *testing.T, c concurrentCase, wt Worktree, cases []concurrentCase) {
	t.Helper()
	st, err := wt.Status()
	if err != nil {
		t.Fatalf("status %s: %v", c.branch, err)
	}
	if !st.IsClean() {
		t.Fatalf("%s should be clean after committing its own file, got %v", c.branch, st)
	}
	for _, other := range cases {
		if _, tracked := st[other.file]; tracked {
			t.Fatalf("%s status contains %s from another worktree", c.branch, other.file)
		}
		if other.file == c.file {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(c.path, other.file)); !os.IsNotExist(statErr) {
			t.Fatalf("%s working dir leaked %s (stat err=%v)", c.branch, other.file, statErr)
		}
	}
}

// assertConcurrentMainClean checks none of the per-worktree files leaked into
// the main repo's status and that the main HEAD never moved off base.
func assertConcurrentMainClean(t *testing.T, f *fixture, cases []concurrentCase) {
	t.Helper()
	mainWT, err := f.repo.Worktree()
	if err != nil {
		t.Fatalf("main worktree: %v", err)
	}
	st, err := mainWT.Status()
	if err != nil {
		t.Fatalf("main status: %v", err)
	}
	for _, c := range cases {
		if _, leaked := st[c.file]; leaked {
			t.Fatalf("%s leaked into main repo status", c.file)
		}
	}
	head, err := f.repo.Head()
	if err != nil {
		t.Fatalf("main head: %v", err)
	}
	if head.Hash() != f.base {
		t.Fatalf("main HEAD moved to %s, must stay at base %s", head.Hash(), f.base)
	}
}
