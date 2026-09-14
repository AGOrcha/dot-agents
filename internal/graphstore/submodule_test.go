package graphstore

import (
	"path/filepath"
	"sort"
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
	runGitFixture(t, super, "submodule", "update", "--init", "--recursive")

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
	runGitFixture(t, base, "clone", "--quiet", filepath.ToSlash(super), clone)

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

// TestDiscoverSubmodules_StagedGitlinkIsFound is why the index — not HEAD's
// tree and not .gitmodules — is the ground truth. A gitlink that is STAGED but
// not yet committed, and one whose .gitmodules entry was lost, are both real
// states after a partial merge, and both are roots whose files a plain
// enumeration silently skips.
func TestDiscoverSubmodules_StagedGitlinkIsFound(t *testing.T) {
	requireGit(t)
	base := t.TempDir()
	child := initRepo(t, filepath.Join(base, "child"), map[string]string{"c.go": "package c\n"})
	super := initRepo(t, filepath.Join(base, "super"), map[string]string{"main.go": "package main\n"})
	// `submodule add` stages the gitlink and .gitmodules; deliberately do NOT
	// commit, and drop .gitmodules so only the index knows this is a submodule.
	runGitFixture(t, super, "submodule", "add", "--quiet", filepath.ToSlash(child), "vendor/child")
	runGitFixture(t, super, "rm", "--quiet", "--cached", ".gitmodules")

	subs, err := DiscoverSubmodules(super)
	if err != nil {
		t.Fatalf("DiscoverSubmodules: %v", err)
	}
	if len(subs) != 1 || subs[0].Path != "vendor/child" || !subs[0].Initialized {
		t.Fatalf("a staged, .gitmodules-less gitlink must still be discovered, got %+v", subs)
	}

	files, err := EnumerateTrackedFiles(super, true)
	if err != nil {
		t.Fatalf("recursive enumeration: %v", err)
	}
	if !contains(files, "vendor/child/c.go") {
		t.Errorf("recursive enumeration missed the staged submodule's file, got %v", files)
	}
}

// TestEnumerateTrackedFiles_IsLexicallySorted pins the output order. Callers
// diff root-only against recursive counts and render the result to operators,
// so a stable order is part of the contract, not an accident of index layout.
func TestEnumerateTrackedFiles_IsLexicallySorted(t *testing.T) {
	super := superprojectFixture(t)
	for _, recurse := range []bool{false, true} {
		files, err := EnumerateTrackedFiles(super, recurse)
		if err != nil {
			t.Fatalf("enumeration (recurse=%v): %v", recurse, err)
		}
		if !sort.StringsAreSorted(files) {
			t.Errorf("enumeration (recurse=%v) is not sorted: %v", recurse, files)
		}
	}
}

// TestEnumerateTrackedFiles_UninitializedSubmoduleKeepsItsGitlink: there is no
// checkout to read, so the recursive walk reports the gitlink entry itself
// rather than dropping it and understating what the repository tracks.
func TestEnumerateTrackedFiles_UninitializedSubmoduleKeepsItsGitlink(t *testing.T) {
	super := superprojectFixture(t)
	base := t.TempDir()
	clone := filepath.Join(base, "clone")
	runGitFixture(t, base, "clone", "--quiet", filepath.ToSlash(super), clone)

	files, err := EnumerateTrackedFiles(clone, true)
	if err != nil {
		t.Fatalf("recursive enumeration of an uninitialized clone: %v", err)
	}
	if !contains(files, "vendor/lib") {
		t.Errorf("the uninitialized gitlink must still be listed, got %v", files)
	}
	if contains(files, "vendor/lib/lib.go") {
		t.Errorf("an uninitialized submodule has no files to list, got %v", files)
	}
}

// TestEnumerateTrackedFiles_UnreadableSubmoduleFails: an initialized submodule
// whose repository cannot be opened is an error, never a silent zero — the
// whole point of this file is that a build must not claim coverage it lacks.
func TestEnumerateTrackedFiles_UnreadableSubmoduleFails(t *testing.T) {
	super := breakSubmoduleCheckout(t)
	_, err := EnumerateTrackedFiles(super, true)
	if err == nil || !strings.Contains(err.Error(), "vendor/lib") {
		t.Fatalf("expected an error naming the unreadable submodule, got %v", err)
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

// TestContainedIn rejects the gitlink paths that would escape the checkout.
// Submodule paths come out of a repository's own index — untrusted input for a
// clone of someone else's superproject — and are used as a working directory,
// a --repo argument, and a database ATTACH target.
func TestContainedIn(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"plain child", "vendor/lib", true},
		{"nested child", "a/b/c", true},
		{"parent escape", "../evil", false},
		{"deep escape", "vendor/../../evil", false},
		{"absolute", filepath.Join(string(filepath.Separator), "etc", "passwd"), false},
		// Slash-rooted is how a git index would spell an escaping path. On
		// Windows it is rooted rather than absolute, which IsAbs alone misses.
		{"slash rooted", "/etc/passwd", false},
		// The repository itself is not a submodule OF itself: walking `.` (or
		// the empty path Clean reports as `.`) would build and merge the
		// superproject a second time under a scope.
		{"self", ".", false},
		{"empty", "", false},
		{"self via traversal", "vendor/..", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containedIn(tc.path); got != tc.want {
				t.Errorf("containedIn(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
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
