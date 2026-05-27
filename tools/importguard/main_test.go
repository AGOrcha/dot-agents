package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestClassify is the unit table covering every cell of the policy matrix:
// allowed root importers, same-subtree imports, forbidden sibling-leaf
// imports, and unrelated import paths. Adding a new rule to classify
// without adding a row here should fail review.
func TestClassify(t *testing.T) {
	mod := modulePath + "/"
	tests := []struct {
		name     string
		importer string
		target   string
		wantBad  bool
	}{
		// ── Allowed root importers ────────────────────────────────
		{
			name:     "commands root imports lifecycle",
			importer: mod + "commands",
			target:   mod + "commands/lifecycle",
			wantBad:  false,
		},
		{
			name:     "commands root imports mcp",
			importer: mod + "commands",
			target:   mod + "commands/mcp",
			wantBad:  false,
		},
		{
			name:     "commands root imports settings",
			importer: mod + "commands",
			target:   mod + "commands/settings",
			wantBad:  false,
		},
		{
			name:     "commands root imports rules",
			importer: mod + "commands",
			target:   mod + "commands/rules",
			wantBad:  false,
		},
		{
			name:     "cmd/dot-agents may import lifecycle",
			importer: mod + "cmd/dot-agents",
			target:   mod + "commands/lifecycle",
			wantBad:  false,
		},

		// ── Same-subtree imports (intra-subpackage) ───────────────
		{
			name:     "lifecycle internal helper imports its own leaf",
			importer: mod + "commands/lifecycle/internal/foo",
			target:   mod + "commands/lifecycle",
			wantBad:  false,
		},
		{
			name:     "lifecycle leaf self-edge is fine",
			importer: mod + "commands/lifecycle",
			target:   mod + "commands/lifecycle",
			wantBad:  false,
		},

		// ── Forbidden cross-leaf edges ────────────────────────────
		{
			name:     "mcp must not import settings",
			importer: mod + "commands/mcp",
			target:   mod + "commands/settings",
			wantBad:  true,
		},
		{
			name:     "settings must not import rules",
			importer: mod + "commands/settings",
			target:   mod + "commands/rules",
			wantBad:  true,
		},
		{
			name:     "lifecycle must not import mcp",
			importer: mod + "commands/lifecycle",
			target:   mod + "commands/mcp",
			wantBad:  true,
		},
		{
			name:     "rules must not import lifecycle",
			importer: mod + "commands/rules",
			target:   mod + "commands/lifecycle",
			wantBad:  true,
		},

		// ── Forbidden outsider edges (negative cases) ─────────────
		{
			name:     "random sibling commands subpackage cannot import lifecycle",
			importer: mod + "commands/agents",
			target:   mod + "commands/lifecycle",
			wantBad:  true,
		},
		{
			name:     "internal package cannot reach into mcp",
			importer: mod + "internal/projectsync",
			target:   mod + "commands/mcp",
			wantBad:  true,
		},

		// ── Prefix-confusion guard ────────────────────────────────
		// commands/lifecyclehelper must NOT be treated as part of
		// commands/lifecycle's budget.
		{
			name:     "look-alike importer outside guarded subtree is rejected",
			importer: mod + "commands/lifecyclehelper",
			target:   mod + "commands/lifecycle",
			wantBad:  true,
		},
		{
			name:     "look-alike target is not classified as guarded",
			importer: mod + "commands",
			target:   mod + "commands/lifecyclehelper",
			wantBad:  false,
		},

		// ── Unrelated imports passthrough ─────────────────────────
		{
			name:     "stdlib import is ignored",
			importer: mod + "commands/mcp",
			target:   "fmt",
			wantBad:  false,
		},
		{
			name:     "third-party import is ignored",
			importer: mod + "commands/mcp",
			target:   "github.com/spf13/cobra",
			wantBad:  false,
		},
		{
			name:     "non-guarded commands subpackage target is ignored",
			importer: mod + "internal/whatever",
			target:   mod + "commands/agents",
			wantBad:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertClassify(t, tc.importer, tc.target, tc.wantBad)
		})
	}
}

// assertClassify is the per-row check extracted from TestClassify so the
// table loop stays a flat dispatch (cognitive complexity stays well
// below Sonar's threshold). It runs classify() and validates both the
// boolean verdict and — when the verdict says "violation" — that the
// returned struct echoes the inputs and carries a non-empty reason for
// the CI log.
func assertClassify(t *testing.T, importer, target string, wantBad bool) {
	t.Helper()
	v, bad := classify(importer, target)
	if bad != wantBad {
		t.Fatalf("classify(%q, %q) bad=%v, want %v (violation=%+v)",
			importer, target, bad, wantBad, v)
	}
	if !bad {
		return
	}
	if v.importer != importer || v.target != target {
		t.Errorf("violation echoes wrong identifiers: got importer=%q target=%q, want %q/%q",
			v.importer, v.target, importer, target)
	}
	if v.reason == "" {
		t.Error("violation must carry a non-empty reason for CI log")
	}
}

// TestGuardedSubpackageFor and TestInSubpackage cover the two predicates
// classify leans on. Keeping them as separate tests means a regression
// here points at the helper instead of cascading through every classify
// row.
func TestGuardedSubpackageFor(t *testing.T) {
	mod := modulePath + "/"
	cases := []struct {
		in   string
		want string
	}{
		{mod + "commands/lifecycle", mod + "commands/lifecycle"},
		{mod + "commands/lifecycle/internal/foo", mod + "commands/lifecycle"},
		{mod + "commands/mcp", mod + "commands/mcp"},
		{mod + "commands/settings/sub", mod + "commands/settings"},
		{mod + "commands/rules", mod + "commands/rules"},

		// Not guarded.
		{mod + "commands", ""},
		{mod + "commands/agents", ""},
		{mod + "commands/lifecyclehelper", ""},
		{"fmt", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := guardedSubpackageFor(c.in); got != c.want {
			t.Errorf("guardedSubpackageFor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInSubpackage(t *testing.T) {
	cases := []struct {
		candidate, sub string
		want           bool
	}{
		{"a/b", "a/b", true},
		{"a/b/c", "a/b", true},
		{"a/bc", "a/b", false}, // critical: prefix-with-slash guard
		{"a", "a/b", false},
		{"", "a/b", false},
	}
	for _, c := range cases {
		if got := inSubpackage(c.candidate, c.sub); got != c.want {
			t.Errorf("inSubpackage(%q, %q) = %v, want %v",
				c.candidate, c.sub, got, c.want)
		}
	}
}

// TestReasonFor exercises the two branches of the human-readable message:
// sibling-leaf violations vs. unrelated outsider violations. Stable
// wording is part of the CI failure UX, so both shapes are asserted.
func TestReasonFor(t *testing.T) {
	mod := modulePath + "/"
	cross := reasonFor(mod+"commands/mcp", mod+"commands/settings")
	if !strings.Contains(cross, "sibling subpackage") {
		t.Errorf("cross-leaf reason should mention sibling subpackage, got %q", cross)
	}
	out := reasonFor(mod+"internal/projectsync", mod+"commands/lifecycle")
	if !strings.Contains(out, "allowed-importer set") {
		t.Errorf("outsider reason should mention allowed-importer set, got %q", out)
	}
}

// TestRepoIsClean is the live end-to-end assertion: the guard, run against
// the actual repository under HEAD, must report zero violations. Without
// this we could ship a tool that passes its unit tests but blocks CI on
// the very commit that lands it. Marked Long so a `-short` run skips the
// heavier package load, and skipped when run outside a module checkout
// (e.g. on the installed binary path used by `go install`).
func TestRepoIsClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping repo-level import scan in -short mode")
	}
	root, ok := repoRoot()
	if !ok {
		t.Skip("repo root not detectable from test binary location; skipping")
	}
	// Run from the repo root so ./... resolves to the module's package set.
	t.Chdir(root)
	violations, err := run([]string{"./..."})
	if err != nil {
		t.Fatalf("run(./...): %v", err)
	}
	if len(violations) > 0 {
		var b strings.Builder
		for _, v := range violations {
			b.WriteString("  " + v.importer + " -> " + v.target + "\n")
		}
		t.Fatalf("expected zero policy violations at HEAD, got %d:\n%s",
			len(violations), b.String())
	}
}

// TestCheckPackagesSynthetic feeds checkPackages a hand-built graph so
// every branch — skip nil/empty, skip error-tagged, accumulate violation,
// stable sort — runs without invoking the real Go toolchain. This is the
// counterpart to TestRepoIsClean: that test confirms the production graph
// is clean, this one confirms the detector still fires when drift exists.
func TestCheckPackagesSynthetic(t *testing.T) {
	mod := modulePath + "/"

	lifecycle := &packages.Package{PkgPath: mod + "commands/lifecycle"}
	mcp := &packages.Package{PkgPath: mod + "commands/mcp"}
	settings := &packages.Package{PkgPath: mod + "commands/settings"}

	// Importer that violates twice: an internal/* package reaching into
	// lifecycle and the same package reaching into mcp. We expect both
	// edges reported, sorted by (importer, target).
	bad := &packages.Package{
		PkgPath: mod + "internal/projectsync",
		Imports: map[string]*packages.Package{
			lifecycle.PkgPath: lifecycle,
			mcp.PkgPath:       mcp,
		},
	}
	// Sibling-leaf cross edge (mcp -> settings) should also fire.
	mcpCross := &packages.Package{
		PkgPath: mod + "commands/mcp",
		Imports: map[string]*packages.Package{
			settings.PkgPath: settings,
		},
	}
	// Allowed importer — must NOT show up in the output.
	rootOK := &packages.Package{
		PkgPath: mod + "commands",
		Imports: map[string]*packages.Package{
			lifecycle.PkgPath: lifecycle,
			mcp.PkgPath:       mcp,
		},
	}
	// Skip cases: nil entry, empty path, error-tagged package.
	errPkg := &packages.Package{
		PkgPath: mod + "commands/something",
		Errors:  []packages.Error{{Msg: "synthetic load failure"}},
		Imports: map[string]*packages.Package{
			lifecycle.PkgPath: lifecycle,
		},
	}

	got := checkPackages([]*packages.Package{
		nil,
		{PkgPath: ""},
		errPkg,
		rootOK,
		bad,
		mcpCross,
	})

	want := []violation{
		{importer: mod + "commands/mcp", target: mod + "commands/settings"},
		{importer: mod + "internal/projectsync", target: mod + "commands/lifecycle"},
		{importer: mod + "internal/projectsync", target: mod + "commands/mcp"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d violations, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].importer != w.importer || got[i].target != w.target {
			t.Errorf("violation %d: got %s -> %s, want %s -> %s",
				i, got[i].importer, got[i].target, w.importer, w.target)
		}
		if got[i].reason == "" {
			t.Errorf("violation %d: empty reason", i)
		}
	}
}

// TestReportViolations confirms the failure log shape: header with count,
// one indented line per edge, trailing guidance. The exact wording is part
// of the CI UX, so we assert key phrases instead of a full string match
// (which would make the test brittle to message tweaks).
func TestReportViolations(t *testing.T) {
	var buf bytes.Buffer
	reportViolations(&buf, []violation{
		{
			importer: modulePath + "/commands/mcp",
			target:   modulePath + "/commands/settings",
			reason:   "sibling-leaf demo",
		},
	})
	out := buf.String()
	for _, want := range []string{
		"importguard: 1 disallowed",
		"commands/mcp -> commands/settings",
		"sibling-leaf demo",
		"root-command-decomposition",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reportViolations output missing %q\nfull:\n%s", want, out)
		}
	}
}

// cleanRunFunc is a fake loader that reports an always-clean graph.
// Hoisted to file scope so every test that needs "load returned no
// violations" can reuse the same value without redeclaring a closure.
func cleanRunFunc(_ []string) ([]violation, error) { return nil, nil }

// failRunFunc fakes a load-time failure so tests can drive mainRun's
// exit-2 branch deterministically.
func failRunFunc(_ []string) ([]violation, error) {
	return nil, errors.New("synthetic load failure")
}

// violRunFunc fakes a successful load that surfaces one violation so
// tests can drive mainRun's exit-1 branch and assert the rendered edge.
func violRunFunc(_ []string) ([]violation, error) {
	return []violation{{
		importer: modulePath + "/commands/mcp",
		target:   modulePath + "/commands/settings",
		reason:   "synthetic violation",
	}}, nil
}

// runMainCase invokes mainRun with the given inputs and returns the exit
// code plus stderr buffer so individual subtests can assert against both
// without re-implementing the bytes.Buffer plumbing each time. Splitting
// this out keeps TestMainRun a flat dispatcher of subtests.
func runMainCase(args []string, load runFunc) (int, string) {
	var buf bytes.Buffer
	code := mainRun(args, &buf, load)
	return code, buf.String()
}

// TestMainRun drives every exit-code path through the testable
// entrypoint. Each subtest is a one-liner over runMainCase + per-case
// helper so the top-level function's cognitive complexity stays minimal.
// The fake runFunc values are file-scope so they can be referenced
// without nesting closures inside the test body.
func TestMainRun(t *testing.T) {
	t.Run("clean exits 0", testMainRunClean)
	t.Run("default pattern is ./...", testMainRunDefaultPattern)
	t.Run("explicit patterns override default", testMainRunExplicitPatterns)
	t.Run("load error exits 2", testMainRunLoadError)
	t.Run("violations exit 1 and render", testMainRunViolations)
	t.Run("bad flag exits 2", testMainRunBadFlag)
}

func testMainRunClean(t *testing.T) {
	code, stderr := runMainCase(nil, cleanRunFunc)
	if code != 0 {
		t.Errorf("clean run exit=%d, want 0 (stderr=%q)", code, stderr)
	}
}

func testMainRunDefaultPattern(t *testing.T) {
	var seen []string
	spy := func(patterns []string) ([]violation, error) {
		seen = patterns
		return nil, nil
	}
	_, _ = runMainCase(nil, spy)
	if len(seen) != 1 || seen[0] != "./..." {
		t.Errorf("default patterns = %v, want [./...]", seen)
	}
}

func testMainRunExplicitPatterns(t *testing.T) {
	var seen []string
	spy := func(patterns []string) ([]violation, error) {
		seen = patterns
		return nil, nil
	}
	_, _ = runMainCase([]string{"./tools/...", "./commands/..."}, spy)
	want := []string{"./tools/...", "./commands/..."}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] {
		t.Errorf("explicit patterns = %v, want %v", seen, want)
	}
}

func testMainRunLoadError(t *testing.T) {
	code, stderr := runMainCase(nil, failRunFunc)
	if code != 2 {
		t.Errorf("load failure exit=%d, want 2", code)
	}
	if !strings.Contains(stderr, "synthetic load failure") {
		t.Errorf("stderr should surface the load error: %q", stderr)
	}
}

func testMainRunViolations(t *testing.T) {
	code, stderr := runMainCase(nil, violRunFunc)
	if code != 1 {
		t.Errorf("violation run exit=%d, want 1", code)
	}
	if !strings.Contains(stderr, "commands/mcp -> commands/settings") {
		t.Errorf("stderr should contain violation edge: %q", stderr)
	}
}

func testMainRunBadFlag(t *testing.T) {
	code, _ := runMainCase([]string{"-unknown-flag"}, cleanRunFunc)
	if code != 2 {
		t.Errorf("bad flag exit=%d, want 2", code)
	}
}

// TestRun exercises the production run() function end-to-end through the
// real loadPackages var, swapping its implementation to return synthetic
// errors. This covers the packages.PrintErrors branch — that path is
// otherwise unreachable from TestRepoIsClean, which only loads a healthy
// graph.
func TestRunSurfacesPackageErrors(t *testing.T) {
	original := loadPackages
	t.Cleanup(func() { loadPackages = original })

	loadPackages = func(patterns []string) ([]*packages.Package, error) {
		return []*packages.Package{{
			PkgPath: modulePath + "/commands/synthetic",
			Errors:  []packages.Error{{Msg: "fake load error"}},
		}}, nil
	}
	_, err := run([]string{"./..."})
	if err == nil {
		t.Fatal("run should surface package errors as a top-level error")
	}
	if !strings.Contains(err.Error(), "package load reported errors") {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestRunPropagatesLoaderError(t *testing.T) {
	original := loadPackages
	t.Cleanup(func() { loadPackages = original })

	want := errors.New("loader exploded")
	loadPackages = func(patterns []string) ([]*packages.Package, error) {
		return nil, want
	}
	_, err := run([]string{"./..."})
	if !errors.Is(err, want) {
		t.Errorf("run should propagate loader error, got %v", err)
	}
}

// repoRoot walks up from this test file's location until it finds the
// module's go.mod. Returning ok=false (instead of failing) lets
// TestRepoIsClean skip cleanly when the test binary is executed without
// source-tree context, which keeps the test friendly to packaged runs.
func repoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
