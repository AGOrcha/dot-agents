package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// pruneFixture is one sandboxed prune scenario: a project whose lock pins the
// "live" digest of a layer and a prompt, plus a superseded entry of each in the
// shared cache. HOME and AGENTS_HOME are sandboxed by withRepoLayer, so no test
// here can ever reach the developer's real cache.
type pruneFixture struct {
	project   string
	liveLayer string
	dead      []string
}

// newPruneFixture seeds the sandboxed cache + lock and returns the fixture.
func newPruneFixture(t *testing.T) pruneFixture {
	t.Helper()
	project := withRepoLayer(t, `{"version": 2}`, "")
	liveLayerSHA, deadLayerSHA := "aaaa111122223333", "bbbb444455556666"
	promptRef := cfg.PromptUnitRef{SourceID: "team", Path: "verifiers/ts-lint.md"}
	livePromptSHA, deadPromptSHA := "cccc777788889999", "dddd000011112222"

	writeLock(t, project, map[string]cfg.LockedUnit{
		"team:team/base.json":       {Kind: "layer", Digest: liveLayerSHA},
		"team:verifiers/ts-lint.md": {Kind: "prompt", Digest: livePromptSHA},
	})
	live := seedLayerCacheEntry(t, "team", "team/base.json", liveLayerSHA)
	dead := seedLayerCacheEntry(t, "team", "team/base.json", deadLayerSHA)
	seedPromptCacheEntry(t, promptRef, livePromptSHA)
	deadPrompt := seedPromptCacheEntry(t, promptRef, deadPromptSHA)
	return pruneFixture{project: project, liveLayer: live, dead: []string{dead, deadPrompt}}
}

// writeLock writes a units lock for the project.
func writeLock(t *testing.T, project string, units map[string]cfg.LockedUnit) {
	t.Helper()
	if err := cfg.WriteUnitsLock(project, cfg.UnitsLock{Units: units}); err != nil {
		t.Fatal(err)
	}
}

// seedLayerCacheEntry writes a cached layer.json and returns its entry dir.
func seedLayerCacheEntry(t *testing.T, sourceID, layerPath, sha string) string {
	t.Helper()
	dir := filepath.Join(cfg.AgentsHome(), "cache", "config", sourceID, filepath.FromSlash(layerPath), sha)
	writeCacheFile(t, filepath.Join(dir, "layer.json"), `{"version":2}`)
	return dir
}

// seedPromptCacheEntry writes cached prompt bytes and returns its entry dir.
func seedPromptCacheEntry(t *testing.T, ref cfg.PromptUnitRef, sha string) string {
	t.Helper()
	path := cfg.CachedPromptPath(ref, sha)
	writeCacheFile(t, path, "# prompt\n")
	return filepath.Dir(path)
}

func writeCacheFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// prunerOptions builds run options bound to the fixture project with an empty
// registry seam (the cwd project supplies the whole live set).
func prunerOptions(project string, jsonOut bool) *runCachePruneOptions {
	return &runCachePruneOptions{
		runContext: runContext{jsonOut: jsonOut, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, cwd: project},
		projects:   func() ([]string, error) { return nil, nil },
	}
}

// pruneReportFrom runs the prune and decodes its JSON report.
func pruneReportFrom(t *testing.T, opts *runCachePruneOptions) CachePruneReport {
	t.Helper()
	if err := runCachePrune(opts, testDeps()); err != nil {
		t.Fatalf("runCachePrune: %v", err)
	}
	var report CachePruneReport
	out := opts.stdout.(*bytes.Buffer).Bytes()
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decoding report: %v\n%s", err, out)
	}
	return report
}

// assertPathsExist asserts every path is still present.
func assertPathsExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %q to still exist: %v", p, err)
		}
	}
}

// TestRunCachePruneDryRunListsWithoutRemoving is the default contract: the
// superseded layer AND prompt entries are listed, nothing is deleted.
func TestRunCachePruneDryRunListsWithoutRemoving(t *testing.T) {
	fx := newPruneFixture(t)
	report := pruneReportFrom(t, prunerOptions(fx.project, true))

	if report.Applied {
		t.Fatal("the default run must be a dry run")
	}
	if len(report.Entries) != 2 {
		t.Fatalf("entries = %#v, want the two superseded entries", report.Entries)
	}
	if report.Bytes == 0 {
		t.Fatal("a dry run must report the reclaimable bytes")
	}
	assertPathsExist(t, append(fx.dead, fx.liveLayer)...)
}

// TestRunCachePruneApplyRemovesOnlyUnreferenced proves --apply deletes exactly
// the unreferenced entries and never a lock-referenced one.
func TestRunCachePruneApplyRemovesOnlyUnreferenced(t *testing.T) {
	fx := newPruneFixture(t)
	opts := prunerOptions(fx.project, true)
	opts.apply = true
	report := pruneReportFrom(t, opts)

	if !report.Applied || len(report.Entries) != 2 || report.Bytes == 0 {
		t.Fatalf("apply report = %#v, want two removed entries with reclaimed bytes", report)
	}
	for _, dir := range fx.dead {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("expected %q removed, stat err = %v", dir, err)
		}
	}
	assertPathsExist(t, fx.liveLayer)
}

// TestRunCachePruneDryRunFlagOverridesApply proves the global --dry-run wins over
// the local --apply opt-in, per the documented preview contract.
func TestRunCachePruneDryRunFlagOverridesApply(t *testing.T) {
	fx := newPruneFixture(t)
	opts := prunerOptions(fx.project, true)
	opts.apply = true
	opts.dryRun = true
	report := pruneReportFrom(t, opts)

	if report.Applied {
		t.Fatal("--dry-run must force a preview even with --apply")
	}
	assertPathsExist(t, fx.dead...)
}

// TestRunCachePruneHumanOutput covers the human render for both the
// something-to-prune and nothing-to-prune shapes.
func TestRunCachePruneHumanOutput(t *testing.T) {
	fx := newPruneFixture(t)
	opts := prunerOptions(fx.project, false)
	if err := runCachePrune(opts, testDeps()); err != nil {
		t.Fatalf("runCachePrune: %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	for _, want := range []string{"--dry-run", "would rm", "prunable", "--apply"} {
		if !strings.Contains(out, want) {
			t.Fatalf("human output missing %q:\n%s", want, out)
		}
	}

	// With every entry referenced (an empty cache), the render says so.
	clean := withRepoLayer(t, `{"version": 2}`, "")
	cleanOpts := prunerOptions(clean, false)
	if err := runCachePrune(cleanOpts, testDeps()); err != nil {
		t.Fatalf("runCachePrune (clean): %v", err)
	}
	if got := cleanOpts.stdout.(*bytes.Buffer).String(); !strings.Contains(got, "nothing to prune") {
		t.Fatalf("clean output = %q, want the nothing-to-prune line", got)
	}
}

// TestRunCachePruneAppliedHumanOutput covers the applied render (verbs + summary).
func TestRunCachePruneAppliedHumanOutput(t *testing.T) {
	fx := newPruneFixture(t)
	opts := prunerOptions(fx.project, false)
	opts.apply = true
	if err := runCachePrune(opts, testDeps()); err != nil {
		t.Fatalf("runCachePrune: %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	for _, want := range []string{"removed", "reclaimed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("applied output missing %q:\n%s", want, out)
		}
	}
}

// TestRunCachePruneReportsSkippedProjects proves a registered project with no
// lockfile is surfaced rather than silently narrowing the live set.
func TestRunCachePruneReportsSkippedProjects(t *testing.T) {
	fx := newPruneFixture(t)
	opts := prunerOptions(fx.project, false)
	lockless := t.TempDir()
	opts.projects = func() ([]string, error) { return []string{lockless}, nil }
	if err := runCachePrune(opts, testDeps()); err != nil {
		t.Fatalf("runCachePrune: %v", err)
	}
	if got := opts.stdout.(*bytes.Buffer).String(); !strings.Contains(got, "Skipped (no lockfile): 1 project") {
		t.Fatalf("output = %q, want the skipped-project note", got)
	}
}

// TestRunCachePruneErrors covers the two failure surfaces: an unreadable
// registry and an unparseable project lock (which must fail closed).
func TestRunCachePruneErrors(t *testing.T) {
	fx := newPruneFixture(t)
	opts := prunerOptions(fx.project, false)
	opts.projects = func() ([]string, error) { return nil, errors.New("registry unreadable") }
	if err := runCachePrune(opts, testDeps()); err == nil {
		t.Fatal("expected a registry read failure to surface")
	}

	broken := withRepoLayer(t, `{"version": 2}`, "")
	if err := os.WriteFile(filepath.Join(broken, ".agentsrc.lock"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCachePrune(prunerOptions(broken, false), testDeps()); err == nil {
		t.Fatal("expected an unparseable lock to fail the prune")
	}
}

// TestRegisteredProjectPaths covers the real registry seam against a sandboxed
// home: a bound project contributes its path, a known-but-unbound one does not.
func TestRegisteredProjectPaths(t *testing.T) {
	withRepoLayer(t, `{"version": 2}`, "")
	home, err := cfg.Load()
	if err != nil {
		t.Fatal(err)
	}
	bound := t.TempDir()
	home.Projects["bound"] = cfg.Project{}
	home.Projects["unbound"] = cfg.Project{}
	home.BindProject("bound", bound)
	if err := home.Save(); err != nil {
		t.Fatal(err)
	}

	paths, err := registeredProjectPaths()
	if err != nil {
		t.Fatalf("registeredProjectPaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != filepath.Clean(bound) {
		t.Fatalf("paths = %#v, want only the bound project %q", paths, bound)
	}
}

// TestCachePruneOptionsProjectSeamDefault proves the options fall back to the
// real registry when no seam is injected.
func TestCachePruneOptionsProjectSeamDefault(t *testing.T) {
	withRepoLayer(t, `{"version": 2}`, "")
	opts := &runCachePruneOptions{}
	if _, err := opts.projectPaths(); err != nil {
		t.Fatalf("default project seam: %v", err)
	}
}

// TestHumanBytes covers the size formatting across unit boundaries.
func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                       "0 B",
		512:                     "512 B",
		2048:                    "2.0 KiB",
		3 * 1024 * 1024:         "3.0 MiB",
		5 * 1 << 30:             "5.0 GiB",
		2 * 1 << 40:             "2.0 TiB",
		1024 * 1024 * 1024 * 10: "10.0 GiB",
	}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestNewCacheCmdWiring proves the subtree exposes `cache prune`.
func TestNewCacheCmdWiring(t *testing.T) {
	cmd := newCacheCmd(testDeps())
	if cmd.Use != "cache" {
		t.Fatalf("command Use = %q", cmd.Use)
	}
	var found bool
	for _, sub := range cmd.Commands() {
		if sub.Use == "prune" {
			found = true
		}
	}
	if !found {
		t.Fatalf("`da config cache` must expose prune, got %#v", cmd.Commands())
	}
}
