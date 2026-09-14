package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/crgbehavior"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// run executes the command and returns its exit code plus both streams.
func run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := mainRun(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// releasePath is the checked-in release capability fixture.
func releasePath() string {
	return filepath.Join("..", "..", crgbehavior.DefaultReleasePath)
}

// contractPath is the checked-in corpus contract.
func contractPath() string {
	return filepath.Join("..", "..", crgbehavior.DefaultContractPath)
}

// manifestPath is the checked-in pinned corpus.
func manifestPath() string {
	return filepath.Join("..", "..", crgbehavior.DefaultManifestPath)
}

func TestUsageErrors(t *testing.T) {
	if code, _, _ := run("-nope"); code != exitError {
		t.Fatalf("unknown flag exit = %d, want %d", code, exitError)
	}
	code, _, stderr := run("-regen", "-record")
	if code != exitError || !strings.Contains(stderr, "one at a time") {
		t.Fatalf("regen+record exit = %d, stderr %q", code, stderr)
	}
}

// Each pinned input is load-bearing; a missing or invalid one is a plumbing
// error, never a quietly reduced run.
func TestMissingPinnedInputsAreErrors(t *testing.T) {
	cases := map[string][]string{
		"release":  {"-release", filepath.Join(t.TempDir(), "absent.json")},
		"manifest": {"-release", releasePath(), "-manifest", filepath.Join(t.TempDir(), "absent.json")},
		"contract": {"-release", releasePath(), "-manifest", manifestPath(),
			"-contract", filepath.Join(t.TempDir(), "absent.json")},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if code, _, _ := run(args...); code != exitError {
				t.Fatalf("missing %s exit = %d, want %d", name, code, exitError)
			}
		})
	}
}

// An unavailable bridge means the run produced NO evidence. It used to exit 0
// with a SKIP notice, which made "we could not test this" and "behavior is
// preserved" the same green result.
func TestUnavailableBridgeIsInconclusiveAndNonZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := reportRunError(fmt.Errorf("probe: %w", crgbehavior.ErrBridgeUnavailable), &stdout, &stderr)
	if code != exitInconclusive {
		t.Fatalf("exit = %d, want %d (INCONCLUSIVE)", code, exitInconclusive)
	}
	if code == exitPass {
		t.Fatal("an unavailable bridge exited 0")
	}
	out := stdout.String()
	if !strings.Contains(out, string(crgbehavior.VerdictInconclusive)) ||
		!strings.Contains(out, "NOT ESTABLISHED") ||
		!strings.Contains(out, crgbehavior.PinnedVersion) {
		t.Fatalf("inconclusive notice does not state the verdict and the pinned release:\n%s", out)
	}
}

// Any other run failure is a plumbing error on stderr, not an inconclusive
// verdict that a reader might mistake for an environment quirk.
func TestOtherRunFailuresAreErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := reportRunError(errors.New("database is locked"), &stdout, &stderr); code != exitError {
		t.Fatalf("exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "database is locked") {
		t.Fatalf("the failure was not reported on stderr: %q", stderr.String())
	}
}

// Regeneration derives the corpus from the pinned release's own indexed
// languages and records that provenance, so a later run can prove which
// release decided what the corpus contains.
func TestRegenPinsACorpusFromRealHistory(t *testing.T) {
	repo := newGitRepo(t)
	out := filepath.Join(t.TempDir(), "manifest.json")
	code, stdout, stderr := run("-regen", "-repo", repo, "-ref", "HEAD", "-commits", "5",
		"-window", "10", "-manifest", out, "-release", releasePath())
	if code != exitPass {
		t.Fatalf("regen exit = %d: %s %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "declaration extractors:") {
		t.Fatalf("regen did not report parser coverage:\n%s", stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m crgbehavior.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if m.SchemaVersion != crgbehavior.ManifestSchemaVersion || m.Release != crgbehavior.PinnedVersion {
		t.Fatalf("manifest = schema %d release %q", m.SchemaVersion, m.Release)
	}
	if m.Window != 10 {
		t.Fatalf("manifest window = %d, want the scanned window recorded", m.Window)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("a regenerated manifest failed validation: %v", err)
	}
	languages := map[string]bool{}
	for _, task := range m.Tasks {
		for _, lang := range task.Languages {
			languages[lang] = true
		}
	}
	if !languages["go"] || !languages["typescript"] {
		t.Fatalf("corpus languages = %v, want coverage-first selection to pin both commits", languages)
	}
}

// A ref the repository does not have, and an unwritable destination, are both
// plumbing errors.
func TestRegenReportsUnusableInputs(t *testing.T) {
	repo := newGitRepo(t)
	if code, _, _ := run("-regen", "-repo", repo, "-ref", "no/such/ref",
		"-manifest", filepath.Join(t.TempDir(), "m.json"), "-release", releasePath()); code != exitError {
		t.Fatalf("unknown ref exit = %d, want %d", code, exitError)
	}
	unwritable := filepath.Join(t.TempDir(), "file", "m.json")
	if err := os.WriteFile(filepath.Dir(unwritable), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("stage unwritable path: %v", err)
	}
	if code, _, _ := run("-regen", "-repo", repo, "-ref", "HEAD",
		"-manifest", unwritable, "-release", releasePath()); code != exitError {
		t.Fatalf("unwritable manifest exit = %d, want %d", code, exitError)
	}
}

// newGitRepo stages a two-commit repository spanning two release languages.
// Staging is native (go-git in-process): the gate executes no git subprocess,
// and neither does its test bed, so the fixture cannot depend on an ambient
// git binary or on the developer's global git configuration.
func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	// Distinct, increasing committer times: the corpus window is ordered by
	// committer time, so equal timestamps would make "newest first" ambiguous.
	base := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	writeCommit(t, repo, root, "pkg/a.go", "package pkg\n\nfunc Handle() {}\n", "feat: add Handle", base)
	writeCommit(t, repo, root, "web/app.ts", "export function render() {}\n", "feat: add render", base.Add(time.Minute))
	return root
}

// writeCommit writes one file and commits it.
func writeCommit(t *testing.T, repo *git.Repository, root, name, body, subject string, when time.Time) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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

// mtTexit runs the command, asserts the exit code — the gate's whole contract
// with CI — and returns both streams for the caller's own oracles.
func mtTexit(t *testing.T, want int, args ...string) (string, string) {
	t.Helper()
	code, stdout, stderr := run(args...)
	if code != want {
		t.Fatalf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, want, stdout, stderr)
	}
	return stdout, stderr
}

// mtTgitFixture stages the native two-commit repository and returns its root
// plus the full SHA of HEAD, so a corpus can pin a commit the fixture has.
func mtTgitFixture(t *testing.T) (string, string) {
	t.Helper()
	root := newGitRepo(t)
	repo, err := git.PlainOpen(root)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return root, head.Hash().String()
}

// mtTmanifest writes a valid one-task corpus pinned at commit.
func mtTmanifest(t *testing.T, commit string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := crgbehavior.Manifest{
		SchemaVersion: crgbehavior.ManifestSchemaVersion,
		GeneratedFrom: "HEAD",
		Head:          commit,
		Window:        1,
		Release:       crgbehavior.PinnedVersion,
		Tasks: []crgbehavior.Task{{
			Commit:       commit,
			Subject:      "feat: add render",
			ChangedFiles: []string{"web/app.ts"},
			Identifiers:  []string{"render"},
			Languages:    []string{"typescript"},
		}},
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	return path
}

// mtTnoBridge guarantees no code-review-graph is discoverable, so "the pinned
// bridge is absent" is a property of the test rather than of the machine.
func mtTnoBridge(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// mtTargs is the pinned-input argument prefix every gate and record run needs,
// with the worktree parent redirected into this test's own temp directory.
func mtTargs(t *testing.T, repo, manifest string, extra ...string) []string {
	t.Helper()
	return append([]string{
		"-release", releasePath(), "-manifest", manifest, "-contract", contractPath(),
		"-repo", repo, "-work-dir", t.TempDir(),
	}, extra...)
}

// mtTabsentCommit is a well-formed SHA the fixture repository does not have.
const mtTabsentCommit = "abababababababababababababababababababab"

// A flag value the flag set cannot parse is a usage error, not a silently
// defaulted knob: a mistyped -tasks must never quietly run the whole corpus.
func TestMtTMalformedFlagValueIsAUsageError(t *testing.T) {
	_, stderr := mtTexit(t, exitError, "-tasks", "seven")
	if !strings.Contains(stderr, "tasks") {
		t.Fatalf("stderr does not name the rejected flag: %q", stderr)
	}
}

// Each pinned-input path is taken from ITS OWN flag. Asserting the reported
// failure names the path that was passed is what proves the flag value reached
// the loader instead of the compiled-in default.
func TestMtTPinnedInputPathsComeFromTheirFlags(t *testing.T) {
	cases := map[string]func(path string) []string{
		"release": func(path string) []string {
			return []string{"-release", path}
		},
		"manifest": func(path string) []string {
			return []string{"-release", releasePath(), "-manifest", path}
		},
		"contract": func(path string) []string {
			return []string{"-release", releasePath(), "-manifest", manifestPath(), "-contract", path}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "absent.json")
			_, stderr := mtTexit(t, exitError, build(path)...)
			if !strings.Contains(stderr, path) {
				t.Fatalf("-%s failure does not name %s:\n%s", name, path, stderr)
			}
		})
	}
}

// A -repo that is not a repository cannot be materialized at any SHA. Both
// commands must report that as plumbing (ERROR), never as a behavior verdict.
func TestMtTNonRepositoryTargetIsAPlumbingError(t *testing.T) {
	for _, extra := range [][]string{nil, {"-record"}} {
		t.Run(mtTcommandName(extra), func(t *testing.T) { mtTassertNonRepository(t, extra) })
	}
}

func mtTassertNonRepository(t *testing.T, extra []string) {
	t.Helper()
	notARepo := t.TempDir()
	args := mtTargs(t, notARepo, mtTmanifest(t, mtTabsentCommit), extra...)
	_, stderr := mtTexit(t, exitError, args...)
	if !strings.Contains(stderr, notARepo) {
		t.Fatalf("failure does not name the unusable repository %s:\n%s", notARepo, stderr)
	}
}

// An absent pinned bridge means the run produced NO evidence, so both commands
// exit INCONCLUSIVE and say the criterion is not established. Reporting this as
// PASS is exactly how a missing legacy side once read as preserved behavior.
func TestMtTAbsentBridgeIsInconclusiveForBothCommands(t *testing.T) {
	for _, extra := range [][]string{nil, {"-record"}} {
		t.Run(mtTcommandName(extra), func(t *testing.T) { mtTassertInconclusive(t, extra) })
	}
}

func mtTassertInconclusive(t *testing.T, extra []string) {
	t.Helper()
	mtTnoBridge(t)
	repo, head := mtTgitFixture(t)
	args := mtTargs(t, repo, mtTmanifest(t, head), extra...)
	stdout, _ := mtTexit(t, exitInconclusive, args...)
	if !strings.Contains(stdout, string(crgbehavior.VerdictInconclusive)) ||
		!strings.Contains(stdout, "NOT ESTABLISHED") ||
		!strings.Contains(stdout, crgbehavior.PinnedVersion) {
		t.Fatalf("the notice does not state the verdict and the pinned release:\n%s", stdout)
	}
}

// mtTcommandName labels the gate and record variants of a shared assertion.
func mtTcommandName(extra []string) string {
	if len(extra) == 0 {
		return "gate"
	}
	return "record"
}

// A pinned task the repository cannot materialize is a FAIL, not an error and
// not an inconclusive: the corpus is pinned, so a task that cannot be replayed
// is a real gate failure the rendered report has to name.
func TestMtTUnmaterializableTaskFailsTheGate(t *testing.T) {
	mtTnoBridge(t)
	repo, _ := mtTgitFixture(t)
	args := mtTargs(t, repo, mtTmanifest(t, mtTabsentCommit))
	stdout, _ := mtTexit(t, exitFail, args...)
	if !strings.Contains(stdout, string(crgbehavior.VerdictFail)) {
		t.Fatalf("the rendered report does not state the FAIL verdict:\n%s", stdout)
	}
}

// -json writes the machine-readable artifact and announces where it went, so a
// CI job can archive the evidence a verdict was derived from.
func TestMtTJSONArtifactIsWrittenAndAnnounced(t *testing.T) {
	mtTnoBridge(t)
	repo, _ := mtTgitFixture(t)
	artifact := filepath.Join(t.TempDir(), "nested", "run.json")
	args := mtTargs(t, repo, mtTmanifest(t, mtTabsentCommit), "-json", artifact)
	stdout, _ := mtTexit(t, exitFail, args...)
	if !strings.Contains(stdout, "artifact: "+artifact) {
		t.Fatalf("the run did not announce the artifact path:\n%s", stdout)
	}
	data, err := os.ReadFile(artifact) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var report crgbehavior.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("artifact is not valid JSON: %v", err)
	}
	if report.PinnedVersion != crgbehavior.PinnedVersion || report.RepoRoot != repo {
		t.Fatalf("artifact = release %q repo %q, want the run's own pinned release and repository",
			report.PinnedVersion, report.RepoRoot)
	}
	if report.Verdict() != crgbehavior.VerdictFail {
		t.Fatalf("artifact verdict = %q, want it to agree with the exit code", report.Verdict())
	}
}

// An artifact that cannot be written is a plumbing ERROR, not the verdict the
// run had otherwise reached: unarchived evidence must not exit as a mere FAIL.
func TestMtTUnwritableJSONArtifactIsAnError(t *testing.T) {
	mtTnoBridge(t)
	repo, _ := mtTgitFixture(t)
	blocked := filepath.Join(t.TempDir(), "file", "run.json")
	if err := os.WriteFile(filepath.Dir(blocked), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("stage unwritable path: %v", err)
	}
	args := mtTargs(t, repo, mtTmanifest(t, mtTabsentCommit), "-json", blocked)
	_, stderr := mtTexit(t, exitError, args...)
	if !strings.Contains(stderr, filepath.Dir(blocked)) {
		t.Fatalf("the write failure does not name the blocked artifact location:\n%s", stderr)
	}
}

// Recording the upstream baseline loads the very same pinned inputs the gate
// does, so an unloadable corpus or contract stops it before any fixture is
// written: a half-recorded baseline would silently become the conformance oracle.
func TestMtTRecordRefusesUnloadablePinnedInputs(t *testing.T) {
	cases := map[string]func(absent string) []string{
		"manifest": func(absent string) []string {
			return []string{"-manifest", absent, "-contract", contractPath()}
		},
		"contract": func(absent string) []string {
			return []string{"-manifest", manifestPath(), "-contract", absent}
		},
	}
	fixtures := filepath.Join(t.TempDir(), "fixtures")
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			absent := filepath.Join(t.TempDir(), "absent.json")
			args := append([]string{"-record", "-release", releasePath(), "-fixtures", fixtures},
				build(absent)...)
			mtTexit(t, exitError, args...)
		})
	}
	if _, err := os.Stat(fixtures); !os.IsNotExist(err) {
		t.Fatalf("record wrote into %s despite unloadable inputs: %v", fixtures, err)
	}
}

// The worktrees are materialized under the temp root, never inside the
// repository, so a checked-out pinned commit can never be mistaken for working
// state or picked up by a build.
func TestMtTDefaultWorkDirIsOutsideTheRepository(t *testing.T) {
	dir := defaultWorkDir()
	if parent := filepath.Dir(dir); parent != os.TempDir() {
		t.Fatalf("work dir parent = %q, want the temp root %q", parent, os.TempDir())
	}
	base := filepath.Base(dir)
	if !strings.Contains(base, "crg") || !strings.Contains(base, "worktree") {
		t.Fatalf("work dir %q does not name the gate's worktrees", base)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if strings.HasPrefix(dir, cwd+string(filepath.Separator)) {
		t.Fatalf("work dir %q is inside the repository tree %q", dir, cwd)
	}
}

// The materializer is bound to the run's own knobs and echoes the pinned
// release's build progress to stdout: one full release build per commit takes
// minutes, and that echo is the only window a CI log has into a live run.
func TestMtTMaterializerBindsTheRunAndEchoesProgress(t *testing.T) {
	rel, err := crgbehavior.LoadRelease(releasePath())
	if err != nil {
		t.Fatalf("load release: %v", err)
	}
	repo, _ := mtTgitFixture(t)
	o := options{repo: repo, workDir: t.TempDir(), depth: 4, maxResults: 11}
	var stdout bytes.Buffer
	mat, err := materializer(o, rel, &stdout)
	if err != nil {
		t.Fatalf("materializer: %v", err)
	}
	wt, ok := mat.(*crgbehavior.WorktreeMaterializer)
	if !ok {
		t.Fatalf("materializer returned %T, want the worktree materializer", mat)
	}
	if wt.RepoRoot != o.repo || wt.WorkDir != o.workDir ||
		wt.Depth != o.depth || wt.MaxResults != o.maxResults ||
		wt.Release.Version != rel.Version {
		t.Fatalf("materializer = repo %q work-dir %q depth %d max-results %d release %q, want the run's knobs",
			wt.RepoRoot, wt.WorkDir, wt.Depth, wt.MaxResults, wt.Release.Version)
	}
	wt.Log("building graph at abc1234")
	if got := stdout.String(); got != "... building graph at abc1234\n" {
		t.Fatalf("progress echo = %q, want the marked progress line on stdout", got)
	}
}
