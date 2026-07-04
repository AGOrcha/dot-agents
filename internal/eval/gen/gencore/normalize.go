package gencore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// crgSymbolSep is the "<file>::<name>" separator the code-review-graph ingestion
// stamps into a node's qualified name. graphstore's makeQualified
// (internal/graphstore/sqlite.go) falls back to `FilePath + "::" + Name` for any
// node without a parent scope, and code-review-graph nodes carry no parent — so
// their qualified names are "<absolute-file-path>::<decl>". The native
// graphstore convention is "pkg.Symbol", which never contains this separator.
const crgSymbolSep = "::"

// resolveRepoRoot returns the repository root the generator relativizes KG file
// paths against. It mirrors commands/eval resolveRepoDir's default (the process
// working directory), which is the repository `da eval gen` is invoked from and
// the root the KG was built against. An unresolvable cwd yields "", which only
// affects absolute-path normalization (relative KG paths never need a root).
func resolveRepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// normalizeSeed rewrites a seed's display- and emission-facing paths and symbol
// names into a repo-relative, ingestion-independent form before spec assembly.
// It is the single normalization chokepoint every language generator inherits
// (they all synthesize through gencore), so the fix lives here once rather than
// in each per-language adapter.
//
// It converts:
//   - r.seed.FilePath  -> a repo-relative slash path (relativized against
//     repoRoot when absolute; passed through, cleaned, when already relative).
//   - r.seed.QualifiedName and every neighborhood name -> the clean decl name,
//     stripping the code-review-graph "<abs-file>::" prefix so no absolute path
//     leaks into a prompt, task id, or the recorded seed symbol.
//
// It fails with a clear error rather than emit garbage when an absolute KG path
// cannot be reduced to a path inside repoRoot (e.g. the KG was built for a
// different checkout): the alternative is a spec whose package pattern and
// prompt point outside the eval sandbox.
func normalizeSeed(r seedResult, repoRoot string) (seedResult, error) {
	rel, err := relativizePath(r.seed.FilePath, repoRoot)
	if err != nil {
		return seedResult{}, err
	}
	r.seed.FilePath = rel
	r.seed.QualifiedName = cleanSymbol(r.seed.QualifiedName)
	r.nbhd = normalizeNeighborhood(r.nbhd)
	return r, nil
}

// relativizePath returns p as a clean, repo-relative slash path. An absolute p
// is relativized against repoRoot; an already-relative p is cleaned in place.
// It errors on an empty path, on an absolute path with no known repoRoot, and
// on any path that resolves outside repoRoot (a leading ".." after
// relativization) — the caller must never emit such a path into a spec.
func relativizePath(p, repoRoot string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("seed file path is empty")
	}
	if !filepath.IsAbs(p) {
		return cleanRelative(filepath.ToSlash(p), p)
	}
	if strings.TrimSpace(repoRoot) == "" {
		return "", fmt.Errorf("cannot relativize absolute seed path %q: repository root is unknown", p)
	}
	rel, err := filepath.Rel(repoRoot, p)
	if err != nil {
		return "", fmt.Errorf("relativize seed path %q against repo root %q: %w", p, repoRoot, err)
	}
	return cleanRelative(filepath.ToSlash(rel), p)
}

// cleanRelative slash-normalizes an already-relative path and rejects one that
// escapes the repo root (".." or a "../"-prefixed result). orig is the original
// KG path, reported in the error so the diagnostic names the offending input.
func cleanRelative(slashPath, orig string) (string, error) {
	cleaned := filepath.ToSlash(filepath.Clean(slashPath))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("seed path %q resolves outside the repository root", orig)
	}
	return cleaned, nil
}

// cleanSymbol returns the usable decl name from a KG qualified name. A
// code-review-graph name ("<abs-file>::Decl") collapses to the segment after the
// last separator ("Decl"); a native name ("pkg/foo.Bar") is returned unchanged.
func cleanSymbol(qn string) string {
	if i := strings.LastIndex(qn, crgSymbolSep); i >= 0 {
		return qn[i+len(crgSymbolSep):]
	}
	return qn
}

// normalizeNeighborhood returns a copy of nbhd whose root and node qualified
// names are cleaned via cleanSymbol, so prompt neighbor lists show clean decl
// names for code-review-graph data instead of "<abs-file>::name" strings. Only
// the display names are rewritten; counts and edges are unaffected.
func normalizeNeighborhood(nbhd kgquery.Neighborhood) kgquery.Neighborhood {
	nbhd.Root.QualifiedName = cleanSymbol(nbhd.Root.QualifiedName)
	nodes := make([]graphstore.GraphNode, len(nbhd.Nodes))
	for i, n := range nbhd.Nodes {
		n.QualifiedName = cleanSymbol(n.QualifiedName)
		nodes[i] = n
	}
	nbhd.Nodes = nodes
	return nbhd
}

// assertSpecRelative is the final tripwire that no absolute path escaped into the
// emitted spec's paths or verification commands. normalizeSeed already
// relativizes every path, so a hit here signals a normalization gap (a new
// emission site that bypassed it), not bad KG data — fail loudly rather than
// ship a spec whose commands or artifacts point outside the eval sandbox.
func assertSpecRelative(spec *eval.TaskSpec) error {
	for _, a := range spec.SolutionArtifacts {
		if hasAbsPathToken(a.Path) {
			return fmt.Errorf("solution artifact path %q is not repo-relative", a.Path)
		}
	}
	cmds := []struct {
		name string
		toks []string
	}{
		{"build", spec.Verification.BuildCmd},
		{"test", spec.Verification.TestCmd},
	}
	for _, c := range cmds {
		for _, tok := range c.toks {
			if hasAbsPathToken(tok) {
				return fmt.Errorf("%s command token %q is not repo-relative", c.name, tok)
			}
		}
	}
	return nil
}

// hasAbsPathToken reports whether tok embeds an absolute filesystem path: either
// tok is itself absolute, or it is a well-formed pattern with an absolute path
// glued on (e.g. Go's `"./" + "/abs/dir/..."` yields the tell-tale "//"). A
// well-formed repo-relative package pattern, pytest dir, or test glob has
// neither.
func hasAbsPathToken(tok string) bool {
	return filepath.IsAbs(tok) || strings.Contains(filepath.ToSlash(tok), "//")
}
