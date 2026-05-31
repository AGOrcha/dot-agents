package registry

import (
	"fmt"
	"strconv"
	"strings"
)

// builtinPrefix is the source prefix for adapters that ship inside `da`.
const builtinPrefix = "dotagents-builtin:graph/"

// Version is a parsed semantic version (major.minor.patch). Pre-release and
// build metadata are not modeled in v1 — adapter versions are plain triples.
type Version struct {
	Major int
	Minor int
	Patch int
}

// String renders the version as "major.minor.patch".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ParseVersion parses a "major.minor.patch" string. Missing minor/patch
// components default to 0 (so "1" and "1.2" are accepted).
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Version{}, fmt.Errorf("version: empty string")
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return Version{}, fmt.Errorf("version: %q has too many components", s)
	}
	var out Version
	dst := []*int{&out.Major, &out.Minor, &out.Patch}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, fmt.Errorf("version: %q component %q is not an integer", s, p)
		}
		if n < 0 {
			return Version{}, fmt.Errorf("version: %q component %q is negative", s, p)
		}
		*dst[i] = n
	}
	return out, nil
}

// Constraint is a parsed version constraint. Only the caret range (`^x.y.z`)
// and an exact match are supported in v1 — these are the forms the spec's
// app-type-profiles use (e.g. `@^1.0`).
type Constraint struct {
	// Caret is true for `^x.y.z` ranges; false for exact matches.
	Caret bool
	Base  Version
}

// ParseConstraint parses a constraint string. A leading `^` selects a caret
// range; otherwise the string is parsed as an exact version.
func ParseConstraint(s string) (Constraint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Constraint{}, fmt.Errorf("constraint: empty string")
	}
	caret := false
	if strings.HasPrefix(s, "^") {
		caret = true
		s = strings.TrimPrefix(s, "^")
	}
	base, err := ParseVersion(s)
	if err != nil {
		return Constraint{}, fmt.Errorf("constraint: %w", err)
	}
	return Constraint{Caret: caret, Base: base}, nil
}

// Satisfies reports whether v satisfies the constraint.
//
//   - Exact: v equals the base.
//   - Caret with major >= 1: same major, and v >= base.
//   - Caret with major 0: same major+minor, and v >= base (npm-style ^0.x).
func (c Constraint) Satisfies(v Version) bool {
	if !c.Caret {
		return v == c.Base
	}
	if v.Major != c.Base.Major {
		return false
	}
	if c.Base.Major == 0 && v.Minor != c.Base.Minor {
		return false
	}
	return !less(v, c.Base)
}

// less reports whether a < b.
func less(a, b Version) bool {
	if a.Major != b.Major {
		return a.Major < b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor < b.Minor
	}
	return a.Patch < b.Patch
}

// Ref is a parsed adapter backend reference.
type Ref struct {
	// Name is the adapter's short name (e.g. "none").
	Name string
	// Builtin is true when the ref carried the dotagents-builtin source
	// prefix.
	Builtin bool
	// Constraint is the version constraint, or nil when the ref carried no
	// `@constraint` suffix.
	Constraint *Constraint
}

// ParseRef parses a backend ref. Accepted forms:
//
//	dotagents-builtin:graph/none@^1.0   → name=none, builtin, caret 1.0.0
//	dotagents-builtin:graph/none        → name=none, builtin, no constraint
//	none@1.0.0                          → name=none, exact constraint
//	none                                → name=none, no constraint
func ParseRef(ref string) (Ref, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Ref{}, fmt.Errorf("ref: empty string")
	}
	var out Ref
	if strings.HasPrefix(ref, builtinPrefix) {
		out.Builtin = true
		ref = strings.TrimPrefix(ref, builtinPrefix)
	}
	name := ref
	if at := strings.Index(ref, "@"); at >= 0 {
		name = ref[:at]
		constraintStr := ref[at+1:]
		c, err := ParseConstraint(constraintStr)
		if err != nil {
			return Ref{}, fmt.Errorf("ref %q: %w", ref, err)
		}
		out.Constraint = &c
	}
	if name == "" {
		return Ref{}, fmt.Errorf("ref: missing adapter name")
	}
	if strings.ContainsAny(name, "/@: ") {
		return Ref{}, fmt.Errorf("ref: invalid adapter name %q", name)
	}
	out.Name = name
	return out, nil
}
