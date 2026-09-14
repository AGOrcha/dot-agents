package graphstore

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// coverageFile is the sidecar a workspace build writes next to the graph
// database, recording which repository roots that build actually covered.
//
// It exists so readiness can tell three different empty submodules apart: one
// the build never looked at (missing — the defect), one the operator
// deliberately excluded, and one the build did index and found no symbols in
// (a docs-only or unsupported-language submodule). Without the record, the
// last two look identical to the first and a legitimately partial workspace
// could never report ready again.
const coverageFile = "da-workspace.json"

// buildCoverage is the sidecar's contents.
type buildCoverage struct {
	// SchemaVersion guards future format changes; an unrecognized version is
	// treated as no record at all.
	SchemaVersion int `json:"schema_version"`
	// Indexed lists the submodule paths the last build indexed.
	Indexed []string `json:"indexed,omitempty"`
	// Excluded lists the submodule paths the last build was told to skip.
	Excluded []string `json:"excluded,omitempty"`
}

// coverageSchemaVersion is the current sidecar format.
const coverageSchemaVersion = 1

// coveragePath returns the sidecar location for repoRoot.
func coveragePath(repoRoot string) string {
	return filepath.Join(filepath.Dir(CRGDBPath(repoRoot)), coverageFile)
}

// indexed reports whether the last build indexed this submodule.
func (c buildCoverage) indexed(path string) bool { return containsPath(c.Indexed, path) }

// excluded reports whether the last build was told to skip this submodule.
func (c buildCoverage) excluded(path string) bool { return containsPath(c.Excluded, path) }

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// readCoverage loads the sidecar, returning a zero value when it is absent,
// unreadable, or written by a format this build does not understand. A missing
// record is not an error: it just means nothing is known about the last
// build's scope, which is exactly the pre-sidecar behavior.
func readCoverage(repoRoot string) buildCoverage {
	data, err := os.ReadFile(coveragePath(repoRoot))
	if err != nil {
		return buildCoverage{}
	}
	var c buildCoverage
	if err := json.Unmarshal(data, &c); err != nil || c.SchemaVersion != coverageSchemaVersion {
		return buildCoverage{}
	}
	return c
}

// writeCoverage records the roots a completed build covered and the ones it
// was told to skip.
func writeCoverage(repoRoot string, plan WorkspacePlan) error {
	c := buildCoverage{SchemaVersion: coverageSchemaVersion}
	for _, root := range plan.Submodules() {
		c.Indexed = append(c.Indexed, root.Path)
	}
	for _, skipped := range plan.Skipped {
		if skipped.Reason == SkipReasonExcluded {
			c.Excluded = append(c.Excluded, skipped.Path)
		}
	}
	// A struct of strings and an int cannot fail to marshal.
	data, _ := json.MarshalIndent(c, "", "  ")
	dir := filepath.Dir(coveragePath(repoRoot))
	if err := fsops.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fsops.WriteFile(coveragePath(repoRoot), data, 0o644)
}
