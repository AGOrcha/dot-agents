package crgbehavior

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// The gate certifies against exactly ONE code-review-graph release. Criterion 2
// asks whether the kg-native adapter preserves the behavior of the bridge the
// product actually ships against, so the baseline is the LATEST ratified
// release — not whichever build happens to be installed, and not an older
// release that would turn a stale-artifact divergence into a green run.
const (
	// PackageName is the PyPI distribution and CLI name of the legacy bridge.
	PackageName = "code-review-graph"
	// PinnedVersion is the ratified release baseline.
	PinnedVersion = "2.3.8"
	// PinnedTag is the upstream git tag PinnedVersion was cut from.
	PinnedTag = "v2.3.8"
	// PinnedCommit is the upstream commit PinnedTag resolves to.
	PinnedCommit = "2c6dae32643572ee528eb9b77dbcc17f58f3a8c9"
	// PinnedSchemaVersion is the graph.db `metadata.schema_version` that
	// release writes (migrations v2..v9; v9 added the edge confidence columns).
	PinnedSchemaVersion = 9
)

// DefaultReleasePath is the repo-relative path of the checked-in release
// capability fixture. Every release-dependent fact the gate needs — the
// indexed-language set, the required base schema, the derived view tables and
// their columns — is READ from that fixture rather than hardcoded per call
// site, so upgrading the baseline is one reviewable data change.
const DefaultReleasePath = "testdata/crg-behavior/release-2.3.8.json"

// TableSpec is one table of the pinned release's SQLite schema.
type TableSpec struct {
	// Name is the SQLite table (or FTS5 virtual table) name.
	Name string `json:"name"`
	// Required marks the BASE graph schema. A required table or column that is
	// absent is a release/plumbing incompatibility, never a behavior
	// divergence, and fails the gate immediately.
	Required bool `json:"required"`
	// Columns are the columns the gate reads. A missing column is a hard
	// schema failure for a required table and a capability failure otherwise —
	// it is never reported as "the view was not computed".
	Columns []string `json:"columns"`
	// Surfaces names the comparison surfaces this table feeds, so an
	// uncomputed view is attributed to the exact surfaces it disables.
	Surfaces []string `json:"surfaces,omitempty"`
}

// Release is the checked-in capability fixture for the pinned release: the
// facts the gate must not guess. It is validated against the pinned constants
// on load, so a fixture describing a different release cannot be used silently.
type Release struct {
	Package       string `json:"package"`
	Version       string `json:"version"`
	Tag           string `json:"tag"`
	Commit        string `json:"commit"`
	SchemaVersion int    `json:"schema_version"`
	// Languages maps a lowercase file extension to the release's language
	// label. It is the authority on which changed files carry graph symbols;
	// deriving it from the release is what stops a supported-language commit
	// from being silently discarded as "docs-only".
	Languages map[string]string `json:"extension_to_language"`
	// Tables is the release's schema contract.
	Tables []TableSpec `json:"tables"`
}

// ReleaseInfo is the OBSERVED release of the bridge a run actually drove — the
// CLI's own `--version` answer plus the schema version its graph.db carries.
// Both are printed in the report and persisted in the JSON artifact, so a run's
// evidence names the release it was produced against.
type ReleaseInfo struct {
	Package       string `json:"package"`
	Version       string `json:"version"`
	SchemaVersion int    `json:"schema_version"`
}

// String renders the observed release for report headers.
func (r ReleaseInfo) String() string {
	return fmt.Sprintf("%s %s (graph schema v%d)", r.Package, r.Version, r.SchemaVersion)
}

// LoadRelease reads and validates the release capability fixture.
func LoadRelease(path string) (Release, error) {
	data, err := os.ReadFile(path) //nolint:gosec // pinned test fixture path
	if err != nil {
		return Release{}, fmt.Errorf("crgbehavior: read release fixture: %w", err)
	}
	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return Release{}, fmt.Errorf("crgbehavior: parse release fixture %s: %w", path, err)
	}
	if err := rel.Validate(); err != nil {
		return Release{}, err
	}
	return rel, nil
}

// Validate rejects a fixture that does not describe the pinned release. The
// gate's whole claim is "behavior preserved against the latest ratified
// release", so a fixture for any other release is a contradiction, not a knob.
func (r Release) Validate() error {
	switch {
	case r.Package != PackageName:
		return fmt.Errorf("crgbehavior: release fixture package %q, want %q", r.Package, PackageName)
	case r.Version != PinnedVersion:
		return fmt.Errorf("crgbehavior: release fixture version %q, want the pinned baseline %q",
			r.Version, PinnedVersion)
	case r.Tag != PinnedTag:
		return fmt.Errorf("crgbehavior: release fixture tag %q, want %q", r.Tag, PinnedTag)
	case r.Commit != PinnedCommit:
		return fmt.Errorf("crgbehavior: release fixture commit %q, want %q", r.Commit, PinnedCommit)
	case r.SchemaVersion != PinnedSchemaVersion:
		return fmt.Errorf("crgbehavior: release fixture schema_version %d, want %d",
			r.SchemaVersion, PinnedSchemaVersion)
	case len(r.Languages) == 0:
		return fmt.Errorf("crgbehavior: release fixture declares no indexed extensions")
	case len(r.Tables) == 0:
		return fmt.Errorf("crgbehavior: release fixture declares no schema tables")
	}
	for _, t := range r.Tables {
		if len(t.Columns) == 0 {
			return fmt.Errorf("crgbehavior: release fixture table %q declares no columns", t.Name)
		}
		// The probe counts rows through a literal statement per table, so the
		// fixture may only name tables the pinned release is known to write.
		// Checking that at LOAD turns a fixture defect into one clear error
		// instead of a mid-probe failure on one table.
		if _, ok := rowCountSQL[t.Name]; !ok {
			return fmt.Errorf("crgbehavior: release fixture names table %q, which %s %s is not known to write",
				t.Name, PackageName, PinnedVersion)
		}
	}
	return nil
}

// Table returns the spec for one table.
func (r Release) Table(name string) (TableSpec, bool) {
	for _, t := range r.Tables {
		if t.Name == name {
			return t, true
		}
	}
	return TableSpec{}, false
}

// LanguageOf returns the release's language label for a repo-relative path, or
// "" when the release does not index that extension.
func (r Release) LanguageOf(file string) string {
	return r.Languages[strings.ToLower(path.Ext(file))]
}

// Indexes reports whether the release's parsers index this file at all.
func (r Release) Indexes(file string) bool {
	return r.LanguageOf(file) != ""
}

// IndexedLanguages returns the release's distinct language labels, sorted.
func (r Release) IndexedLanguages() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(r.Languages))
	for _, lang := range r.Languages {
		if !seen[lang] {
			seen[lang] = true
			out = append(out, lang)
		}
	}
	sort.Strings(out)
	return out
}

// cliVersionRe matches the pinned release's `--version` line, which prints
// "code-review-graph <version>" and nothing else.
var cliVersionRe = regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(PackageName) + `\s+(\S+)\s*$`)

// ParseCLIVersion extracts the version from `code-review-graph --version`
// output. A CLI whose version cannot be read is a hard failure: an unidentified
// bridge cannot be a release-conformance baseline.
func ParseCLIVersion(out string) (string, error) {
	m := cliVersionRe.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("crgbehavior: cannot read a %s version from %q", PackageName, strings.TrimSpace(out))
	}
	return m[1], nil
}

// CheckVersion rejects a bridge that is not the pinned release.
func (r Release) CheckVersion(observed string) error {
	if observed != r.Version {
		return fmt.Errorf("%w: bridge CLI reports %s %s, the gate certifies against %s",
			ErrReleaseMismatch, PackageName, observed, r.Version)
	}
	return nil
}
