package codegraph

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// This suite drives the kg-native lifecycle over the generated v2.3.8
// release-contract fixtures. testdata/crg-release/v2.3.8/lifecycle.json is
// the result dict upstream produced for each case, captured in a known
// order against a reproducible two-commit repository; the test rebuilds
// that repository, replays the same order, and compares the report's FIELD
// SET and values after substituting the generator's ${...} tokens.
//
// The field set matters as much as the values: upstream's presence rules are
// how a caller tells a full build from an incremental one, an empty diff from
// a real update, and a post-build pass from a standalone one.

// releaseFixtureDir is the generated contract, relative to this package.
const releaseFixtureDir = "../../testdata/crg-release/v2.3.8"

// fixtureCommitEnv is the generator's fixed commit identity. It is what makes
// the fixture repository's two commit SHAs reproducible, so the ${HEAD_SHA}
// and ${BASE_SHA} tokens name real commits in the rebuilt repository.
var fixtureCommitEnv = []string{
	"GIT_AUTHOR_NAME=crg fixture",
	"GIT_AUTHOR_EMAIL=fixture@example.com",
	"GIT_COMMITTER_NAME=crg fixture",
	"GIT_COMMITTER_EMAIL=fixture@example.com",
	"GIT_AUTHOR_DATE=2024-01-01T00:00:00+00:00",
	"GIT_COMMITTER_DATE=2024-01-01T00:00:00+00:00",
}

// secondCommitFiles are the paths the generator holds back for commit two.
var secondCommitFiles = []string{"pkg/auth/token.go"}

// releaseMeta is the slice of release.json the lifecycle cases depend on.
type releaseMeta struct {
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha"`
	Version string `json:"version"`
}

// lifecycleFixture is one materialized fixture repository plus the recorded
// upstream results to compare against.
type lifecycleFixture struct {
	t       *testing.T
	root    string
	release releaseMeta
	cases   map[string]map[string]any
	engine  *Engine
}

// newLifecycleFixture rebuilds the generator's repository in a temp
// directory and opens a kg-native engine on it.
func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	fix := &lifecycleFixture{
		t:       t,
		root:    materializeFixtureRepo(t),
		release: readFixtureJSON[releaseMeta](t, "release.json"),
		cases:   readFixtureJSON[map[string]map[string]any](t, "lifecycle.json"),
	}
	fix.assertReproducibleHistory()
	fix.engine = Open(fix.root)
	t.Cleanup(func() { _ = fix.engine.Close() })
	return fix
}

// assertReproducibleHistory fails loudly when the rebuilt repository's
// commits do not match the ones the fixtures were generated against. Every
// ${BASE_SHA}/${HEAD_SHA} substitution below is meaningless otherwise, so
// this is checked once rather than diagnosed per case.
func (f *lifecycleFixture) assertReproducibleHistory() {
	f.t.Helper()
	if got := f.gitOut("rev-parse", "HEAD"); got != f.release.HeadSHA {
		f.t.Fatalf("head sha = %s, want %s (fixture repository is not reproducible)", got, f.release.HeadSHA)
	}
	if got := f.gitOut("rev-parse", "HEAD~1"); got != f.release.BaseSHA {
		f.t.Fatalf("base sha = %s, want %s (fixture repository is not reproducible)", got, f.release.BaseSHA)
	}
}

// gitOut runs git in the fixture repository and returns its trimmed stdout.
func (f *lifecycleFixture) gitOut(args ...string) string {
	f.t.Helper()
	return runFixtureGit(f.t, f.root, args...)
}

// runFixtureGit runs one git command with the generator's commit identity.
func runFixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), fixtureCommitEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// materializeFixtureRepo copies the fixture sources into a temp directory and
// replays the generator's two-commit history, holding the second commit's
// files outside the work tree so the first commit carries no trace of them.
func materializeFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyFixtureTree(t, filepath.Join(releaseFixtureDir, "repo"), root)

	runFixtureGit(t, root, "init", "-q", "-b", "main")
	runFixtureGit(t, root, "config", "commit.gpgsign", "false")
	hold := t.TempDir()
	for i, rel := range secondCommitFiles {
		moveFile(t, filepath.Join(root, rel), filepath.Join(hold, fmt.Sprintf("%d.hold", i)))
	}
	runFixtureGit(t, root, "add", "-A")
	runFixtureGit(t, root, "commit", "-q", "-m", "fixture: initial")
	for i, rel := range secondCommitFiles {
		moveFile(t, filepath.Join(hold, fmt.Sprintf("%d.hold", i)), filepath.Join(root, rel))
	}
	runFixtureGit(t, root, "add", "-A")
	runFixtureGit(t, root, "commit", "-q", "-m", "fixture: mint tokens")
	return root
}

// copyFixtureTree copies a directory tree, preserving relative layout.
func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixture dir %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	for _, entry := range entries {
		from, to := filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyFixtureTree(t, from, to)
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatalf("read %s: %v", from, err)
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", to, err)
		}
	}
}

// moveFile relocates one file, creating the destination directory.
func moveFile(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(to), err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("move %s -> %s: %v", from, to, err)
	}
}

// readFixtureJSON decodes one generated fixture file.
func readFixtureJSON[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(releaseFixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return out
}

// ── Report comparison ────────────────────────────────────────────────────────

// reportJSON round-trips a report through its own JSON encoding, which is the
// form every caller and the MCP envelope sees — and the only form in which
// upstream's "key is absent" versus "key is null" distinction is observable.
func reportJSON(t *testing.T, report *graphstore.CRGOperationReport) map[string]any {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	return out
}

// expect returns the recorded upstream result for a case, with the
// generator's volatile-value tokens resolved against this repository.
func (f *lifecycleFixture) expect(name string) map[string]any {
	f.t.Helper()
	recorded, ok := f.cases[name]
	if !ok {
		f.t.Fatalf("lifecycle.json has no case %q", name)
	}
	resolved := make(map[string]any, len(recorded))
	for key, value := range recorded {
		resolved[key] = f.resolveToken(value)
	}
	return resolved
}

// resolveToken substitutes the SHA tokens; the time-like tokens are left in
// place because they are matched by predicate, not by value.
func (f *lifecycleFixture) resolveToken(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	switch text {
	case "${HEAD_SHA}":
		return f.release.HeadSHA
	case "${BASE_SHA}":
		return f.release.BaseSHA
	case "${REPO_ROOT}":
		return f.root
	}
	return strings.ReplaceAll(text, "${REPO_ROOT}", f.root)
}

// assertReport compares one report against a recorded upstream result: the
// key sets must be identical, and each value must match or satisfy its token.
func (f *lifecycleFixture) assertReport(name string, report *graphstore.CRGOperationReport) {
	f.t.Helper()
	got := reportJSON(f.t, report)
	want := f.expect(name)

	gotKeys := slices.Sorted(maps.Keys(got))
	wantKeys := slices.Sorted(maps.Keys(want))
	if !slices.Equal(gotKeys, wantKeys) {
		f.t.Errorf("%s: field set mismatch\n got: %v\nwant: %v\nmissing: %v\nextra: %v",
			name, gotKeys, wantKeys, missing(wantKeys, gotKeys), missing(gotKeys, wantKeys))
		return
	}
	for _, key := range wantKeys {
		assertValue(f.t, name+"."+key, got[key], want[key])
	}
}

// missing returns the elements of want that have is not in got.
func missing(want, got []string) []string {
	var out []string
	for _, key := range want {
		if !slices.Contains(got, key) {
			out = append(out, key)
		}
	}
	return out
}

// assertValue compares one field, honouring the generator's tokens.
func assertValue(t *testing.T, path string, got, want any) {
	t.Helper()
	if token, ok := want.(string); ok && strings.HasPrefix(token, "${") {
		assertToken(t, path, got, token)
		return
	}
	if wantMap, ok := want.(map[string]any); ok {
		gotMap, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s: got %T (%v), want an object", path, got, got)
			return
		}
		gotKeys, wantKeys := slices.Sorted(maps.Keys(gotMap)), slices.Sorted(maps.Keys(wantMap))
		if !slices.Equal(gotKeys, wantKeys) {
			t.Errorf("%s: key set mismatch\n got: %v\nwant: %v", path, gotKeys, wantKeys)
			return
		}
		for _, key := range wantKeys {
			assertValue(t, path+"."+key, gotMap[key], wantMap[key])
		}
		return
	}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("%s: got %#v, want %#v", path, got, want)
	}
}

// assertToken checks a value the generator normalized away. A duration must
// still be a non-negative number and a timestamp a non-empty string, so the
// normalization does not turn the field into a free pass.
func assertToken(t *testing.T, path string, got any, token string) {
	t.Helper()
	switch token {
	case "${DURATION}", "${AGE_SECONDS}":
		seconds, ok := got.(float64)
		if !ok {
			t.Errorf("%s: got %#v, want a %s number", path, got, token)
			return
		}
		if seconds < 0 {
			t.Errorf("%s: got %v, want a non-negative duration", path, seconds)
		}
	case "${TIMESTAMP}":
		if text, ok := got.(string); !ok || strings.TrimSpace(text) == "" {
			t.Errorf("%s: got %#v, want a timestamp", path, got)
		}
	default:
		t.Errorf("%s: unhandled fixture token %s", path, token)
	}
}

// ── The lifecycle replay ─────────────────────────────────────────────────────

// TestLifecycleMatchesReleaseContract replays the generator's exact sequence
// of build / update / post-process calls and compares every report against
// the result upstream produced for it.
//
// The order is part of the contract: several cases only mean what the fixture
// records because of the state the previous call left behind — an incremental
// update with no changes is only "no changes" because the build before it
// wrote the commit the base resolves to.
func TestLifecycleMatchesReleaseContract(t *testing.T) {
	fix := newLifecycleFixture(t)
	e := fix.engine

	full := graphstore.BuildOptions{Postprocess: graphstore.PostprocessFull}

	for _, step := range []struct {
		name string
		run  func() (*graphstore.CRGOperationReport, error)
	}{
		{"full_build", func() (*graphstore.CRGOperationReport, error) {
			return e.BuildReport(full)
		}},
		{"postprocess_none_build", func() (*graphstore.CRGOperationReport, error) {
			return e.BuildReport(graphstore.BuildOptions{Postprocess: graphstore.PostprocessNone})
		}},
		{"postprocess_minimal_build", func() (*graphstore.CRGOperationReport, error) {
			return e.BuildReport(graphstore.BuildOptions{Postprocess: graphstore.PostprocessMinimal})
		}},
		{"full_build_again", func() (*graphstore.CRGOperationReport, error) {
			return e.BuildReport(full)
		}},
	} {
		report, err := step.run()
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		fix.assertReport(step.name, report)
	}

	// A standalone post-process recomputes flows, communities and FTS but
	// must NOT recompute the three summary tables. Emptying them first is
	// what makes that observable: they stay empty afterwards even though
	// flows and communities were recomputed.
	fix.truncateSummaryTables()
	before := fix.summaryCounts()

	standalone, err := e.PostprocessReport(graphstore.PostprocessOptions{})
	if err != nil {
		t.Fatalf("standalone_postprocess: %v", err)
	}
	fix.assertReport("standalone_postprocess", standalone)
	if after := fix.summaryCounts(); !summaryTablesEqual(before, after) {
		t.Errorf("standalone post-process refreshed the summary tables: before %v, after %v", before, after)
	}

	flowsOnly, err := e.PostprocessReport(graphstore.PostprocessOptions{
		Communities: new(false),
		FTS:         new(false),
	})
	if err != nil {
		t.Fatalf("standalone_postprocess_flows_only: %v", err)
	}
	fix.assertReport("standalone_postprocess_flows_only", flowsOnly)

	noFlows, err := e.PostprocessReport(graphstore.PostprocessOptions{
		Flows:       new(false),
		Communities: new(false),
		FTS:         new(false),
	})
	if err != nil {
		t.Fatalf("standalone_postprocess_no_flows: %v", err)
	}
	fix.assertReport("standalone_postprocess_no_flows", noFlows)

	restored, err := e.BuildReport(full)
	if err != nil {
		t.Fatalf("full_build_restoring_summaries: %v", err)
	}
	fix.assertReport("full_build_restoring_summaries", restored)
	if counts := fix.summaryCounts(); counts["community_summaries"] == 0 || counts["flow_snapshots"] == 0 || counts["risk_index"] == 0 {
		t.Errorf("a full build must recompute the summary tables, got %v", counts)
	}

	// An automatic base resolves to the commit the graph was last built at.
	noChanges, err := e.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("incremental_auto_base_no_changes: %v", err)
	}
	fix.assertReport("incremental_auto_base_no_changes", noChanges)

	explicit, err := e.UpdateReport(graphstore.UpdateOptions{Base: fix.release.BaseSHA})
	if err != nil {
		t.Fatalf("incremental_explicit_base: %v", err)
	}
	fix.assertReport("incremental_explicit_base", explicit)

	// An EXPLICIT base that resolves to nothing stays incremental and
	// reports an empty diff. Only an ABSENT base falls back to a rebuild.
	unresolvable, err := e.UpdateReport(graphstore.UpdateOptions{Base: strings.Repeat("0", 40)})
	if err != nil {
		t.Fatalf("incremental_unresolvable_base: %v", err)
	}
	fix.assertReport("incremental_unresolvable_base", unresolvable)

	fix.withEditedAuthFile(func() {
		changed, err := e.UpdateReport(graphstore.UpdateOptions{})
		if err != nil {
			t.Fatalf("incremental_auto_base_with_changes: %v", err)
		}
		fix.assertReport("incremental_auto_base_with_changes", changed)
	})

	final, err := e.BuildReport(full)
	if err != nil {
		t.Fatalf("final_full_build: %v", err)
	}
	fix.assertReport("final_full_build", final)
}

// withEditedAuthFile appends the generator's marker comment to the file the
// `incremental_auto_base_with_changes` case edits, runs fn, then restores the
// original content AND mtime — several tools compare source mtimes against
// the graph's build time, so a just-now mtime would race the clock.
func (f *lifecycleFixture) withEditedAuthFile(fn func()) {
	f.t.Helper()
	path := filepath.Join(f.root, "pkg", "auth", "auth.go")
	original, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatalf("read %s: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		f.t.Fatalf("stat %s: %v", path, err)
	}
	edited := append(append([]byte{}, original...), "\n// touched by the release-contract fixture generator\n"...)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		f.t.Fatalf("write %s: %v", path, err)
	}
	defer func() {
		if err := os.WriteFile(path, original, 0o644); err != nil {
			f.t.Fatalf("restore %s: %v", path, err)
		}
		_ = os.Chtimes(path, info.ModTime(), info.ModTime())
	}()
	fn()
}

// ── Summary-table observation ────────────────────────────────────────────────

// summaryTableNames are the three tables a build refreshes and a standalone
// post-process deliberately leaves alone.
var summaryTableNames = []string{"community_summaries", "flow_snapshots", "risk_index"}

// summaryCounts reads the row counts of the three summary tables.
func (f *lifecycleFixture) summaryCounts() map[string]int {
	f.t.Helper()
	store, err := f.engine.readStore()
	if err != nil || store == nil {
		f.t.Fatalf("read store: %v", err)
	}
	summaries, err := store.ReadCommunitySummaries()
	if err != nil {
		f.t.Fatalf("read community_summaries: %v", err)
	}
	snapshots, err := store.ReadFlowSnapshots()
	if err != nil {
		f.t.Fatalf("read flow_snapshots: %v", err)
	}
	risk, err := store.ReadRiskIndex()
	if err != nil {
		f.t.Fatalf("read risk_index: %v", err)
	}
	return map[string]int{
		"community_summaries": len(summaries),
		"flow_snapshots":      len(snapshots),
		"risk_index":          len(risk),
	}
}

// truncateSummaryTables empties the three tables a standalone post-process
// must not repopulate, the same way the generator does before its capture.
func (f *lifecycleFixture) truncateSummaryTables() {
	f.t.Helper()
	store, err := f.engine.readStore()
	if err != nil || store == nil {
		f.t.Fatalf("read store: %v", err)
	}
	if _, err := store.ReplaceCommunitySummaries(nil); err != nil {
		f.t.Fatalf("truncate community_summaries: %v", err)
	}
	if _, err := store.ReplaceFlowSnapshots(nil); err != nil {
		f.t.Fatalf("truncate flow_snapshots: %v", err)
	}
	if _, err := store.ReplaceRiskIndex(nil); err != nil {
		f.t.Fatalf("truncate risk_index: %v", err)
	}
}

// summaryTablesEqual compares two summary-table snapshots.
func summaryTablesEqual(a, b map[string]int) bool {
	for _, name := range summaryTableNames {
		if a[name] != b[name] {
			return false
		}
	}
	return true
}

// ── Post-process timing ──────────────────────────────────────────────────────

// TestPostprocessTimingKeysMatchLevel pins the timing block's shape per
// level. The keys are how a caller tells which stages actually ran, so a
// pass that silently skipped flows would be caught here even if every
// counter it reported happened to look plausible.
func TestPostprocessTimingKeysMatchLevel(t *testing.T) {
	for _, tc := range []struct {
		level string
		want  []string
	}{
		{graphstore.PostprocessFull, []string{"communities_s", "flows_s", "fts_s", "signatures_s", "summaries_s"}},
		{graphstore.PostprocessMinimal, []string{"fts_s", "signatures_s"}},
		{graphstore.PostprocessNone, nil},
	} {
		t.Run(tc.level, func(t *testing.T) {
			fix := newLifecycleFixture(t)
			report, err := fix.engine.BuildReport(graphstore.BuildOptions{Postprocess: tc.level})
			if err != nil {
				t.Fatalf("BuildReport(%s): %v", tc.level, err)
			}
			timing, present := reportJSON(t, report)["postprocess_timing"]
			if tc.want == nil {
				if present {
					t.Fatalf("postprocess=%s reported timing %v, want the key absent", tc.level, timing)
				}
				return
			}
			if !present {
				t.Fatalf("postprocess=%s reported no timing, want keys %v", tc.level, tc.want)
			}
			got := slices.Sorted(maps.Keys(timing.(map[string]any)))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("postprocess=%s timing keys = %v, want %v", tc.level, got, tc.want)
			}
			for key, value := range timing.(map[string]any) {
				if seconds, ok := value.(float64); !ok || seconds < 0 {
					t.Errorf("timing.%s = %#v, want a non-negative number of seconds", key, value)
				}
			}
		})
	}
}

// TestStandalonePostprocessReportsOnlyEnabledSteps pins the per-step counter
// presence rule from the other direction: a disabled step contributes no key
// at all, rather than a zero.
func TestStandalonePostprocessReportsOnlyEnabledSteps(t *testing.T) {
	fix := newLifecycleFixture(t)
	if _, err := fix.engine.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	report, err := fix.engine.PostprocessReport(graphstore.PostprocessOptions{FTS: new(false)})
	if err != nil {
		t.Fatalf("PostprocessReport: %v", err)
	}
	fields := reportJSON(t, report)
	if _, ok := fields["fts_indexed"]; ok {
		t.Error("fts_indexed reported although the FTS step was disabled")
	}
	for _, key := range []string{"flows_detected", "communities_detected", "signatures_updated"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("%s missing although its step was enabled", key)
		}
	}
	// A standalone pass never carries the build-shaped keys.
	for _, key := range []string{"build_type", "base_resolved", "postprocess_level", "postprocess_timing"} {
		if _, ok := fields[key]; ok {
			t.Errorf("%s reported by a standalone post-process, want it absent", key)
		}
	}
}

// ── Base resolution ──────────────────────────────────────────────────────────

// TestAutomaticBaseFallsBackToFullRebuild proves the escalation upstream's
// resolve_incremental_base exists for: when the commit the graph was built at
// is gone — a history rewrite, a shallow clone, a re-cloned working copy —
// an automatic update must REBUILD, not diff against HEAD~1 and report a
// stale graph as up to date.
func TestAutomaticBaseFallsBackToFullRebuild(t *testing.T) {
	fix := newLifecycleFixture(t)
	if _, err := fix.engine.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	store, err := fix.engine.writeStore()
	if err != nil {
		t.Fatalf("writeStore: %v", err)
	}
	// A well-formed sha that resolves to no object in this repository.
	if err := store.SetMetadata(metaGitHeadSHA, strings.Repeat("a", 40)); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	if err := store.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	report, err := fix.engine.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.BuildType != buildTypeFull {
		t.Errorf("build_type = %q, want %q — an unusable stored anchor must rebuild",
			report.BuildType, buildTypeFull)
	}
	fields := reportJSON(t, report)
	if base, ok := fields["base_resolved"]; !ok || base != nil {
		t.Errorf("base_resolved = %#v (present=%v), want a present null", base, ok)
	}
	if _, ok := fields["files_parsed"]; !ok {
		t.Error("files_parsed missing: the fallback did not run the full-build path")
	}
	// The rebuild must re-stamp the anchor so the NEXT update resolves.
	restamped, err := store.GetMetadata(metaGitHeadSHA)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if restamped != fix.release.HeadSHA {
		t.Errorf("git_head_sha = %q, want the rebuilt-at commit %q", restamped, fix.release.HeadSHA)
	}
}

// TestNonGitWorkingCopyResolvesHeadTilde1 pins the other arm of
// resolve_incremental_base: a working copy with no git metadata has no
// last-synced commit to resolve, and upstream substitutes the fixed
// "HEAD~1" its change discovery ignores anyway.
func TestNonGitWorkingCopyResolvesHeadTilde1(t *testing.T) {
	root := t.TempDir()
	writeGoFixture(t, filepath.Join(root, "go.mod"), "module example.com/plain\n\ngo 1.22\n")
	writeGoFixture(t, filepath.Join(root, "lib.go"), "package lib\n\nfunc Hello() string { return \"hi\" }\n")

	e := Open(root)
	t.Cleanup(func() { _ = e.Close() })
	if _, err := e.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if vcs := graphstore.DetectVCS(root); vcs != graphstore.VCSNone {
		t.Fatalf("fixture root reports vcs %q, want %q", vcs, graphstore.VCSNone)
	}

	report, err := e.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.BuildType != buildTypeIncremental {
		t.Fatalf("build_type = %q, want %q", report.BuildType, buildTypeIncremental)
	}
	if report.BaseResolved == nil || !report.BaseResolved.Valid || report.BaseResolved.Value != nonGitAutoBase {
		t.Errorf("base_resolved = %#v, want %q", report.BaseResolved, nonGitAutoBase)
	}
}

// writeGoFixture writes one source file, creating its directory.
func writeGoFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ── Renames ──────────────────────────────────────────────────────────────────

// TestIncrementalRenamePurgesOldPath pins the reason upstream diffs with
// --name-status rather than --name-only: a rename carries TWO paths, and if
// only the new one reaches the update the old path's nodes and edges stay in
// the graph forever and the incremental result diverges from a rebuild.
func TestIncrementalRenamePurgesOldPath(t *testing.T) {
	fix := newLifecycleFixture(t)
	if _, err := fix.engine.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	const (
		oldRel = "pkg/auth/token.go"
		newRel = "pkg/auth/minting.go"
	)
	oldAbs := fix.engine.absPath(oldRel)
	if nodes, err := storeNodesByFile(t, fix.engine, oldAbs); err != nil || len(nodes) == 0 {
		t.Fatalf("expected the build to index %s (err %v, %d nodes)", oldRel, err, len(nodes))
	}

	base := fix.gitOut("rev-parse", "HEAD")
	runFixtureGit(t, fix.root, "mv", oldRel, newRel)
	runFixtureGit(t, fix.root, "commit", "-q", "-m", "fixture: rename token.go")

	report, err := fix.engine.UpdateReport(graphstore.UpdateOptions{Base: base})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.ChangedFiles == nil {
		t.Fatal("changed_files absent from a real incremental update")
	}
	for _, want := range []string{oldRel, newRel} {
		if !slices.Contains(*report.ChangedFiles, want) {
			t.Errorf("changed_files = %v, want it to carry %q", *report.ChangedFiles, want)
		}
	}

	remaining, err := storeNodesByFile(t, fix.engine, oldAbs)
	if err != nil {
		t.Fatalf("GetNodesByFile: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("%d row(s) still indexed under the renamed-away path %s", len(remaining), oldRel)
	}
	moved, err := storeNodesByFile(t, fix.engine, fix.engine.absPath(newRel))
	if err != nil {
		t.Fatalf("GetNodesByFile: %v", err)
	}
	if len(moved) == 0 {
		t.Errorf("the rename destination %s was not indexed", newRel)
	}
}

// storeNodesByFile reads one file's persisted nodes.
func storeNodesByFile(t *testing.T, e *Engine, absPath string) ([]graphstore.GraphNode, error) {
	t.Helper()
	store, err := e.readStore()
	if err != nil || store == nil {
		return nil, err
	}
	return store.GetNodesByFile(absPath)
}

// ── Status ───────────────────────────────────────────────────────────────────

// TestStatusReportsReleaseContract pins the `status --json` object the CLI
// and the MCP server expose, including the VCS half a build stamps.
func TestStatusReportsReleaseContract(t *testing.T) {
	fix := newLifecycleFixture(t)

	unbuilt, err := fix.engine.Status()
	if err != nil {
		t.Fatalf("Status on an unbuilt graph: %v", err)
	}
	if unbuilt.Ready || unbuilt.State != graphstore.CRGReadinessUnbuilt {
		t.Errorf("unbuilt status = %+v, want an unbuilt, not-ready graph", unbuilt)
	}
	if unbuilt.LastUpdated != nil {
		t.Errorf("last_updated = %v, want null before any build", *unbuilt.LastUpdated)
	}

	if _, err := fix.engine.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	status, err := fix.engine.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Ready || status.State != graphstore.CRGReadinessReady {
		t.Errorf("status = %+v, want a ready graph", status)
	}
	if !slices.Equal(status.Languages, []string{"go"}) {
		t.Errorf("languages = %v, want [go]", status.Languages)
	}
	if status.VCS != graphstore.VCSGit {
		t.Errorf("vcs = %q, want %q", status.VCS, graphstore.VCSGit)
	}
	assertStringPtr(t, "built_on_branch", status.BuiltOnBranch, "main")
	assertStringPtr(t, "built_at_commit", status.BuiltAtCommit, fix.release.HeadSHA)
	assertStringPtr(t, "current_branch", status.CurrentBranch, "main")
	assertStringPtr(t, "current_sha", status.CurrentSHA, fix.release.HeadSHA)
	if status.SVNBranch != nil || status.SVNRevision != nil {
		t.Errorf("svn fields = %v/%v, want null in a git repository", status.SVNBranch, status.SVNRevision)
	}
	if status.LastUpdated == nil {
		t.Fatal("last_updated missing after a build")
	}
	if _, err := time.Parse(upstreamTimeLayout, *status.LastUpdated); err != nil {
		t.Errorf("last_updated = %q, want upstream's %q layout: %v", *status.LastUpdated, upstreamTimeLayout, err)
	}
}

// assertStringPtr checks a nullable status field.
func assertStringPtr(t *testing.T, name string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = null, want %q", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %q, want %q", name, *got, want)
	}
}

// TestBuildStampsUpstreamMetadata pins the metadata keys a build writes; the
// next automatic base resolution and `status --json` both read them back.
func TestBuildStampsUpstreamMetadata(t *testing.T) {
	fix := newLifecycleFixture(t)
	if _, err := fix.engine.BuildReport(graphstore.BuildOptions{Postprocess: graphstore.PostprocessFull}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	store, err := fix.engine.readStore()
	if err != nil || store == nil {
		t.Fatalf("readStore: %v", err)
	}
	for key, want := range map[string]string{
		metaSchemaVersion:    fmt.Sprintf("%d", graphstore.SchemaVersion),
		metaLastBuildType:    buildTypeFull,
		metaGitBranch:        "main",
		metaGitHeadSHA:       fix.release.HeadSHA,
		metaPostprocessLevel: graphstore.PostprocessFull,
	} {
		got, err := store.GetMetadata(key)
		if err != nil {
			t.Fatalf("GetMetadata(%s): %v", key, err)
		}
		if got != want {
			t.Errorf("metadata[%s] = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{metaLastUpdated, metaLastPostprocessed} {
		got, err := store.GetMetadata(key)
		if err != nil {
			t.Fatalf("GetMetadata(%s): %v", key, err)
		}
		if _, perr := time.Parse(upstreamTimeLayout, got); perr != nil {
			t.Errorf("metadata[%s] = %q, want upstream's %q layout: %v", key, got, upstreamTimeLayout, perr)
		}
	}
}

// TestMinimalBuildWritesNoPostprocessMetadata pins the subtlety that
// upstream's minimal pass returns BEFORE stamping the post-process metadata:
// only a full pass records that the expensive stages ran.
func TestMinimalBuildWritesNoPostprocessMetadata(t *testing.T) {
	fix := newLifecycleFixture(t)
	if _, err := fix.engine.BuildReport(graphstore.BuildOptions{Postprocess: graphstore.PostprocessMinimal}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	store, err := fix.engine.readStore()
	if err != nil || store == nil {
		t.Fatalf("readStore: %v", err)
	}
	for _, key := range []string{metaLastPostprocessed, metaPostprocessLevel} {
		got, err := store.GetMetadata(key)
		if err != nil {
			t.Fatalf("GetMetadata(%s): %v", key, err)
		}
		if got != "" {
			t.Errorf("metadata[%s] = %q after a minimal build, want it unwritten", key, got)
		}
	}
}

// ── Options ──────────────────────────────────────────────────────────────────

// TestEmbeddingHalfPairWarns pins upstream's all-or-nothing opt-in: one half
// of the provider/model pair is a warning on the report, not an error, and a
// complete pair adds nothing at all for a graph with no embedding vectors.
func TestEmbeddingHalfPairWarns(t *testing.T) {
	fix := newLifecycleFixture(t)
	half, err := fix.engine.BuildReport(graphstore.BuildOptions{EmbeddingProvider: "local"})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if !slices.Contains(half.Warnings, graphstore.EmbeddingRefreshWarning) {
		t.Errorf("warnings = %v, want upstream's half-pair warning", half.Warnings)
	}

	both, err := fix.engine.BuildReport(graphstore.BuildOptions{
		EmbeddingProvider: "local",
		EmbeddingModel:    "all-MiniLM-L6-v2",
	})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if len(both.Warnings) != 0 {
		t.Errorf("warnings = %v, want none for a complete pair against a graph with no vectors", both.Warnings)
	}
	if _, ok := reportJSON(t, both)["embeddings_refreshed"]; ok {
		t.Error("embeddings_refreshed reported although no vectors exist to refresh")
	}
}

// TestExplicitEmptyBaseIsNotResolved pins the third state of upstream's base
// argument: an explicitly supplied empty ref is a ref that matches nothing,
// which is different from an absent one that gets resolved.
func TestExplicitEmptyBaseIsNotResolved(t *testing.T) {
	fix := newLifecycleFixture(t)
	if _, err := fix.engine.BuildReport(graphstore.BuildOptions{}); err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	report, err := fix.engine.UpdateReport(graphstore.UpdateOptions{Base: "", BaseSet: true})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.BuildType != buildTypeIncremental {
		t.Fatalf("build_type = %q, want %q", report.BuildType, buildTypeIncremental)
	}
	if report.BaseResolved == nil || !report.BaseResolved.Valid || report.BaseResolved.Value != "" {
		t.Errorf("base_resolved = %#v, want a present empty string", report.BaseResolved)
	}
	if report.Summary != graphstore.NoChangesSummary() {
		t.Errorf("summary = %q, want %q", report.Summary, graphstore.NoChangesSummary())
	}
}

// TestEmptyGraphEscalatesToFullRebuild pins upstream's other implicit
// escalation: there is nothing to update incrementally in an empty graph.
func TestEmptyGraphEscalatesToFullRebuild(t *testing.T) {
	fix := newLifecycleFixture(t)
	report, err := fix.engine.UpdateReport(graphstore.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if report.BuildType != buildTypeFull {
		t.Errorf("build_type = %q, want %q for an update against an empty graph",
			report.BuildType, buildTypeFull)
	}
	fix.assertReport("clean_full_build", report)
}

// TestRepoRootOptionBuildsTheRequestedRepository pins the option upstream's
// _resolve_repo_root puts above every other source. Getting this wrong is not
// a missing feature but a confidently wrong answer: the engine's database
// path is derived from its root, so a build that honoured the option's root
// for its SOURCES while keeping this engine's database would write one
// repository's graph into the other's file.
func TestRepoRootOptionBuildsTheRequestedRepository(t *testing.T) {
	other := t.TempDir()
	writeGoFixture(t, filepath.Join(other, "go.mod"), "module example.com/other\n\ngo 1.22\n")
	writeGoFixture(t, filepath.Join(other, "only_here.go"),
		"package other\n\nfunc OnlyHere() string { return \"other\" }\n")

	fix := newLifecycleFixture(t)
	report, err := fix.engine.BuildReport(graphstore.BuildOptions{RepoRoot: other})
	if err != nil {
		t.Fatalf("BuildReport(RepoRoot): %v", err)
	}
	if report.FilesParsed == nil || *report.FilesParsed != 1 {
		t.Fatalf("files_parsed = %v, want the 1 file of the requested repository", report.FilesParsed)
	}

	// The requested repository got its own database…
	otherDB := graphstore.NativeGraphDBPath(other)
	if _, err := os.Stat(otherDB); err != nil {
		t.Fatalf("the requested repository's graph was not created at %s: %v", otherDB, err)
	}
	// …and this engine's own graph was left untouched.
	if _, err := os.Stat(fix.engine.DBPath()); err == nil {
		t.Fatalf("building %s created a graph at the engine's own path %s", other, fix.engine.DBPath())
	}

	// A RepoRoot naming the engine's own root is the no-op branch.
	same, err := fix.engine.BuildReport(graphstore.BuildOptions{RepoRoot: fix.root})
	if err != nil {
		t.Fatalf("BuildReport(own RepoRoot): %v", err)
	}
	fix.assertReport("clean_full_build", same)
}
