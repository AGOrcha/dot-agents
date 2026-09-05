// Package crgbehavior implements the CRG behavior-preservation gate
// (graph-backend-adapter-contract §11.4 criterion 2): the dual-read comparison
// the hermetic §11.6 parity gate performs over a SYNTHETIC corpus, re-run over
// a corpus of REAL review tasks derived from this repository's own history.
//
// The parity gate (testdata/crg-parity) proves the kg-native adapter and the
// crg-bridge mirror agree on 10 pinned synthetic commits. It cannot prove the
// kg-native derivations reproduce what the legacy Python bridge actually
// persisted for a real repository, because it never touches the live bridge.
// This package closes that gap: for each pinned real commit it drives the
// review-relevant queries the review skills issue (changed-file impact radius,
// flows touched, community membership of changed symbols, FTS over changed
// identifiers) against BOTH sides and applies the SAME structural oracles as
// the parity gate (crg.CompareFlowMemberships, graphstore.PartitionAgreement,
// graphstore.SpearmanTau, crg.CompareFTS, graphstore.CompareImpactRadius).
//
// The bridge side is the legacy Python CRG's own persisted state and its own
// query surface; the native side is driven through the adapter/Store API
// directly (crg.Bootstrap + the *FromStore readback surfaces), never through
// the `da kg` command layer, so the gate is independent of which backend the
// production commands are currently wired to.
package crgbehavior

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// ManifestSchemaVersion is the pinned manifest format version. A manifest
// written by an older builder is rejected rather than silently misread.
const ManifestSchemaVersion = 1

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
	// ChangedFiles are the repo-relative graph-indexable files the commit
	// touched — the impact-radius / flows / communities query input.
	ChangedFiles []string `json:"changed_files"`
	// Identifiers are the declaration names added or removed by the commit —
	// the FTS query input ("FTS over changed identifiers").
	Identifiers []string `json:"identifiers"`
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
	if len(m.Tasks) == 0 {
		return fmt.Errorf("crgbehavior: manifest has no tasks")
	}
	for i, t := range m.Tasks {
		if t.Commit == "" {
			return fmt.Errorf("crgbehavior: manifest task %d has no commit", i)
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
