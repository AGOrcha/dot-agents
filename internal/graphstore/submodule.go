// Package graphstore — submodule-aware workspace enumeration.
//
// A submodule appears in its superproject's index as a single gitlink entry,
// never as the files inside it. A code-graph build that enumerates the index
// without descending therefore indexes 0 of a submodule's files while still
// reporting success — the failure mode recorded in the
// kg-code-graph-submodule-blindness proposal (47 nodes / 2 files indexed where
// reality was 5946 nodes / 885 files, status READY). This file owns the
// enumeration side of the fix: discover the gitlink roots, enumerate each one,
// and report what was (and was not) covered so a build can never claim
// completeness it does not have.
//
// All Git access is in-process (gitnative.go); no `git` subprocess is spawned.
package graphstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxSubmoduleDepth bounds nested-submodule recursion. Real superprojects nest
// one or two levels; the bound exists so a pathological (or hand-crafted)
// checkout cannot drive an unbounded walk.
const maxSubmoduleDepth = 8

// scopeSeparator joins a submodule scope to a qualified name when merging a
// submodule graph into its superproject. It matches the `::` qualified-name
// separator CRG already uses, so a scoped name stays readable.
const scopeSeparator = "::"

// Submodule is one gitlink root discovered under a superproject.
type Submodule struct {
	// Path is the submodule path relative to the superproject root, always
	// slash-separated (git's own spelling, stable across platforms).
	Path string `json:"path"`
	// Initialized reports whether the submodule working tree is present — an
	// uninitialized submodule is an empty directory with nothing to index.
	Initialized bool `json:"initialized"`
}

// AbsPath returns the submodule's absolute location under repoRoot.
func (s Submodule) AbsPath(repoRoot string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(s.Path))
}

// Scope returns the qualified-name namespace for symbols from this submodule.
// Scoping is what keeps `Button` in one repo from colliding with `Button` in
// another once their graphs share a database.
func (s Submodule) Scope() string { return s.Path }

// DiscoverSubmodules returns every gitlink under repoRoot, including nested
// ones (paths relative to repoRoot), sorted by path.
//
// It reads the git index rather than .gitmodules or HEAD's tree: the index is
// the ground truth for what is actually a submodule in this checkout. A
// STAGED gitlink, and a gitlink with no .gitmodules entry (both real states
// after a partial merge), are still roots whose files a plain enumeration
// would miss.
func DiscoverSubmodules(repoRoot string) ([]Submodule, error) {
	subs, err := discoverSubmodules(repoRoot, "", 0)
	if err != nil {
		return nil, err
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].Path < subs[j].Path })
	return subs, nil
}

// discoverSubmodules is the depth-bounded recursive worker behind
// DiscoverSubmodules. prefix is the slash path of dir relative to the
// outermost superproject ("" at the top level).
func discoverSubmodules(dir, prefix string, depth int) ([]Submodule, error) {
	if depth >= maxSubmoduleDepth {
		return nil, nil
	}
	paths, err := gitlinkPaths(dir)
	if err != nil {
		return nil, err
	}
	var out []Submodule
	for _, p := range paths {
		full := p
		if prefix != "" {
			full = prefix + "/" + p
		}
		sub := Submodule{Path: full, Initialized: submoduleInitialized(filepath.Join(dir, filepath.FromSlash(p)))}
		out = append(out, sub)
		if !sub.Initialized {
			continue
		}
		nested, nestedErr := discoverSubmodules(filepath.Join(dir, filepath.FromSlash(p)), full, depth+1)
		if nestedErr != nil {
			// A nested checkout whose index cannot be read is reported as a
			// root with no children rather than failing the whole discovery:
			// the parent still needs to know the gitlink exists.
			continue
		}
		out = append(out, nested...)
	}
	return out, nil
}

// containedIn reports whether an index-supplied submodule path names a real
// location STRICTLY INSIDE the repository it came from.
//
// A gitlink path is untrusted input when the superproject was cloned from
// elsewhere, and it becomes a build root, a CRG `--repo` argument, and a
// database ATTACH target. An escaping path (`../`, absolute) is dropped rather
// than walked, and so are the paths that resolve to the repository ITSELF
// (empty, `.`): a submodule rooted at its own superproject would be walked,
// built, and merged as if it were a separate repository.
func containedIn(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." {
		return false // also covers the empty path, which Clean reports as "."
	}
	// Absolute, rooted, and volume-qualified paths all leave the checkout.
	// Windows needs all three tests: `\etc\passwd` is rooted but not absolute
	// there, and `C:evil` is drive-relative with no separator at all.
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || os.IsPathSeparator(clean[0]) {
		return false
	}
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// submoduleInitialized reports whether a submodule working tree is checked
// out. A cloned-but-uninitialized submodule is an empty directory; an
// initialized one carries a .git file (worktree pointer) or directory.
func submoduleInitialized(abs string) bool {
	if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
		return true
	}
	return false
}

// WorkspaceRoot is one repository a build indexes: the superproject itself or
// one of its submodules.
type WorkspaceRoot struct {
	// Path is the root's path relative to the superproject ("." for the
	// superproject itself), slash-separated.
	Path string `json:"path"`
	// AbsPath is the root's absolute location on disk.
	AbsPath string `json:"-"`
	// Scope is the qualified-name namespace applied to this root's symbols
	// when they are merged into the superproject graph ("" for the
	// superproject, whose names are stored unscoped).
	Scope string `json:"scope,omitempty"`
	// Files is the number of tracked files enumerated for this root.
	Files int `json:"files"`
}

// SkippedRoot is a submodule a build detected but did not index, with the
// reason stated. A skipped root is never silent: it is carried into the build
// report and the readiness status.
type SkippedRoot struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Reasons a detected submodule is not indexed. They are user-facing strings —
// the operator has to be able to act on them.
const (
	SkipReasonUninitialized = "not initialized (run: git submodule update --init --recursive)"
	SkipReasonExcluded      = "excluded by --no-recurse-submodules"
)

// WorkspacePlan is what a build intends to index, resolved before any work
// happens. It is the honest-readiness input: every root that will be indexed
// and every submodule that will not, with counts.
type WorkspacePlan struct {
	// Roots are the repositories that will be indexed, superproject first.
	Roots []WorkspaceRoot `json:"roots"`
	// Skipped are submodules detected but not indexed, each with a reason.
	Skipped []SkippedRoot `json:"skipped,omitempty"`
	// RootOnlyFiles is the file count a non-recursive enumeration would have
	// seen — the pre-fix number. Compare against Files() to see the blind spot.
	RootOnlyFiles int `json:"root_only_files"`
}

// Submodules returns the plan's submodule roots (everything but the
// superproject).
func (p WorkspacePlan) Submodules() []WorkspaceRoot {
	if len(p.Roots) <= 1 {
		return nil
	}
	return p.Roots[1:]
}

// Files is the total number of tracked files across every indexed root.
func (p WorkspacePlan) Files() int {
	total := 0
	for _, r := range p.Roots {
		total += r.Files
	}
	return total
}

// Summary renders the per-root enumeration as a single operator-readable
// line, naming skipped submodules explicitly.
func (p WorkspacePlan) Summary() string {
	parts := make([]string, 0, len(p.Roots)+len(p.Skipped))
	for _, r := range p.Roots {
		parts = append(parts, fmt.Sprintf("%s: %d files", r.Path, r.Files))
	}
	for _, s := range p.Skipped {
		parts = append(parts, fmt.Sprintf("%s: SKIPPED (%s)", s.Path, s.Reason))
	}
	return strings.Join(parts, "; ")
}

// PlanWorkspace resolves the roots a build over repoRoot should index.
//
// With recurseSubmodules true (the default everywhere) every initialized
// submodule becomes an indexed root; an uninitialized one is recorded as
// skipped with the reason, never dropped. With it false every detected
// submodule is recorded as explicitly excluded — the opt-out is loud, not
// blind.
func PlanWorkspace(repoRoot string, recurseSubmodules bool) (WorkspacePlan, error) {
	subs, err := DiscoverSubmodules(repoRoot)
	if err != nil {
		return WorkspacePlan{}, err
	}
	plan := WorkspacePlan{}
	// The superproject is just the first root: every root is enumerated the
	// same way, so a root that cannot be read is reported the same way too.
	candidates := []WorkspaceRoot{{Path: ".", AbsPath: repoRoot}}
	for _, sub := range subs {
		switch {
		case !recurseSubmodules:
			plan.Skipped = append(plan.Skipped, SkippedRoot{Path: sub.Path, Reason: SkipReasonExcluded})
		case !sub.Initialized:
			plan.Skipped = append(plan.Skipped, SkippedRoot{Path: sub.Path, Reason: SkipReasonUninitialized})
		default:
			candidates = append(candidates, WorkspaceRoot{
				Path:    sub.Path,
				AbsPath: sub.AbsPath(repoRoot),
				Scope:   sub.Scope(),
			})
		}
	}
	for _, root := range candidates {
		files, ferr := EnumerateTrackedFiles(root.AbsPath, false)
		if ferr != nil {
			plan.Skipped = append(plan.Skipped, SkippedRoot{Path: root.Path, Reason: "enumeration failed: " + ferr.Error()})
			continue
		}
		root.Files = len(files)
		if root.Path == "." {
			plan.RootOnlyFiles = root.Files
		}
		plan.Roots = append(plan.Roots, root)
	}
	return plan, nil
}
