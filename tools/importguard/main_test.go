package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
			v, bad := classify(tc.importer, tc.target)
			if bad != tc.wantBad {
				t.Fatalf("classify(%q, %q) bad=%v, want %v (violation=%+v)",
					tc.importer, tc.target, bad, tc.wantBad, v)
			}
			if bad {
				if v.importer != tc.importer || v.target != tc.target {
					t.Errorf("violation echoes wrong identifiers: got importer=%q target=%q, want %q/%q",
						v.importer, v.target, tc.importer, tc.target)
				}
				if v.reason == "" {
					t.Errorf("violation must carry a non-empty reason for CI log")
				}
			}
		})
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
