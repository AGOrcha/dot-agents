package crgbehavior

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrOutsideRoot reports a bridge-stored path that does not live under the
// graph root. Such a path CANNOT be normalized into the comparison id space, so
// it is rejected rather than rewritten: a substring rewrite would silently
// invent an id and make a divergence look like agreement.
var ErrOutsideRoot = errors.New("crgbehavior: path lies outside the graph root")

// qualifiedSep is the separator upstream puts between a symbol's file path and
// its symbol part in `nodes.qualified_name` ("pkg/a.go::Entry").
const qualifiedSep = "::"

// Normalizer rewrites the bridge's stored paths and qualified names into the
// repo-relative, slash-separated spelling both sides of the gate key on.
//
// The rewrite is a CLEAN PREFIX transformation, never a substring replace:
//
//   - separators are folded to "/" and the value is path.Clean'ed, so
//     "/repo//pkg/./a.go" and "/repo/pkg/a.go" normalize identically;
//   - an absolute value must equal the root or start with root + "/" — a root
//     with a prefix-like sibling ("/repo" vs "/repo-vendor/x.go") does NOT
//     match, where a strings.ReplaceAll would have rewritten it;
//   - the root text embedded LATER in a value (a qualified name that happens to
//     contain the root string) is left untouched, because only the prefix is
//     trimmed;
//   - anything absolute but outside the root is an error, not a rewrite.
//
// Case policy: comparison is case-sensitive, except under a Windows root
// (drive-lettered or UNC), where the platform itself is case-insensitive and a
// graph built as "C:\Repo" must still match a root spelled "c:\repo". The
// policy follows the ROOT's spelling, so it is deterministic on every host.
type Normalizer struct {
	// root is the cleaned, slash-separated graph root without a trailing "/".
	root string
	// fold enables case-insensitive prefix matching (Windows roots only).
	fold bool
}

// NewNormalizer binds a normalizer to a graph root. An empty root yields a
// normalizer that only cleans separators — the shape a pre-normalized fixture
// graph needs.
func NewNormalizer(root string) Normalizer {
	cleaned := cleanSlash(root)
	for len(cleaned) > 1 && strings.HasSuffix(cleaned, "/") {
		cleaned = strings.TrimSuffix(cleaned, "/")
	}
	if cleaned == "." {
		cleaned = ""
	}
	return Normalizer{root: cleaned, fold: hasDriveLetter(cleaned) || strings.HasPrefix(cleaned, "//")}
}

// Root returns the cleaned graph root.
func (n Normalizer) Root() string { return n.root }

// Path normalizes one stored file path to the repo-relative spelling.
func (n Normalizer) Path(value string) (string, error) {
	cleaned := cleanSlash(value)
	if cleaned == "" {
		return "", fmt.Errorf("crgbehavior: the bridge stored an empty path")
	}
	if !looksAbsolute(cleaned) {
		if escapesRoot(cleaned) {
			return "", fmt.Errorf("%w: %q escapes the root via ..", ErrOutsideRoot, value)
		}
		return cleaned, nil
	}
	rel, ok := n.trimRoot(cleaned)
	if !ok {
		return "", fmt.Errorf("%w: %q is not under %q", ErrOutsideRoot, value, n.root)
	}
	return rel, nil
}

// Qualified normalizes a stored qualified name. Upstream spells a SYMBOL as
// "<file path>::<symbol>", so only the path part is rewritten; the symbol part
// is preserved verbatim (it may legitimately contain separators, and for some
// languages backslashes).
//
// A value with no separator is one of two things, and telling them apart
// matters. An ABSOLUTE one is a FILE node — the release spells a file's
// qualified name as its path, and the build wrote it under the materialization
// root, so it has to be trimmed like any other path or the comparison ids
// carry "/home/runner/.cache/crg-behavior-worktrees/crg-<sha>/…" and the
// recorded baseline becomes specific to the machine that produced it. A
// RELATIVE one is an unresolved bare target (a call to `append`): it names no
// file and must not be run through path normalization at all.
func (n Normalizer) Qualified(value string) (string, error) {
	i := strings.Index(value, qualifiedSep)
	if i < 0 {
		if looksAbsolute(cleanSlash(value)) {
			return n.Path(value)
		}
		return value, nil
	}
	file, err := n.Path(value[:i])
	if err != nil {
		return "", err
	}
	return file + value[i:], nil
}

// trimRoot performs the clean-prefix trim. ok is false when value is not the
// root or a descendant of it.
func (n Normalizer) trimRoot(value string) (string, bool) {
	if n.root == "" {
		return value, true
	}
	if !n.hasPrefix(value, n.root) {
		return "", false
	}
	rest := value[len(n.root):]
	switch {
	case rest == "":
		return ".", true
	case strings.HasPrefix(rest, "/"):
		return strings.TrimPrefix(rest, "/"), true
	default:
		// "/repo-vendor/x.go" against root "/repo": a prefix-like SIBLING, not
		// a descendant. Rejecting it here is the whole point of clean-prefix
		// semantics.
		return "", false
	}
}

// hasPrefix applies the normalizer's case policy to a prefix test.
func (n Normalizer) hasPrefix(value, prefix string) bool {
	if len(value) < len(prefix) {
		return false
	}
	if n.fold {
		return strings.EqualFold(value[:len(prefix)], prefix)
	}
	return value[:len(prefix)] == prefix
}

// cleanSlash folds separators to "/" and cleans the value, preserving the
// leading "//" of a UNC root.
func cleanSlash(value string) string {
	slashed := strings.ReplaceAll(value, `\`, "/")
	if slashed == "" {
		return ""
	}
	unc := strings.HasPrefix(slashed, "//")
	cleaned := path.Clean(slashed)
	if unc && !strings.HasPrefix(cleaned, "//") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

// escapesRoot reports whether a relative path walks above the root.
func escapesRoot(value string) bool {
	return value == ".." || strings.HasPrefix(value, "../")
}

// looksAbsolute reports whether a stored path is absolute in the POSIX
// convention OR in Windows'. A graph built elsewhere must still be recognized
// as un-normalized regardless of the host the gate runs on.
func looksAbsolute(value string) bool {
	return strings.HasPrefix(value, "/") || hasDriveLetter(value)
}

// hasDriveLetter reports whether value starts with a Windows drive root.
func hasDriveLetter(value string) bool {
	if len(value) < 3 || value[1] != ':' {
		return false
	}
	if value[2] != '\\' && value[2] != '/' {
		return false
	}
	c := value[0] | ' ' // fold case
	return c >= 'a' && c <= 'z'
}
