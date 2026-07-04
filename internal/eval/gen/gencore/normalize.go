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
// (under EITHER OS convention — see isAbsAnyOS) is relativized against repoRoot;
// an already-relative p is cleaned in place. It errors on an empty path, on an
// absolute path with no known repoRoot, on an absolute path from a different OS
// than the runtime (which cannot be reduced against a runtime-OS repoRoot), and
// on any path that resolves outside repoRoot — the caller must never emit such a
// path into a spec.
func relativizePath(p, repoRoot string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("seed file path is empty")
	}
	if !isAbsAnyOS(p) {
		return cleanRelative(filepath.ToSlash(p), p)
	}
	if strings.TrimSpace(repoRoot) == "" {
		return "", fmt.Errorf("cannot relativize absolute seed path %q: repository root is unknown", p)
	}
	// Only the runtime OS's absolute form can be relativized against repoRoot
	// (itself a runtime-OS path). A foreign-OS absolute path (e.g. a Windows
	// "C:\\..." seed on a Unix host, or a "/..." seed on Windows) must fail loudly
	// rather than pass through uncleaned.
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("seed path %q is an absolute path from a different OS and cannot be relativized against repo root %q", p, repoRoot)
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
// emitted spec. normalizeSeed already relativizes every path and cleans every
// symbol, so a hit here signals a normalization gap (a new emission site that
// bypassed it), not bad KG data — fail loudly rather than ship a spec whose
// commands, artifacts, or prompt point outside the eval sandbox. It backs every
// path-bearing field the PR advertises: the artifact paths and verification
// commands (path tokens) plus the prompt, task id, and recorded seed symbol
// (free-text/composite fields).
func assertSpecRelative(spec *eval.TaskSpec) error {
	if err := assertArtifactsRelative(spec.SolutionArtifacts); err != nil {
		return err
	}
	if err := assertCommandsRelative(spec.Verification); err != nil {
		return err
	}
	return assertTextFieldsRelative(spec)
}

// assertArtifactsRelative rejects an absolute solution-artifact path.
func assertArtifactsRelative(arts []eval.SolutionArtifact) error {
	for _, a := range arts {
		if hasAbsPathToken(a.Path) {
			return fmt.Errorf("solution artifact path %q is not repo-relative", a.Path)
		}
	}
	return nil
}

// assertCommandsRelative rejects an absolute token in either verification command.
func assertCommandsRelative(v eval.Verification) error {
	cmds := []struct {
		name string
		toks []string
	}{
		{"build", v.BuildCmd},
		{"test", v.TestCmd},
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

// assertTextFieldsRelative rejects an absolute-path residue in the free-text /
// composite spec fields: the prompt, the task id, and the recorded seed symbol.
func assertTextFieldsRelative(spec *eval.TaskSpec) error {
	fields := []struct {
		name string
		val  string
	}{
		{"prompt", spec.Prompt},
		{"task id", spec.TaskID},
		{"seed symbol", seedSymbolOf(spec)},
	}
	for _, f := range fields {
		if hasAbsPathResidue(f.val) {
			return fmt.Errorf("%s carries an absolute-path residue: %q", f.name, f.val)
		}
	}
	return nil
}

// seedSymbolOf returns the recorded seed symbol, tolerating an absent KGQuery.
func seedSymbolOf(spec *eval.TaskSpec) string {
	if spec.GeneratedFrom.KGQuery == nil {
		return ""
	}
	return spec.GeneratedFrom.KGQuery.SeedSymbol
}

// hasAbsPathToken reports whether a single emitted path token is not
// repo-relative: it is absolute under either OS convention (see isAbsAnyOS),
// carries a backslash (an un-normalized Windows separator, never present in a
// repo-relative slash path), or has the "//" glue a `"./" + "/abs/..."`
// concatenation produces. A well-formed package pattern, pytest dir, or test
// glob has none of these.
func hasAbsPathToken(tok string) bool {
	return isAbsAnyOS(tok) || strings.ContainsRune(tok, '\\') || strings.Contains(tok, "//")
}

// hasAbsPathResidue reports whether a free-text or composite field carries an
// absolute-path residue. Beyond the per-token checks (any whitespace/backtick-
// delimited token that hasAbsPathToken flags), the code-review-graph "::"
// separator must never survive normalization anywhere in the field, even glued
// onto an otherwise-relative token.
func hasAbsPathResidue(s string) bool {
	if strings.Contains(s, crgSymbolSep) {
		return true
	}
	for _, tok := range strings.FieldsFunc(s, isPathBoundary) {
		if hasAbsPathToken(tok) {
			return true
		}
	}
	return false
}

// isPathBoundary reports whether r delimits a path token inside a free-text
// field (prompt, task id, seed symbol). Paths are backtick-quoted in prompts and
// otherwise space/punctuation-separated, so these boundaries isolate a candidate
// path for hasAbsPathToken to inspect.
func isPathBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '`', ',', '(', ')':
		return true
	default:
		return false
	}
}

// isAbsAnyOS reports whether p is an absolute path under EITHER the Unix or the
// Windows convention, regardless of the runtime OS. filepath.IsAbs only
// recognizes the running OS's form, but a knowledge graph can be ingested on a
// different OS than the one da eval runs on, so a foreign-OS absolute path must
// still be caught. It matches: a leading "/" (Unix root or POSIX "//host" UNC),
// a leading "\" (Windows rooted or "\\host"/"\\?\" UNC/device), or a drive-letter
// prefix ("C:\" or "C:/").
func isAbsAnyOS(p string) bool {
	if p == "" {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	return len(p) >= 3 && isASCIILetter(p[0]) && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

// isASCIILetter reports whether b is an ASCII letter (a Windows drive letter).
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
