package graphstore

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestEnumerateTrackedFiles_PlainEnumerationMissesSubmoduleFiles is the
// regression pin for the defect itself: the non-recursive enumeration — what
// the build used to do — sees the submodule as a single gitlink entry and
// none of the files inside it. The recursive enumeration picks all of them up.
func TestEnumerateTrackedFiles_PlainEnumerationMissesSubmoduleFiles(t *testing.T) {
	super := superprojectFixture(t)

	plain, err := EnumerateTrackedFiles(super, false)
	if err != nil {
		t.Fatalf("plain enumeration: %v", err)
	}
	recursed, err := EnumerateTrackedFiles(super, true)
	if err != nil {
		t.Fatalf("recursive enumeration: %v", err)
	}

	submoduleFiles := []string{"vendor/lib/lib.go", "vendor/lib/widget.go", "vendor/lib/internal/x.go"}
	for _, f := range submoduleFiles {
		if contains(plain, f) {
			t.Errorf("plain enumeration unexpectedly saw %s", f)
		}
		if !contains(recursed, f) {
			t.Errorf("recursive enumeration missed %s (got %v)", f, recursed)
		}
	}
	if contains(plain, "vendor/lib") != true {
		t.Errorf("plain enumeration should list the gitlink entry itself, got %v", plain)
	}
	// The recursive walk replaces the single gitlink entry with the submodule's
	// real files: 4 entries become 6, and the three that appear are exactly the
	// files a plain-enumeration build never parsed.
	if len(plain) != 4 || len(recursed) != 6 {
		t.Errorf("enumeration counts = plain %d / recursed %d, want 4 / 6 (plain=%v recursed=%v)",
			len(plain), len(recursed), plain, recursed)
	}
}

// TestDiscoverSubmodules_FindsInitializedGitlink pins discovery against a real
// `git submodule add`.
func TestDiscoverSubmodules_FindsInitializedGitlink(t *testing.T) {
	super := superprojectFixture(t)

	subs, err := DiscoverSubmodules(super)
	if err != nil {
		t.Fatalf("DiscoverSubmodules: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 submodule, got %d (%+v)", len(subs), subs)
	}
	if subs[0].Path != "vendor/lib" || !subs[0].Initialized {
		t.Errorf("unexpected submodule: %+v", subs[0])
	}
	if got, want := subs[0].AbsPath(super), filepath.Join(super, "vendor", "lib"); got != want {
		t.Errorf("AbsPath = %q, want %q", got, want)
	}
	if subs[0].Scope() != "vendor/lib" {
		t.Errorf("Scope = %q", subs[0].Scope())
	}
}

// TestDiscoverSubmodules_NestedSubmodule proves discovery descends: a
// submodule that itself carries a submodule yields both roots, with the nested
// path spelled relative to the outermost superproject.
func TestDiscoverSubmodules_NestedSubmodule(t *testing.T) {
	requireGit(t)
	base := t.TempDir()
	leaf := initRepo(t, filepath.Join(base, "leaf"), map[string]string{"leaf.go": "package leaf\n"})
	mid := initRepo(t, filepath.Join(base, "mid"), map[string]string{"mid.go": "package mid\n"})
	addSubmodule(t, mid, leaf, "deps/leaf")
	super := initRepo(t, filepath.Join(base, "super"), map[string]string{"main.go": "package main\n"})
	addSubmodule(t, super, mid, "vendor/mid")
	// `submodule add` clones without recursing, so initialize the nested one.
	git(t, super, "submodule", "update", "--init", "--recursive")

	subs, err := DiscoverSubmodules(super)
	if err != nil {
		t.Fatalf("DiscoverSubmodules: %v", err)
	}
	var paths []string
	for _, s := range subs {
		paths = append(paths, s.Path)
	}
	want := []string{"vendor/mid", "vendor/mid/deps/leaf"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("nested discovery = %v, want %v", paths, want)
	}
}

// TestDiscoverSubmodules_NotAGitRepo: a non-repo directory yields an error
// rather than a false "no submodules here" answer.
func TestDiscoverSubmodules_NotAGitRepo(t *testing.T) {
	requireGit(t)
	if _, err := DiscoverSubmodules(t.TempDir()); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}

// TestEnumerateTrackedFiles_NotAGitRepo mirrors the discovery error path.
func TestEnumerateTrackedFiles_NotAGitRepo(t *testing.T) {
	requireGit(t)
	if _, err := EnumerateTrackedFiles(t.TempDir(), true); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}

// TestPlanWorkspace_RecursesIntoSubmodule: the default plan indexes both roots
// and records what a root-only enumeration would have seen.
func TestPlanWorkspace_RecursesIntoSubmodule(t *testing.T) {
	super := superprojectFixture(t)

	plan, err := PlanWorkspace(super, true)
	if err != nil {
		t.Fatalf("PlanWorkspace: %v", err)
	}
	if len(plan.Roots) != 2 {
		t.Fatalf("expected 2 roots, got %+v", plan.Roots)
	}
	if plan.Roots[0].Path != "." || plan.Roots[0].Scope != "" {
		t.Errorf("superproject root = %+v, want path=. and an empty scope", plan.Roots[0])
	}
	sub := plan.Roots[1]
	if sub.Path != "vendor/lib" || sub.Scope != "vendor/lib" || sub.Files != 3 {
		t.Errorf("submodule root = %+v, want vendor/lib with 3 files", sub)
	}
	if plan.RootOnlyFiles != 4 || plan.Files() != 7 {
		t.Errorf("RootOnlyFiles=%d Files=%d, want 4 and 7", plan.RootOnlyFiles, plan.Files())
	}
	if len(plan.Skipped) != 0 {
		t.Errorf("nothing should be skipped, got %+v", plan.Skipped)
	}
	if got := plan.Summary(); !strings.Contains(got, "vendor/lib: 3 files") {
		t.Errorf("Summary() = %q, want it to name the submodule and its file count", got)
	}
	if len(plan.Submodules()) != 1 {
		t.Errorf("Submodules() = %+v", plan.Submodules())
	}
}

// TestPlanWorkspace_OptOutRecordsExclusion: opting out never hides the
// submodule — it is reported as explicitly excluded.
func TestPlanWorkspace_OptOutRecordsExclusion(t *testing.T) {
	super := superprojectFixture(t)

	plan, err := PlanWorkspace(super, false)
	if err != nil {
		t.Fatalf("PlanWorkspace: %v", err)
	}
	if len(plan.Roots) != 1 || len(plan.Submodules()) != 0 {
		t.Fatalf("opt-out should leave a single root, got %+v", plan.Roots)
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0].Path != "vendor/lib" ||
		plan.Skipped[0].Reason != SkipReasonExcluded {
		t.Fatalf("expected vendor/lib recorded as excluded, got %+v", plan.Skipped)
	}
	if got := plan.Summary(); !strings.Contains(got, "vendor/lib: SKIPPED") {
		t.Errorf("Summary() = %q, want it to name the skipped submodule", got)
	}
}

// TestPlanWorkspace_UninitializedSubmoduleIsSkippedWithReason: a clone made
// without --recurse-submodules has an empty submodule directory. The plan must
// say so, with an actionable reason, instead of silently indexing nothing.
func TestPlanWorkspace_UninitializedSubmoduleIsSkippedWithReason(t *testing.T) {
	super := superprojectFixture(t)
	base := t.TempDir()
	clone := filepath.Join(base, "clone")
	git(t, base, "clone", "--quiet", filepath.ToSlash(super), clone)

	subs, err := DiscoverSubmodules(clone)
	if err != nil {
		t.Fatalf("DiscoverSubmodules: %v", err)
	}
	if len(subs) != 1 || subs[0].Initialized {
		t.Fatalf("expected one uninitialized submodule, got %+v", subs)
	}
	plan, err := PlanWorkspace(clone, true)
	if err != nil {
		t.Fatalf("PlanWorkspace: %v", err)
	}
	if len(plan.Roots) != 1 {
		t.Fatalf("an uninitialized submodule must not become an indexed root: %+v", plan.Roots)
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0].Reason != SkipReasonUninitialized {
		t.Fatalf("expected the uninitialized reason, got %+v", plan.Skipped)
	}
}

// TestPlanWorkspace_NoSubmodulesIsUnchanged: a plain repository keeps the
// single-root shape, so the fix costs nothing for the common case.
func TestPlanWorkspace_NoSubmodulesIsUnchanged(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "plain"), map[string]string{"a.go": "package a\n"})

	plan, err := PlanWorkspace(repo, true)
	if err != nil {
		t.Fatalf("PlanWorkspace: %v", err)
	}
	if len(plan.Roots) != 1 || plan.Roots[0].Path != "." || plan.Roots[0].Files != 1 {
		t.Fatalf("unexpected plan for a plain repo: %+v", plan)
	}
	if plan.Summary() != ".: 1 files" {
		t.Errorf("Summary() = %q", plan.Summary())
	}
}

// TestPlanWorkspace_NotAGitRepo surfaces the enumeration error to the caller.
func TestPlanWorkspace_NotAGitRepo(t *testing.T) {
	requireGit(t)
	if _, err := PlanWorkspace(t.TempDir(), true); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}

// TestPlanWorkspace_UnreadableSubmoduleIsSkippedWithReason: a submodule whose
// checkout git cannot read (a broken .git pointer) is reported as skipped with
// the underlying failure, never silently indexed as empty.
func TestPlanWorkspace_UnreadableSubmoduleIsSkippedWithReason(t *testing.T) {
	super := breakSubmoduleCheckout(t)

	plan, err := PlanWorkspace(super, true)
	if err != nil {
		t.Fatalf("PlanWorkspace: %v", err)
	}
	if len(plan.Roots) != 1 || plan.Roots[0].Path != "." {
		t.Fatalf("an unreadable submodule must not become a root: %+v", plan.Roots)
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "enumeration failed") {
		t.Fatalf("expected an enumeration-failure reason, got %+v", plan.Skipped)
	}
}

// TestDiscoverSubmodules_UnreadableNestedCheckout: discovery still reports a
// gitlink whose own index cannot be read — the parent has to know it exists.
func TestDiscoverSubmodules_UnreadableNestedCheckout(t *testing.T) {
	super := breakSubmoduleCheckout(t)

	subs, err := DiscoverSubmodules(super)
	if err != nil {
		t.Fatalf("DiscoverSubmodules: %v", err)
	}
	if len(subs) != 1 || subs[0].Path != "vendor/lib" || !subs[0].Initialized {
		t.Fatalf("expected the broken submodule to still be reported, got %+v", subs)
	}
}

// breakSubmoduleCheckout returns the superproject fixture with the submodule's
// .git pointer corrupted, so its working tree exists but git cannot read it.
func breakSubmoduleCheckout(t *testing.T) string {
	t.Helper()
	super := superprojectFixture(t)
	writeFiles(t, filepath.Join(super, "vendor", "lib"), map[string]string{".git": "not a git pointer\n"})
	return super
}

// TestParseStageEntry pins the `ls-files --stage -z` record parser, including
// the malformed records a truncated or empty read produces.
func TestParseStageEntry(t *testing.T) {
	cases := []struct {
		name       string
		entry      string
		mode, path string
		ok         bool
	}{
		{"gitlink", "160000 abc123 0\tvendor/lib", gitlinkMode, "vendor/lib", true},
		{"blob", "100644 def456 0\tmain.go", "100644", "main.go", true},
		{"path with spaces", "100644 def456 0\tdir/a b.go", "100644", "dir/a b.go", true},
		{"no tab", "160000 abc123 0 vendor/lib", "", "", false},
		{"no space in meta", "160000abc\tvendor/lib", "", "", false},
		{"empty path", "100644 def456 0\t", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, path, ok := parseStageEntry(tc.entry)
			if ok != tc.ok || mode != tc.mode || path != tc.path {
				t.Errorf("parseStageEntry(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.entry, mode, path, ok, tc.mode, tc.path, tc.ok)
			}
		})
	}
}

// TestSubmoduleInitialized covers both arms of the checkout probe.
func TestSubmoduleInitialized(t *testing.T) {
	dir := t.TempDir()
	if submoduleInitialized(dir) {
		t.Error("a directory with no .git must read as uninitialized")
	}
	writeFiles(t, dir, map[string]string{".git": "gitdir: ../.git/modules/lib\n"})
	if !submoduleInitialized(dir) {
		t.Error("a .git worktree pointer must read as initialized")
	}
}

// TestDiscoverSubmodules_DepthBound proves the recursion bound terminates the
// walk rather than following an unbounded chain.
func TestDiscoverSubmodules_DepthBound(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "r"), map[string]string{"a.go": "package a\n"})
	subs, err := discoverSubmodules(repo, "", maxSubmoduleDepth)
	if err != nil || subs != nil {
		t.Errorf("at the depth bound discovery must stop: subs=%+v err=%v", subs, err)
	}
}

// contains reports whether haystack holds needle.
func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
