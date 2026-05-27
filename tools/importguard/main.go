// Command importguard enforces the commands/* subpackage import boundary
// established by plan root-command-decomposition (t13a + t13a-pre + t13b).
//
// Contract (locked here so CI fails if it drifts):
//
//   - commands/lifecycle, commands/mcp, commands/settings, commands/rules
//     are leaf composition targets. They MUST NOT import each other.
//   - commands/mcp, commands/settings, commands/rules may only be imported
//     by the root commands package (commands/root.go and sibling files) or
//     by the cmd/dot-agents entrypoint.
//   - commands/lifecycle may only be imported by the root commands package,
//     by the cmd/dot-agents entrypoint, or by code inside the
//     commands/lifecycle subtree itself.
//
// Test files inside the root commands/ package and inside each subpackage
// share the same import budget as their owning package — the policy is
// applied at the importing-package level, not the file level.
//
// Usage: importguard [packages...]
// Defaults to "./..." when no package patterns are supplied. The tool
// exits non-zero (and prints the violation list) the moment any forbidden
// edge appears, which is what the CI job in .github/workflows/test.yml
// keys off.
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

// guardedSubpackages enumerates the four commands/* leaves whose import
// edges the guard polices. Each entry is the canonical package import path
// (no trailing slash). The order here is only used for deterministic
// rendering — lookups are by string match.
var guardedSubpackages = []string{
	modulePath + "/commands/lifecycle",
	modulePath + "/commands/mcp",
	modulePath + "/commands/settings",
	modulePath + "/commands/rules",
}

// allowedRootImporters is the closed set of importers that may pull in any
// guarded leaf from outside the leaf's own subtree. Everything else is
// forbidden. The root commands package owns composition (root.go wires the
// AddCommand calls); cmd/dot-agents is the binary entrypoint that depends
// on commands.NewRootCmd. Membership is an exact package-path match.
var allowedRootImporters = map[string]struct{}{
	modulePath + "/commands":       {},
	modulePath + "/cmd/dot-agents": {},
}

// violation captures one disallowed import edge for reporting. We surface
// the importing package and the target subpackage import path so the
// failure log points straight at the offending source file once a developer
// runs `go list -f '{{.GoFiles}}' <importer>`.
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
				"  exits non-zero on any disallowed import edge into\n"+
				"  commands/{lifecycle,mcp,settings,rules}.\n")
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
		// as their own package (e.g. commands_test) which carries no
		// allow-list entry and would always violate if checked here.
		Tests: false,
	}
	return packages.Load(cfg, patterns...)
}

// checkPackages inspects every loaded package's direct imports against the
// policy and accumulates violations. The traversal stays shallow on
// purpose: transitive dependencies are covered because every package that
// imports a guarded leaf transitively is itself loaded by packages.Load
// when given ./..., and a direct edge is what the policy regulates.
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
// policy. Splitting the rule out makes it cheap to table-test.
func classify(importer, target string) (violation, bool) {
	sub := guardedSubpackageFor(target)
	if sub == "" {
		return violation{}, false // target is not a guarded leaf
	}
	// Same-subtree imports are always allowed: a file under
	// commands/lifecycle/ may freely import commands/lifecycle/internal/x
	// or its own root path. inSubpackage covers both equality and
	// prefix-with-slash so commands/lifecyclextra never matches
	// commands/lifecycle.
	if inSubpackage(importer, sub) {
		return violation{}, false
	}
	if _, ok := allowedRootImporters[importer]; ok {
		return violation{}, false
	}
	return violation{
		importer: importer,
		target:   target,
		reason:   reasonFor(importer, sub),
	}, true
}

// guardedSubpackageFor returns the guarded leaf path that owns the given
// import path, or "" if the import is unrelated to the policy. Matching
// uses exact equality OR prefix-with-slash so a hypothetical sibling like
// commands/lifecyclehelper is not folded into commands/lifecycle's budget.
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

// reasonFor produces a short, human-actionable explanation that names the
// rule each violation breaks. Cross-leaf edges (mcp -> settings) and
// outsider edges (some internal/... -> commands/lifecycle) read
// differently, so we branch on the importer.
func reasonFor(importer, sub string) string {
	if other := guardedSubpackageFor(importer); other != "" && other != sub {
		return fmt.Sprintf("subpackage %s must not import sibling subpackage %s",
			trimModule(other), trimModule(sub))
	}
	return fmt.Sprintf("package %s is not in the allowed-importer set for %s "+
		"(allowed: commands, cmd/dot-agents, %s subtree)",
		trimModule(importer), trimModule(sub), trimModule(sub))
}

// trimModule strips the module prefix off a package path so the CI log
// shows "commands/lifecycle" instead of the full Go import path.
func trimModule(pkgPath string) string {
	return strings.TrimPrefix(pkgPath, modulePath+"/")
}

// reportViolations renders the failure list. Kept in main.go (not a
// helper package) because the tool has exactly one consumer.
func reportViolations(w io.Writer, vs []violation) {
	fmt.Fprintf(w, "importguard: %d disallowed import edge(s):\n", len(vs))
	for _, v := range vs {
		fmt.Fprintf(w, "  %s -> %s\n      %s\n",
			trimModule(v.importer), trimModule(v.target), v.reason)
	}
	fmt.Fprintf(w, "\nThis tool locks the commands/* subpackage boundary set\n"+
		"by plan root-command-decomposition. If the violation is intentional,\n"+
		"update tools/importguard/main.go and explain why in the commit.\n")
}
