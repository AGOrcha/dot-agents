package scoring

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- temp-repo harness -----------------------------------------------------

// gitRepo is a throwaway git repository built per test. Tests must not depend
// on the real dot-agents history — it is rebased freely — so every case here
// crafts its own commits and branches.
type gitRepo struct {
	t   *testing.T
	dir string
}

// newGitRepo initializes an empty repo on branch master with a hermetic
// identity and a deterministic environment.
func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	dir := t.TempDir()
	r := &gitRepo{t: t, dir: dir}
	r.run("init", "-q", "-b", "master")
	r.run("config", "user.email", "test@example.com")
	r.run("config", "user.name", "Test")
	r.run("config", "commit.gpgsign", "false")
	return r
}

// run executes a git command in the repo, failing the test on error.
func (r *gitRepo) run(args ...string) string {
	r.t.Helper()
	full := append([]string{"-C", r.dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// commit writes a file and commits it with the given message, returning the
// full SHA of the new commit.
func (r *gitRepo) commit(file, content, msg string) string {
	r.t.Helper()
	p := filepath.Join(r.dir, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", file, err)
	}
	r.run("add", "-A")
	r.run("commit", "-q", "-m", msg)
	return r.head()
}

// head returns the full SHA of the current branch tip.
func (r *gitRepo) head() string {
	r.t.Helper()
	return strings.TrimSpace(r.run("rev-parse", "HEAD"))
}

// writeTasks writes a canonical TASKS.yaml for planID under the repo's plan
// tree, so resolveWriteScope can find it.
func (r *gitRepo) writeTasks(planID, body string) {
	r.t.Helper()
	dir := filepath.Join(r.dir, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatalf("mkdir plan dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TASKS.yaml"), []byte(body), 0o644); err != nil {
		r.t.Fatalf("write TASKS.yaml: %v", err)
	}
}

// --- ExtractGitSignals: non-repo error -------------------------------------

func TestExtractGitSignals_NotARepo(t *testing.T) {
	dir := t.TempDir() // plain directory, no git
	_, err := ExtractGitSignals(IterationRecord{Commit: "abc"}, dir)
	if err == nil {
		t.Fatal("ExtractGitSignals on a non-repo dir = nil error, want error")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error = %q, want it to mention 'not a git repository'", err)
	}
}

// --- landed signal ---------------------------------------------------------

func TestExtractLanded_ReachableNotReverted(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")
	target := r.commit("b.txt", "two", "second")
	r.commit("c.txt", "three", "third")

	gs, err := ExtractGitSignals(IterationRecord{Commit: target}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 1.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 1.0", gs.LandedObserved)
	}
	if !strings.Contains(gs.LandedObserved.Detail, "reachable") {
		t.Errorf("detail = %q, want it to mention reachability", gs.LandedObserved.Detail)
	}
}

func TestExtractLanded_AbbreviatedSHAResolves(t *testing.T) {
	r := newGitRepo(t)
	full := r.commit("a.txt", "one", "first")
	r.commit("b.txt", "two", "second")

	// v1 entries carry abbreviated SHAs; an abbreviation that still resolves
	// must be treated identically to the full SHA.
	gs, err := ExtractGitSignals(IterationRecord{Commit: full[:8]}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 1.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 1.0", gs.LandedObserved)
	}
}

func TestExtractLanded_ReachableButReverted(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")
	target := r.commit("b.txt", "two", "second")
	// A later commit on master whose message reverts the target.
	r.commit("d.txt", "x", "Revert \"second\"\n\nThis reverts commit "+target+".")

	gs, err := ExtractGitSignals(IterationRecord{Commit: target}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 0.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 0.0 (reverted)", gs.LandedObserved)
	}
	if !strings.Contains(gs.LandedObserved.Detail, "reverted") {
		t.Errorf("detail = %q, want it to mention the revert", gs.LandedObserved.Detail)
	}
}

func TestExtractLanded_RevertReferencesAbbreviatedSHA(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")
	target := r.commit("b.txt", "two", "second")
	// Revert body names the target only by its abbreviation.
	r.commit("d.txt", "x", "Revert second change\n\nreverts "+target[:10])

	gs, err := ExtractGitSignals(IterationRecord{Commit: target}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 0.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 0.0 (abbrev-referenced revert)", gs.LandedObserved)
	}
}

func TestExtractLanded_OrphanedNotOnMaster(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")
	// A commit on a side branch never merged into master.
	r.run("checkout", "-q", "-b", "feature")
	orphan := r.commit("f.txt", "feat", "feature work")
	r.run("checkout", "-q", "master")

	gs, err := ExtractGitSignals(IterationRecord{Commit: orphan}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 0.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 0.0 (orphaned)", gs.LandedObserved)
	}
	if !strings.Contains(gs.LandedObserved.Detail, "not reachable") {
		t.Errorf("detail = %q, want it to mention unreachability", gs.LandedObserved.Detail)
	}
}

func TestExtractLanded_EmptyCommitIsAbsent(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")

	gs, err := ExtractGitSignals(IterationRecord{Commit: ""}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if gs.LandedObserved.Present {
		t.Errorf("LandedObserved = %+v, want absent for empty commit", gs.LandedObserved)
	}
	if !strings.Contains(gs.LandedObserved.Detail, "no commit SHA") {
		t.Errorf("detail = %q, want it to explain the empty SHA", gs.LandedObserved.Detail)
	}
}

func TestExtractLanded_UnresolvableSHAMessageMatch(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")
	landed := r.commit("b.txt", "two", "implement the widget")

	// The recorded SHA is from since-rebased history and no longer resolves,
	// but the impl summary still matches a master commit subject.
	rec := IterationRecord{
		Commit: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		Impl:   ImplBlock{Summary: "implement the widget"},
	}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 1.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 1.0 (message match)", gs.LandedObserved)
	}
	if !strings.Contains(gs.LandedObserved.Detail, "message-matched") {
		t.Errorf("detail = %q, want it to mention the message match", gs.LandedObserved.Detail)
	}
	_ = landed
}

func TestExtractLanded_UnresolvableSHAMessageMatchReverted(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")
	landed := r.commit("b.txt", "two", "implement the gadget")
	r.commit("d.txt", "x", "Revert gadget\n\nThis reverts commit "+landed+".")

	rec := IterationRecord{
		Commit: "feedface" + strings.Repeat("0", 32),
		Impl:   ImplBlock{Summary: "implement the gadget"},
	}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 0.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 0.0 (matched then reverted)", gs.LandedObserved)
	}
}

func TestExtractLanded_UnresolvableSHANoMatch(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")

	rec := IterationRecord{
		Commit: strings.Repeat("a", 40),
		Impl:   ImplBlock{Summary: "a summary matching nothing on master"},
	}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if gs.LandedObserved.Present {
		t.Errorf("LandedObserved = %+v, want absent (unresolvable, no match)", gs.LandedObserved)
	}
}

func TestExtractLanded_UnresolvableSHANoSummary(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")

	// Unresolvable SHA and no impl summary at all → straight to absent.
	rec := IterationRecord{Commit: strings.Repeat("b", 40)}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if gs.LandedObserved.Present {
		t.Errorf("LandedObserved = %+v, want absent (no SHA match, no summary)", gs.LandedObserved)
	}
}

// --- scope signal ----------------------------------------------------------

const scopeTasksYAML = `schema_version: 1
plan_id: demo
tasks:
    - id: t1-in-scope
      title: scoped task
      write_scope:
        - internal/scoring/
        - docs/NOTES.md
    - id: t2-no-scope
      title: task with empty write_scope
      write_scope: []
`

func TestExtractScope_AllFilesInScope(t *testing.T) {
	r := newGitRepo(t)
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plan")
	sha := r.commit("internal/scoring/widget.go", "package scoring", "scoped change")

	rec := IterationRecord{Commit: sha, TaskID: "t1-in-scope"}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.ScopeObserved.Present || gs.ScopeObserved.SubScore != 1.0 {
		t.Errorf("ScopeObserved = %+v, want present sub-score 1.0", gs.ScopeObserved)
	}
}

func TestExtractScope_PartiallyInScope(t *testing.T) {
	r := newGitRepo(t)
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plan")

	// One file in the directory scope, one matching the exact-file entry, one
	// outside both → 2/3.
	if err := os.MkdirAll(filepath.Join(r.dir, "internal", "scoring"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"internal/scoring/x.go", "docs/NOTES.md", "cmd/main.go"} {
		p := filepath.Join(r.dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "mixed change")
	sha := r.head()

	rec := IterationRecord{Commit: sha, TaskID: "t1-in-scope"}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.ScopeObserved.Present {
		t.Fatalf("ScopeObserved = %+v, want present", gs.ScopeObserved)
	}
	if got := gs.ScopeObserved.SubScore; got < 0.66 || got > 0.67 {
		t.Errorf("ScopeObserved.SubScore = %g, want ~0.667 (2/3)", got)
	}
}

func TestExtractScope_NoWriteScopeForTaskIsAbsent(t *testing.T) {
	r := newGitRepo(t)
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plan")
	sha := r.commit("anything.go", "x", "change")

	// t2-no-scope has an empty write_scope; an unknown task id is also absent.
	for _, id := range []string{"t2-no-scope", "t9-unknown"} {
		rec := IterationRecord{Commit: sha, TaskID: id}
		gs, err := ExtractGitSignals(rec, r.dir)
		if err != nil {
			t.Fatalf("ExtractGitSignals(%s): %v", id, err)
		}
		if gs.ScopeObserved.Present {
			t.Errorf("ScopeObserved for %q = %+v, want absent", id, gs.ScopeObserved)
		}
	}
}

func TestExtractScope_EmptyTaskIDIsAbsent(t *testing.T) {
	r := newGitRepo(t)
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plan")
	sha := r.commit("x.go", "x", "change")

	gs, err := ExtractGitSignals(IterationRecord{Commit: sha, TaskID: ""}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if gs.ScopeObserved.Present {
		t.Errorf("ScopeObserved = %+v, want absent for empty task id", gs.ScopeObserved)
	}
}

func TestExtractScope_EmptyCommitIsAbsent(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")

	gs, err := ExtractGitSignals(IterationRecord{Commit: "", TaskID: "t1-in-scope"}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if gs.ScopeObserved.Present {
		t.Errorf("ScopeObserved = %+v, want absent for empty commit", gs.ScopeObserved)
	}
}

func TestExtractScope_UnresolvableCommitIsAbsent(t *testing.T) {
	r := newGitRepo(t)
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plan")

	rec := IterationRecord{Commit: strings.Repeat("c", 40), TaskID: "t1-in-scope"}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if gs.ScopeObserved.Present {
		t.Errorf("ScopeObserved = %+v, want absent for unresolvable commit", gs.ScopeObserved)
	}
}

func TestExtractScope_EmptyCommitWithinScopeVacuously(t *testing.T) {
	r := newGitRepo(t)
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plan")
	// A commit that changes no tracked files (--allow-empty).
	r.run("commit", "-q", "--allow-empty", "-m", "empty change")
	sha := r.head()

	rec := IterationRecord{Commit: sha, TaskID: "t1-in-scope"}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.ScopeObserved.Present || gs.ScopeObserved.SubScore != 1.0 {
		t.Errorf("ScopeObserved = %+v, want present sub-score 1.0 (vacuous)", gs.ScopeObserved)
	}
}

func TestExtractScope_MalformedTasksFileSkipped(t *testing.T) {
	r := newGitRepo(t)
	// A malformed TASKS.yaml in one plan must not abort the search; the valid
	// plan still resolves.
	r.writeTasks("broken", "tasks: [this is not: valid: yaml")
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plans")
	sha := r.commit("internal/scoring/y.go", "package scoring", "scoped")

	rec := IterationRecord{Commit: sha, TaskID: "t1-in-scope"}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.ScopeObserved.Present || gs.ScopeObserved.SubScore != 1.0 {
		t.Errorf("ScopeObserved = %+v, want present sub-score 1.0 despite malformed sibling", gs.ScopeObserved)
	}
}

// --- helper unit tests -----------------------------------------------------

func TestPathInScope(t *testing.T) {
	scope := []string{"internal/scoring/", "docs/NOTES.md", "  ", "cmd/main.go"}
	tests := []struct {
		file string
		want bool
	}{
		{"internal/scoring/signal.go", true},  // under directory prefix
		{"internal/scoring", true},            // the directory itself
		{"docs/NOTES.md", true},               // exact file
		{"cmd/main.go", true},                 // exact file, OS sep normalized
		{"internal/scoringx/other.go", false}, // prefix is not a path boundary
		{"docs/NOTES.md.bak", false},          // exact match must be exact
		{"README.md", false},                  // outside every entry
	}
	for _, tt := range tests {
		if got := pathInScope(tt.file, scope); got != tt.want {
			t.Errorf("pathInScope(%q) = %v, want %v", tt.file, got, tt.want)
		}
	}
	if pathInScope("anything", nil) {
		t.Error("pathInScope with empty scope = true, want false")
	}
}

func TestShort(t *testing.T) {
	long := strings.Repeat("a", 40)
	if got := short(long); got != strings.Repeat("a", 12) {
		t.Errorf("short(40 chars) = %q, want 12 chars", got)
	}
	if got := short("abc"); got != "abc" {
		t.Errorf("short(%q) = %q, want unchanged", "abc", got)
	}
}

func TestBodyReferencesSHA(t *testing.T) {
	sha := strings.Repeat("a", 40)
	cases := []struct {
		body string
		want bool
	}{
		{"reverts " + sha, true},                      // full SHA
		{"reverts " + sha[:9], true},                  // 9-char abbreviation
		{"reverts " + strings.ToUpper(sha[:8]), true}, // uppercase abbreviation
		{"reverts abc", false},                        // hex but under minAbbrevLen
		{"no commit reference here", false},           // no hex run at all
		{"reverts " + strings.Repeat("b", 9), false},  // long hex, wrong prefix
	}
	for _, c := range cases {
		if got := bodyReferencesSHA(c.body, sha); got != c.want {
			t.Errorf("bodyReferencesSHA(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}

// TestExtractGitSignals_BareGitDir points repoDir at the .git directory itself,
// where `rev-parse --is-inside-work-tree` reports false rather than erroring —
// exercising the not-a-work-tree branch of assertGitRepo.
func TestExtractGitSignals_BareGitDir(t *testing.T) {
	r := newGitRepo(t)
	r.commit("a.txt", "one", "first")

	_, err := ExtractGitSignals(IterationRecord{Commit: "abc"}, filepath.Join(r.dir, ".git"))
	if err == nil {
		t.Fatal("ExtractGitSignals on a .git dir = nil error, want error")
	}
}

// TestExtractLanded_TrunkBranchMissing covers the path where the commit
// resolves but the trunk ref does not exist: every git query against master
// errors, so the commit is reported unreachable.
func TestExtractLanded_TrunkBranchMissing(t *testing.T) {
	r := newGitRepo(t)
	// Commit on a non-master branch; master is never created.
	r.run("checkout", "-q", "-b", "main")
	sha := r.commit("a.txt", "one", "only commit")

	gs, err := ExtractGitSignals(IterationRecord{Commit: sha}, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if !gs.LandedObserved.Present || gs.LandedObserved.SubScore != 0.0 {
		t.Errorf("LandedObserved = %+v, want present sub-score 0.0 (no trunk)", gs.LandedObserved)
	}
}

// TestExtractLanded_MessageMatchTrunkMissing covers matchByMessage's git-error
// path: the SHA is unresolvable and the trunk branch is absent, so the message
// scan itself fails and the signal falls through to absent.
func TestExtractLanded_MessageMatchTrunkMissing(t *testing.T) {
	r := newGitRepo(t)
	r.run("checkout", "-q", "-b", "main")
	r.commit("a.txt", "one", "implement the thing")

	rec := IterationRecord{
		Commit: strings.Repeat("d", 40),
		Impl:   ImplBlock{Summary: "implement the thing"},
	}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	if gs.LandedObserved.Present {
		t.Errorf("LandedObserved = %+v, want absent (trunk missing, no scan)", gs.LandedObserved)
	}
}

// TestExtractScope_ChangedFilesErrorIsAbsent covers changedFiles' error path:
// the recorded commit resolves to a tree, not a commit — git show on it cannot
// list a commit's files. In practice this is reached when the repository state
// is unusual; the signal degrades to absent rather than erroring.
func TestExtractScope_ChangedFilesErrorIsAbsent(t *testing.T) {
	r := newGitRepo(t)
	r.writeTasks("demo", scopeTasksYAML)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", "add plan")

	// resolveWriteScope finds the task; changedFiles then runs against an
	// empty tree object, which carries no commit file list.
	emptyTree := strings.TrimSpace(r.run("hash-object", "-t", "tree", "/dev/null"))
	rec := IterationRecord{Commit: emptyTree, TaskID: "t1-in-scope"}
	gs, err := ExtractGitSignals(rec, r.dir)
	if err != nil {
		t.Fatalf("ExtractGitSignals: %v", err)
	}
	// The tree hash does not resolve as a commit, so the signal is absent.
	if gs.ScopeObserved.Present {
		t.Errorf("ScopeObserved = %+v, want absent", gs.ScopeObserved)
	}
}

func TestRunGit_ExitErrorCarriesStderr(t *testing.T) {
	r := newGitRepo(t)
	// An invalid revision makes git exit non-zero with a diagnostic on stderr.
	_, err := runGit(r.dir, "rev-parse", "--verify", "no-such-ref-xyz")
	if err == nil {
		t.Fatal("runGit on a bad ref = nil error, want error")
	}
}

func TestRunGit_CommandNotFound(t *testing.T) {
	// A non-ExitError failure path: git invoked in a directory that does not
	// exist surfaces a start/exec error rather than a clean non-zero exit.
	_, err := runGit(filepath.Join(t.TempDir(), "does-not-exist"), "status")
	if err == nil {
		t.Fatal("runGit in a missing directory = nil error, want error")
	}
}
