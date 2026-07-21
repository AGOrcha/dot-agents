package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// perf_bench_test.go — Phase 0 measure-first baselines for the workflow
// package's git-subprocess-heavy hot paths (status/orient, checkpoint, the
// git-ref CAS read/write path) plus the pure-CPU iter-log and hook-outcome
// paths. Each git-touching benchmark also reports git-spawns/op via the passive
// gitSpawnCounter (state.go), so a later optimization can PROVE it cut spawns.
//
// Regression guard: `go test ./commands/workflow/ -bench=. -benchmem
// -run='^$'`. These are additive baselines only — no production behavior
// changes here.

const (
	benchGitSpawnMetric = "git-spawns/op"
	benchStateRefPlanID = "bench"
	benchPlansRel       = ".agents/workflow/plans/"
	benchIterLogDirRel  = ".agents/active/iteration-log"
	benchStatusAgentsRC = `{"project":"workflow-proj","version":1,"sources":[{"type":"local"}]}`
)

// benchGitEnv pins a deterministic committer identity so `git commit` succeeds
// regardless of the host's global git config.
var benchGitEnv = []string{
	"GIT_AUTHOR_NAME=Bench",
	"GIT_AUTHOR_EMAIL=bench@example.com",
	"GIT_COMMITTER_NAME=Bench",
	"GIT_COMMITTER_EMAIL=bench@example.com",
}

// benchInitGitRepo bootstraps a committed git repo at repo containing files.
// It mirrors testutil.InitGitRepo but takes *testing.B (that helper is
// *testing.T-only and bottoms out in testutil.InitGitRepo, which a benchmark
// cannot call), so the fixtures match the seed-helper shapes the tests use.
func benchInitGitRepo(b *testing.B, repo string, files map[string]string) {
	b.Helper()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), benchGitEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("config", "user.name", "Bench")
	git("config", "user.email", "bench@example.com")
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, rel := range names {
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[rel]), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-m", "init")
}

// benchChdir enters dir for the benchmark and restores the prior cwd on cleanup.
func benchChdir(b *testing.B, dir string) {
	b.Helper()
	old, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = os.Chdir(old) })
}

// benchSilenceStdout redirects os.Stdout to the null device so a command's
// human output does not pollute benchmark results; restored on cleanup.
func benchSilenceStdout(b *testing.B) {
	b.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = devnull
	b.Cleanup(func() {
		os.Stdout = orig
		_ = devnull.Close()
	})
}

// benchReportSpawns records the average number of git child processes spawned
// per timed iteration, read from the passive counter (b.N holds the b.Loop count).
func benchReportSpawns(b *testing.B) {
	b.ReportMetric(float64(gitSpawnCount())/float64(b.N), benchGitSpawnMetric)
}

// benchTaskIDs returns n synthetic task ids (t1..tN).
func benchTaskIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("t%d", i+1)
	}
	return ids
}

// benchStatusRepo builds a repo shaped like initWorkflowTestRepo: an active
// plan, lessons, and a dirty working tree so git status has content to report.
func benchStatusRepo(b *testing.B) string {
	b.Helper()
	repo := b.TempDir()
	benchInitGitRepo(b, repo, map[string]string{
		testAgentsRCName:                benchStatusAgentsRC,
		".agents/active/sample.plan.md": "# Sample Plan\n\n- [ ] First pending task\n- [ ] Second pending task\n",
		".agents/lessons.md":            "- lesson one\n- lesson two\n",
		"README.md":                     "hello\n",
	})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello world\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	return repo
}

// benchStateRefRepo seeds a git repo with one active plan carrying n pending
// tasks, matching seedStateRefRepo's shape (working-copy plan + tasks).
func benchStateRefRepo(b *testing.B, n int) string {
	b.Helper()
	repo := b.TempDir()
	rel := benchPlansRel + benchStateRefPlanID + "/"
	benchInitGitRepo(b, repo, map[string]string{
		testAgentsRCName:   testAgentsRCLocal,
		rel + "PLAN.yaml":  planYAMLActive(benchStateRefPlanID),
		rel + "TASKS.yaml": tasksYAMLMulti(benchStateRefPlanID, benchTaskIDs(n)...),
	})
	return repo
}

// BenchmarkCollectWorkflowState measures the status/orient state collection —
// the shared read path behind `da workflow status` and `orient`, dominated by
// per-call git subprocess probes (branch/sha/dirty-count + repo detection).
func BenchmarkCollectWorkflowState(b *testing.B) {
	repo := benchStatusRepo(b)
	b.Setenv("AGENTS_HOME", b.TempDir())
	benchChdir(b, repo)
	b.ReportAllocs()
	resetGitSpawnCount()
	for b.Loop() {
		if _, err := collectWorkflowState(); err != nil {
			b.Fatal(err)
		}
	}
	benchReportSpawns(b)
}

// BenchmarkRunWorkflowCheckpoint measures the full checkpoint write. It runs
// collectWorkflowState AND a separate gitModifiedFiles pass, so its
// git-spawns/op includes the duplicate isGitRepo probe (collectWorkflowState's
// git summary already proved the repo, then gitModifiedFiles re-probes it).
func BenchmarkRunWorkflowCheckpoint(b *testing.B) {
	repo := benchStatusRepo(b)
	b.Setenv("AGENTS_HOME", b.TempDir())
	benchChdir(b, repo)
	benchSilenceStdout(b)
	b.ReportAllocs()
	resetGitSpawnCount()
	for b.Loop() {
		if err := runWorkflowCheckpoint("bench checkpoint", "pass", "all green"); err != nil {
			b.Fatal(err)
		}
	}
	benchReportSpawns(b)
}

// BenchmarkWritePlanStateRefCAS measures the git-ref CAS write path at
// 1/10/50 tasks. Each write re-checks every sibling task's presence on the ref
// (seed-if-absent), so git-spawns/op scales O(tasks).
func BenchmarkWritePlanStateRefCAS(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			repo := benchStateRefRepo(b, n)
			overwrite, seed, err := collectPlanTaskStateRefWrite(repo, benchStateRefPlanID, "")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			resetGitSpawnCount()
			for b.Loop() {
				if err := writePlanStateRefCAS(repo, overwrite, seed); err != nil {
					b.Fatal(err)
				}
			}
			benchReportSpawns(b)
		})
	}
}

// BenchmarkReadPlanTaskRecordsFromStateRef measures the git-ref CAS read path
// at 1/10/50 tasks: one ls-tree plus one `git show` per task blob, so
// git-spawns/op scales O(tasks).
func BenchmarkReadPlanTaskRecordsFromStateRef(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			repo := benchStateRefRepo(b, n)
			overwrite, seed, err := collectPlanTaskStateRefWrite(repo, benchStateRefPlanID, "")
			if err != nil {
				b.Fatal(err)
			}
			if err := writePlanStateRefCAS(repo, overwrite, seed); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			resetGitSpawnCount()
			for b.Loop() {
				if _, err := readPlanTaskRecordsFromStateRef(repo, benchStateRefPlanID); err != nil {
					b.Fatal(err)
				}
			}
			benchReportSpawns(b)
		})
	}
}

// benchIterLogV1 is a full schema_version:1 iter-log document (migrated to v2
// on load), matching iter_log_test.go's v1 migration fixture.
var benchIterLogV1 = []byte(`schema_version: 1
iteration: 5
date: "2026-04-30"
wave: w
task_id: t1
commit: abc
files_changed: 3
lines_added: 10
lines_removed: 2
first_commit: true
item: thing
summary: did it
scope_note: ok
feedback_goal: cover
retries: 0
tests_added: 1
tests_total_pass: 1
self_assessment:
  read_loop_state: true
  one_item_only: true
  committed_after_tests: true
  aligned_with_canonical_tasks: true
  persisted_via_workflow_commands: true
  stayed_under_10_files: true
  no_destructive_commands: true
`)

// benchIterLogV2 is a schema_version:2 iter-log document (native decode path).
var benchIterLogV2 = []byte(`schema_version: 2
iteration: 7
date: "2026-04-30"
wave: w
task_id: t1
commit: abc
impl:
  feedback_goal: cover
`)

// BenchmarkLoadIterLogDocument measures the YAML probe+decode of an iter-log
// document for both schema versions (v1 takes the legacy-migrate branch, v2
// the native decode branch). Pure CPU/alloc — no git spawns.
func BenchmarkLoadIterLogDocument(b *testing.B) {
	cases := []struct {
		name string
		data []byte
	}{
		{"v1", benchIterLogV1},
		{"v2", benchIterLogV2},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := loadIterLogDocument(tc.data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAppendHookOutcome measures a single hook-outcome sidecar append into
// a fresh sidecar: resolve active N, load (empty) sidecar, idempotency scan,
// schema-validate, atomic write. The per-iteration sidecar reset is excluded
// from timing so each measured op is the same clean write. Pure filesystem — no
// git spawns.
func BenchmarkAppendHookOutcome(b *testing.B) {
	dir := b.TempDir()
	iterDir := filepath.Join(dir, filepath.FromSlash(benchIterLogDirRel))
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(iterDir, "iter-1.yaml"), []byte("schema_version: 2\niteration: 1\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	rec := newValidHookOutcomeRecord()
	sidecar := hookOutcomeSidecarPath(dir, 1)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, rec); err != nil {
			b.Fatal(err)
		}
	}
}
