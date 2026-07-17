package config

import "fmt"

// oci_resolve.go classifies an OCI ref's version-spec into the two
// wire-supported forms — an explicit tag or a "pinned:sha256:..." digest — and
// rejects the one form package-artifact-install spec §6 explicitly defers:
// a SemVer range selector (e.g. "^1.2", "~1.2.3", ">=1.0 <2.0"). Without this
// guard, parseOCIRef (fetcher_oci.go) silently treated a range spec as a
// literal tag string; a caller learned only from a confusing registry
// 404/malformed-tag error far downstream instead of a clear, actionable
// "ranges are deferred" message at the point the ref was parsed. Both the
// packages (artifact) and extends (layer) OCI refs route through parseOCIRef,
// so this classification applies to both — neither surface resolves SemVer
// ranges yet.

// classifyOCIVersionSpec resolves an OCI ref's version-spec into its Tag/Digest
// form. A "pinned:sha256:..." spec becomes a digest pin (tag is empty); an
// empty spec leaves both empty so the registry's own default (e.g. "latest")
// applies; any other spec is treated as a literal tag UNLESS it looks like a
// SemVer range, which is rejected with an explicit "deferred" error instead of
// being silently forwarded as a tag name.
func classifyOCIVersionSpec(spec string) (tag, digest string, err error) {
	if d, ok := digestFromVersionSpec(spec); ok {
		return "", d, nil
	}
	if spec != "" && looksLikeSemVerRange(spec) {
		return "", "", fmt.Errorf("oci version-spec %q looks like a SemVer range; range resolution is deferred (package-artifact-install spec §6) — use an explicit tag or pinned:sha256:<digest>", spec)
	}
	return spec, "", nil
}

// looksLikeSemVerRange reports whether spec has the shape of a SemVer range
// selector rather than a literal tag: a caret/tilde prefix, an explicit
// comparison operator, a wildcard segment ("1.x", "1.*"), or a " - " range
// join. A bare version or tag ("1.2.3", "latest", "main", "v2") is not a range
// and is treated as a literal tag, matching today's registry tag semantics.
func looksLikeSemVerRange(spec string) bool {
	if spec == "" {
		return false
	}
	switch spec[0] {
	case '^', '~', '<', '>', '=':
		return true
	}
	for i := 0; i < len(spec); i++ {
		switch spec[i] {
		case '*':
			return true
		case ' ':
			// " - " (space-hyphen-space) is the SemVer range join form
			// ("1.2.0 - 1.3.0"); a bare space is otherwise not a valid tag
			// character, so any spec containing one is already suspect.
			if i+2 < len(spec) && spec[i+1] == '-' && spec[i+2] == ' ' {
				return true
			}
		}
	}
	// A trailing ".x"/".X" wildcard segment ("1.x", "2.X") is the other common
	// range shorthand not caught by the scans above.
	if len(spec) >= 2 {
		last := spec[len(spec)-1]
		if (last == 'x' || last == 'X') && spec[len(spec)-2] == '.' {
			return true
		}
	}
	return false
}
