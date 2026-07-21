package workflow

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// TestBuildStateRefCommit_LinkedWorktreeCommonStore verifies the CAS-critical
// invariant for go-git in a LINKED WORKTREE (the production loop-worker case,
// which seedStateRefRepo's plain repo does not exercise): objects the in-process
// builder writes must land in the COMMON object store the untouched
// git-native update-ref swap reads, or the ref would point at an unreachable
// commit.
func TestBuildStateRefCommit_LinkedWorktreeCommonStore(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	wt := t.TempDir() + "/linked-wt"
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "--detach", wt).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	base := ".agents/workflow/plans/" + stateRefTestPlanID + "/"
	files := []stateRefFile{{relPath: base + "PLAN.yaml", content: []byte("plan: wt\n")}}

	commit, err := buildStateRefCommit(wt, "", files)
	if err != nil {
		t.Fatalf("buildStateRefCommit in linked worktree: %v", err)
	}
	// The commit object MUST be visible to git in the linked worktree — i.e.
	// go-git wrote it to the common objects dir, not the per-worktree gitdir.
	if out, err := exec.Command("git", "-C", wt, "cat-file", "-t", commit).CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "commit" {
		t.Fatalf("commit not in the store git reads from the worktree (wrong object store): err=%v out=%q", err, out)
	}
	// The untouched git-native CAS swap must install it and read it back.
	if err := compareAndSwapStateRef(wt, commit, ""); err != nil {
		t.Fatalf("compareAndSwapStateRef in worktree (object unreachable to update-ref?): %v", err)
	}
	if got := stateRefHead(wt); got != commit {
		t.Fatalf("stateRefHead(worktree) = %q, want %q", got, commit)
	}
	// The read path must resolve the blob written from the worktree.
	if raw, ok := readPlanFileFromStateRef(wt, stateRefTestPlanID); !ok || strings.TrimSpace(string(raw)) != "plan: wt" {
		t.Fatalf("read-back from worktree ref failed: ok=%v raw=%q", ok, raw)
	}
}
