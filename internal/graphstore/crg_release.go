package graphstore

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// CRGRelease identifies the code-review-graph installation behind the bridge.
//
// It exists because "which release is actually installed?" is a question
// several callers must answer before they can trust anything else: the
// behaviour gate records it in its report, and the capability diagnostic has
// to say whether the bridge it would route to is the release this product is
// pinned to. Answering it by re-running `--version` in each caller would add
// a process-execution site per caller; this is the one accessor.
type CRGRelease struct {
	// Version is the installed release, e.g. "2.3.8".
	Version string
	// SchemaVersion is the graph database's `metadata.schema_version`, or 0
	// when no graph has been built yet. It is read separately from the
	// version because a new release can be installed against a database that
	// has not been migrated.
	SchemaVersion int
	// Bin is the executable the version was read from.
	Bin string
	// MatchesPin reports whether Version equals the release this product's
	// contract is generated from.
	MatchesPin bool
}

// Release reports the installed release and the graph's schema version.
func (b *CRGBridge) Release() (*CRGRelease, error) {
	out, err := b.runCaptured("--version")
	if err != nil {
		return nil, fmt.Errorf("read code-review-graph version: %w", err)
	}
	version := parseCRGVersion(string(out))
	if version == "" {
		return nil, fmt.Errorf("unrecognized code-review-graph version output: %q",
			strings.TrimSpace(string(out)))
	}
	release := &CRGRelease{
		Version:    version,
		Bin:        b.Bin,
		MatchesPin: version == crgrelease.Version,
	}
	release.SchemaVersion = b.graphSchemaVersion()
	return release, nil
}

// parseCRGVersion extracts the version from `--version` output, which the
// release prints as a single line that may or may not be prefixed with the
// program name.
func parseCRGVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for i := len(fields) - 1; i >= 0; i-- {
			candidate := strings.TrimPrefix(fields[i], "v")
			if candidate == "" {
				continue
			}
			if _, err := strconv.Atoi(strings.SplitN(candidate, ".", 2)[0]); err == nil {
				return candidate
			}
		}
	}
	return ""
}

// graphSchemaVersion reads the graph's schema version, reporting 0 when no
// graph exists. A missing graph is not an error here: the caller asked which
// release is installed, and "installed but never built" is a valid answer.
func (b *CRGBridge) graphSchemaVersion() int {
	db, err := sql.Open("sqlite", CRGDBPath(b.RepoRoot)+crgReadOnlyPragma)
	if err != nil {
		return 0
	}
	defer func() { _ = db.Close() }()
	var value string
	row := db.QueryRow("SELECT value FROM metadata WHERE key = 'schema_version'")
	if err := row.Scan(&value); err != nil {
		return 0
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}
