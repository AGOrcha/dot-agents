// Package crgbehavior implements the CRG behavior-preservation gate
// (graph-backend-adapter-contract §11.4 criterion 2): does the kg-native
// adapter preserve the behavior consumers get from the code-review-graph
// release the product actually ships against?
//
// The hermetic §11.6 parity gate (testdata/crg-parity) compares the kg-native
// adapter against an in-process mirror over a SYNTHETIC corpus. It never
// touches the real bridge, so it cannot answer that question. This package
// does, under four rules that together decide whether a run is evidence:
//
//  1. ONE PINNED RELEASE. Every run is driven against code-review-graph
//     PinnedVersion and its graph schema PinnedSchemaVersion, both verified
//     before anything is compared and both printed and persisted in the run's
//     report and JSON artifact. An unidentified or off-release bridge is a
//     hard failure, not a baseline.
//
//  2. SAME-SHA MATERIALIZATION. Each pinned review task is replayed against a
//     graph BUILT AT THAT TASK'S OWN COMMIT, in an isolated worktree. Replaying
//     historical changed-file paths against one current graph cannot detect a
//     historical-output regression and lets a moved path resolve to a
//     different symbol.
//
//  3. EXACT ORACLES. The release's output for a given graph is deterministic,
//     so every surface is compared by exact set equality over canonical rows —
//     flow identity and ordered path, per-flow depth/counts/criticality, flow
//     snapshots, the community partition and its summaries, the full risk_index
//     row, the FTS5 index and its search RESULTS, the schema-v9 edge confidence
//     columns, and the release's build/postprocess staleness contract. Only
//     genuinely nondeterministic upstream values (autoincrement flow and
//     community ids, wall-clock timestamps) are normalized away. No rank
//     correlation, no partition-similarity score, no token-set overlap.
//
//  4. EXPLICIT COVERAGE. A corpus contract names the surfaces a run must
//     actually exercise. An unexercised required surface fails unless a
//     ratified, attributed exception covers it, so "we compared nothing here"
//     can never report the same verdict as "these behaviors match".
//
// The release side is the Python CRG's own persisted state and its own query
// surfaces; the native side is driven through the adapter/Store API directly
// (crg.Bootstrap plus the *FromStore readback surfaces), never through the
// `da kg` command layer, so the gate is independent of which backend the
// production commands are currently wired to.
package crgbehavior

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// ManifestSchemaVersion is the pinned manifest format version. v2 records the
// release the corpus was derived from and each task's languages, because both
// decide which commits are eligible and which surfaces a run can exercise — a
// v1 manifest was derived from a narrower, hardcoded language set and would
// silently under-report coverage.
const ManifestSchemaVersion = 2

// DefaultManifestPath is the repo-relative path of the checked-in corpus
// manifest. Regeneration is an explicit command (tools/crgbehaviorgate
// -regen), never an implicit side effect of running the gate, so a gate run is
// reproducible against a pinned commit list.
const DefaultManifestPath = "testdata/crg-behavior/manifest.json"

// Task is one pinned review task: a real commit plus the query inputs a review
// of that commit issues against the code graph. The commit SHA pins the corpus;
// changed files and identifiers pin the queries.
type Task struct {
	// Commit is the full SHA of the pinned commit.
	Commit string `json:"commit"`
	// Subject is the commit subject line, for human-readable gate reports.
	Subject string `json:"subject"`
	// ChangedFiles are the repo-relative files the pinned release INDEXES that
	// the commit touched — the impact-radius / flows / communities query input.
	ChangedFiles []string `json:"changed_files"`
	// Identifiers are the declaration names added or removed by the commit —
	// the FTS search input.
	Identifiers []string `json:"identifiers"`
	// Languages are the release's language labels for ChangedFiles, recorded
	// so parser coverage is auditable per task rather than assumed.
	Languages []string `json:"languages"`
}

// Manifest is the pinned review-task corpus. It is checked in so a gate run is
// reproducible: the same commits, the same query inputs, every run.
type Manifest struct {
	SchemaVersion int `json:"schema_version"`
	// GeneratedAt is the RFC3339 UTC timestamp of the last regeneration.
	GeneratedAt string `json:"generated_at"`
	// GeneratedFrom is the git ref the commit window was taken from.
	GeneratedFrom string `json:"generated_from"`
	// Head is the SHA GeneratedFrom pointed at when the manifest was written.
	Head string `json:"head"`
	// Window is how many commits the builder SCANNED to pin Tasks from. A
	// corpus is a sample; recording the window it was sampled from is what
	// makes the sample reproducible and its language coverage auditable.
	Window int `json:"window"`
	// Release is the code-review-graph release whose indexed-language set
	// decided which files and identifiers each task carries.
	Release string `json:"release"`
	// Tasks are the pinned review tasks, newest commit first.
	Tasks []Task `json:"tasks"`
}

// LoadManifest reads and validates a pinned corpus manifest.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // pinned test corpus path
	if err != nil {
		return Manifest{}, fmt.Errorf("crgbehavior: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("crgbehavior: parse manifest %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate rejects a manifest the gate cannot run reproducibly against.
func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("crgbehavior: manifest schema_version %d, want %d (regenerate with tools/crgbehaviorgate -regen)",
			m.SchemaVersion, ManifestSchemaVersion)
	}
	if m.Release != PinnedVersion {
		return fmt.Errorf("crgbehavior: manifest was derived from release %q, the gate certifies against %q "+
			"(regenerate with tools/crgbehaviorgate -regen)", m.Release, PinnedVersion)
	}
	if len(m.Tasks) == 0 {
		return fmt.Errorf("crgbehavior: manifest has no tasks")
	}
	for i, t := range m.Tasks {
		if t.Commit == "" {
			return fmt.Errorf("crgbehavior: manifest task %d has no commit", i)
		}
		if len(t.ChangedFiles) == 0 {
			return fmt.Errorf("crgbehavior: manifest task %d (%s) lists no indexed file", i, short(t.Commit))
		}
	}
	return nil
}

// Save writes the manifest as pretty-printed JSON, creating the parent
// directory. Regeneration is explicit, so the write is too.
func (m Manifest) Save(path string) error {
	return writeJSON(path, m)
}

// writeJSON writes v as pretty-printed JSON, creating the parent directory.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("crgbehavior: encode manifest: %w", err)
	}
	if err := fsops.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("crgbehavior: create manifest dir: %w", err)
	}
	if err := fsops.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("crgbehavior: write manifest: %w", err)
	}
	return nil
}
