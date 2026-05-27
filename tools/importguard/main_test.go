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

// TestClassify is the unit table covering every cell of the narrowed
// policy matrix: same-leaf imports (allowed), cross-leaf imports
// (forbidden), and unrelated import paths (ignored). Adding a new rule
// to classify without adding a row here should fail review.
//
// Outsider importers (e.g. internal/projectsync importing a leaf)
// would be blocked by Go's internal/ rule before they could reach
// this tool, so they are intentionally absent here — the row labelled
// "outsider edges are ignored (compiler blocks them)" documents that
// classify treats them as a non-event rather than a violation.
func TestClassify(t *testing.T) {
	mod := modulePath + "/"
	tests := []struct {
		name     string
		importer string
		target   string
		wantBad  bool
	}{
		// ── Same-leaf imports (intra-subpackage) ──────────────────
		{
			name:     "lifecycle internal helper imports its own leaf",
			importer: mod + "commands/internal/lifecycle/internal/foo",
			target:   mod + "commands/internal/lifecycle",
			wantBad:  false,
		},
		{
			name:     "lifecycle leaf self-edge is fine",
			importer: mod + "commands/internal/lifecycle",
			target:   mod + "commands/internal/lifecycle",
			wantBad:  false,
		},

		// ── Forbidden cross-leaf edges ────────────────────────────
		{
			name:     "mcp must not import settings",
			importer: mod + "commands/internal/mcp",
			target:   mod + "commands/internal/settings",
			wantBad:  true,
		},
		{
			name:     "settings must not import rules",
			importer: mod + "commands/internal/settings",
			target:   mod + "commands/internal/rules",
			wantBad:  true,
		},
		{
			name:     "lifecycle must not import mcp",
			importer: mod + "commands/internal/lifecycle",
			target:   mod + "commands/internal/mcp",
			wantBad:  true,
		},
		{
			name:     "rules must not import lifecycle",
			importer: mod + "commands/internal/rules",
			target:   mod + "commands/internal/lifecycle",
			wantBad:  true,
		},

		// ── Outsider edges are now compiler-blocked ──────────────
		// Go's internal/ rule prevents these at build time. classify
		// returns false (no-op) rather than re-flagging them — the
		// build would have failed first.
		{
			name:     "outsider edges are ignored (compiler blocks them)",
			importer: mod + "internal/projectsync",
			target:   mod + "commands/internal/mcp",
			wantBad:  false,
		},
		{
			name:     "root commands package edge into leaf is allowed",
			importer: mod + "commands",
			target:   mod + "commands/internal/lifecycle",
			wantBad:  false,
		},
		{
			name:     "cmd/dot-agents edge into leaf is allowed",
			importer: mod + "cmd/dot-agents",
			target:   mod + "commands/internal/lifecycle",
			wantBad:  false,
		},

		// ── Prefix-confusion guard ────────────────────────────────
		// commands/internal/lifecyclehelper must NOT be treated as
		// part of commands/internal/lifecycle's budget.
		{
			name:     "look-alike importer outside guarded subtree is ignored",
			importer: mod + "commands/internal/lifecyclehelper",
			target:   mod + "commands/internal/lifecycle",
			wantBad:  false,
		},
		{
			name:     "look-alike target is not classified as guarded",
			importer: mod + "commands",
			target:   mod + "commands/internal/lifecyclehelper",
			wantBad:  false,
		},

		// ── Unrelated imports passthrough ─────────────────────────
		{
			name:     "stdlib import is ignored",
			importer: mod + "commands/internal/mcp",
			target:   "fmt",
			wantBad:  false,
		},
		{
			name:     "third-party import is ignored",
			importer: mod + "commands/internal/mcp",
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
	if !strings.Contains(v.reason, "sibling subpackage") {
		t.Errorf("violation reason should name the rule (sibling subpackage), got %q", v.reason)
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
		{mod + "commands/internal/lifecycle", mod + "commands/internal/lifecycle"},
		{mod + "commands/internal/lifecycle/internal/foo", mod + "commands/internal/lifecycle"},
		{mod + "commands/internal/mcp", mod + "commands/internal/mcp"},
		{mod + "commands/internal/settings/sub", mod + "commands/internal/settings"},
		{mod + "commands/internal/rules", mod + "commands/internal/rules"},

		// Not guarded.
		{mod + "commands", ""},
		{mod + "commands/agents", ""},
		{mod + "commands/internal/lifecyclehelper", ""},
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
//
// Note: outsider edges (e.g. internal/projectsync -> mcp) are NOT in
// the expected output anymore — Go's internal/ rule blocks them at
// compile time, so the tool intentionally ignores them. Only true
// cross-leaf edges (mcp -> settings, lifecycle -> rules) fire.
func TestCheckPackagesSynthetic(t *testing.T) {
	mod := modulePath + "/"

	lifecycle := &packages.Package{PkgPath: mod + "commands/internal/lifecycle"}
	mcp := &packages.Package{PkgPath: mod + "commands/internal/mcp"}
	settings := &packages.Package{PkgPath: mod + "commands/internal/settings"}
	rules := &packages.Package{PkgPath: mod + "commands/internal/rules"}

	// Sibling-leaf cross edge (mcp -> settings) should fire.
	mcpCross := &packages.Package{
		PkgPath: mod + "commands/internal/mcp",
		Imports: map[string]*packages.Package{
			settings.PkgPath: settings,
		},
	}
	// A second cross-leaf edge (lifecycle -> rules) with a stable
	// sibling to confirm checkPackages sorts the output.
	lifecycleCross := &packages.Package{
		PkgPath: mod + "commands/internal/lifecycle",
		Imports: map[string]*packages.Package{
			rules.PkgPath: rules,
		},
	}
	// Outsider edge — must NOT show up: Go's internal/ rule blocks
	// it at compile time, so we treat it as a no-op.
	outsider := &packages.Package{
		PkgPath: mod + "internal/projectsync",
		Imports: map[string]*packages.Package{
			lifecycle.PkgPath: lifecycle,
			mcp.PkgPath:       mcp,
		},
	}
	// Allowed importer (root commands) — must NOT show up.
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
		outsider,
		mcpCross,
		lifecycleCross,
	})

	want := []violation{
		{importer: mod + "commands/internal/lifecycle", target: mod + "commands/internal/rules"},
		{importer: mod + "commands/internal/mcp", target: mod + "commands/internal/settings"},
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
			importer: modulePath + "/commands/internal/mcp",
			target:   modulePath + "/commands/internal/settings",
			reason:   "sibling-leaf demo",
		},
	})
	out := buf.String()
	for _, want := range []string{
		"importguard: 1 disallowed",
		"commands/internal/mcp -> commands/internal/settings",
		"sibling-leaf demo",
		"cross-leaf isolation",
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
		importer: modulePath + "/commands/internal/mcp",
		target:   modulePath + "/commands/internal/settings",
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
	if !strings.Contains(stderr, "commands/internal/mcp -> commands/internal/settings") {
		t.Errorf("stderr should contain violation edge: %q", stderr)
	}
}

func testMainRunBadFlag(t *testing.T) {
	code, _ := runMainCase([]string{"-unknown-flag"}, cleanRunFunc)
	if code != 2 {
		t.Errorf("bad flag exit=%d, want 2", code)
	}
}

// TestRunSurfacesPackageErrors exercises the production run() function
// end-to-end through the real loadPackages var, swapping its
// implementation to return synthetic errors. This covers the
// packages.PrintErrors branch — that path is otherwise unreachable from
// TestRepoIsClean, which only loads a healthy graph.
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
