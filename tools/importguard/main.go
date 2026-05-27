// Command importguard enforces cross-leaf isolation between the four
// commands/internal/* composition leaves established by plan
// root-command-decomposition (t13a + t13a-pre + t13b + t15).
//
// Background: outsider-prevention is now handled by Go's built-in
// internal/ package convention — only code rooted at or below
// commands/ may import commands/internal/lifecycle (etc.). The Go
// compiler enforces that for us. What the compiler does NOT enforce is
// sibling isolation: commands/internal/lifecycle is free, as far as
// Go is concerned, to import commands/internal/mcp. This tool exists
// solely to forbid those cross-leaf edges.
//
// Contract:
//
//   - commands/internal/lifecycle, commands/internal/mcp,
//     commands/internal/settings, commands/internal/rules are leaf
//     composition targets. They MUST NOT import each other.
//
// Test files inside each leaf share the same import budget as their
// owning package — the policy is applied at the importing-package
// level, not the file level.
//
// Usage: importguard [packages...]
// Defaults to "./..." when no package patterns are supplied. The tool
// exits non-zero (and prints the violation list) the moment any
// forbidden cross-leaf edge appears, which is what the CI job in
// .github/workflows/test.yml keys off.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const modulePath = "github.com/NikashPrakash/dot-agents"

// guardedSubpackages enumerates the four commands/internal/* leaves
// whose cross-edges the guard polices. Each entry is the canonical
// package import path (no trailing slash). The order here is only used
// for deterministic rendering — lookups are by string match.
var guardedSubpackages = []string{
	modulePath + "/commands/internal/lifecycle",
	modulePath + "/commands/internal/mcp",
	modulePath + "/commands/internal/settings",
	modulePath + "/commands/internal/rules",
}

// violation captures one disallowed import edge for reporting. We
// surface the importing leaf and the target leaf so the failure log
// points straight at the offending source file once a developer runs
// `go list -f '{{.GoFiles}}' <importer>`.
type violation struct {
	importer string
	target   string
	reason   string
}

func main() {
	os.Exit(mainRun(os.Args[1:], os.Stderr, run))
}

// runFunc is the package-loading hook mainRun calls. Threading it as a
// parameter (instead of calling run directly) is the seam tests use to
// drive every exit-code branch — load failure (exit 2), violations found
// (exit 1), and clean run (exit 0) — without invoking the real Go
// toolchain. The default wiring in main passes the production run.
type runFunc func(patterns []string) ([]violation, error)

// mainRun is main's testable body. It parses args, invokes load, prints
// any violations, and returns the process exit code. Keeping it pure
// (no os.Exit, no global state mutation beyond the FlagSet it owns) lets
// the per-branch tests assert exit codes and stderr content directly.
func mainRun(args []string, stderr io.Writer, load runFunc) int {
	fs := flag.NewFlagSet("importguard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr,
			"usage: importguard [packages...]\n"+
				"  default packages: ./...\n"+
				"  exits non-zero on any cross-leaf import edge between\n"+
				"  commands/internal/{lifecycle,mcp,settings,rules}.\n")
	}
	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already wrote the usage to stderr;
		// exit code 2 mirrors flag.ExitOnError's behavior on bad args.
		return 2
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	violations, err := load(patterns)
	if err != nil {
		fmt.Fprintf(stderr, "importguard: %v\n", err)
		return 2
	}
	if len(violations) > 0 {
		reportViolations(stderr, violations)
		return 1
	}
	return 0
}

// run loads the requested packages and returns every disallowed import
// edge it finds. Returning a slice (instead of failing on first hit) keeps
// the CI output actionable when several files drift at once. The load
// step is delegated to a package-level var so tests can inject synthetic
// graphs without spinning up the real Go toolchain.
func run(patterns []string) ([]violation, error) {
	pkgs, err := loadPackages(patterns)
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("package load reported errors (see above)")
	}
	return checkPackages(pkgs), nil
}

// loadPackages is a var (not a const func) so tests can swap in a fake
// loader. Production callers get the real packages.Load behind a config
// that requests only the graph signal we need.
var loadPackages = func(patterns []string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		// NeedImports is the only graph signal required; NeedName +
		// NeedFiles make package errors and file diagnostics readable
		// without re-loading.
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports,
		// Tests:false keeps the import set production-only. The policy
		// is intentionally evaluated against the build graph, not the
		// test graph: tests inside an allowed package inherit the
		// package's import budget, and out-of-package tests are loaded
		// as their own package which carries no leaf identity and would
		// always pass.
		Tests: false,
	}
	return packages.Load(cfg, patterns...)
}

// checkPackages inspects every loaded package's direct imports against the
// policy and accumulates violations. The traversal stays shallow on
// purpose: a direct edge from one leaf to another is what the policy
// regulates, and packages.Load(./...) loads every leaf anyway.
func checkPackages(pkgs []*packages.Package) []violation {
	var out []violation
	for _, p := range pkgs {
		// Skip placeholder packages produced by load errors; their
		// imports map is unreliable and the error has already been
		// reported by packages.PrintErrors.
		if p == nil || p.PkgPath == "" || len(p.Errors) > 0 {
			continue
		}
		// Sort import paths so the violation list is stable across
		// runs (packages.Package.Imports is a map).
		importPaths := make([]string, 0, len(p.Imports))
		for ip := range p.Imports {
			importPaths = append(importPaths, ip)
		}
		sort.Strings(importPaths)
		for _, ip := range importPaths {
			if v, bad := classify(p.PkgPath, ip); bad {
				out = append(out, v)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].importer != out[j].importer {
			return out[i].importer < out[j].importer
		}
		return out[i].target < out[j].target
	})
	return out
}

// classify is the single decision point: given an importer package path
// and one of its direct imports, return whether the edge violates the
// cross-leaf rule. An edge is forbidden iff both endpoints sit in
// different guarded leaves. Same-leaf edges (including internal
// helpers under the leaf's subtree) and edges where either endpoint is
// not a guarded leaf at all (root commands package, cmd entrypoint,
// stdlib, third-party) are all allowed.
func classify(importer, target string) (violation, bool) {
	targetLeaf := guardedSubpackageFor(target)
	if targetLeaf == "" {
		return violation{}, false // target is not a guarded leaf
	}
	importerLeaf := guardedSubpackageFor(importer)
	if importerLeaf == "" {
		// Outsider importers are now blocked by Go's internal/ rule at
		// compile time; if one somehow reaches this code (e.g. the
		// rename was undone), let the compiler fail the build before
		// we do — return false to keep this tool single-purpose.
		return violation{}, false
	}
	if importerLeaf == targetLeaf {
		return violation{}, false // same-leaf edge is always fine
	}
	return violation{
		importer: importer,
		target:   target,
		reason: fmt.Sprintf("subpackage %s must not import sibling subpackage %s",
			trimModule(importerLeaf), trimModule(targetLeaf)),
	}, true
}

// guardedSubpackageFor returns the guarded leaf path that owns the given
// import path, or "" if the import is unrelated to the policy. Matching
// uses exact equality OR prefix-with-slash so a hypothetical sibling like
// commands/internal/lifecyclehelper is not folded into
// commands/internal/lifecycle's budget.
func guardedSubpackageFor(importPath string) string {
	for _, sub := range guardedSubpackages {
		if inSubpackage(importPath, sub) {
			return sub
		}
	}
	return ""
}

// inSubpackage reports whether candidate is sub itself or lives strictly
// beneath it. The trailing-slash guard prevents the common Go-path bug
// where HasPrefix("a/bc", "a/b") returns true.
func inSubpackage(candidate, sub string) bool {
	if candidate == sub {
		return true
	}
	return strings.HasPrefix(candidate, sub+"/")
}

// trimModule strips the module prefix off a package path so the CI log
// shows "commands/internal/lifecycle" instead of the full Go import path.
func trimModule(pkgPath string) string {
	return strings.TrimPrefix(pkgPath, modulePath+"/")
}

// reportViolations renders the failure list. Kept in main.go (not a
// helper package) because the tool has exactly one consumer.
func reportViolations(w io.Writer, vs []violation) {
	fmt.Fprintf(w, "importguard: %d disallowed cross-leaf import edge(s):\n", len(vs))
	for _, v := range vs {
		fmt.Fprintf(w, "  %s -> %s\n      %s\n",
			trimModule(v.importer), trimModule(v.target), v.reason)
	}
	fmt.Fprintf(w, "\nThis tool locks cross-leaf isolation between the\n"+
		"commands/internal/{lifecycle,mcp,settings,rules} composition\n"+
		"leaves. Outsider imports are blocked by Go's internal/ rule.\n"+
		"If the violation is intentional, update tools/importguard/main.go\n"+
		"and explain why in the commit.\n")
}
