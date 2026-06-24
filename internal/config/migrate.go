package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// V1BackupSuffix is the sidecar extension `da config migrate` appends to the
// original manifest before rewriting it as v2 (so `.agentsrc.json` →
// `.agentsrc.json.v1.bak`). The backup is the un-touched v1 bytes, letting a
// maintainer restore the pre-migration manifest by hand during the soak window.
const V1BackupSuffix = ".v1.bak"

// CurrentManifestVersion is the current .agentsrc.json schema version. A
// manifest with a lower Version (or one carrying deprecated v1 keys) is
// considered legacy and triggers a deprecation warning — the file still loads
// and the legacy keys are still folded (config-v2 §15.3 deprecation cadence:
// warn during the soak window, do not break parsing).
const CurrentManifestVersion = 2

// deprecatedV1Keys are the legacy top-level JSON keys that v2 supersedes. They
// are still read and folded into the unified stage_profiles / execution_profile
// model (see foldLegacyProfiles) but are never re-emitted; their presence in a
// manifest marks it as a legacy (pre-v2) shape. UnmarshalJSON records which of
// these were seen into AgentsRC.LegacyKeys.
var deprecatedV1Keys = []string{
	"app_type_verifier_map",
	"reviewer_profiles",
	"verifier_profiles",
}

// V1DeprecationWarning is the structured deprecation notice produced when a
// legacy (pre-v2) .agentsrc.json shape is detected. It is intentionally a value
// type (not an error): a v1 manifest still loads and still works during the
// soak window — the warning only nudges the user toward a v2 manifest. The
// zero value (Detected=false) means the manifest is clean v2.
type V1DeprecationWarning struct {
	// Detected reports whether any v1 marker was found. When false the other
	// fields are empty and callers should emit nothing.
	Detected bool
	// Version is the manifest's declared schema version (0 when absent).
	Version int
	// LegacyVersion is true when Version is below CurrentManifestVersion.
	LegacyVersion bool
	// LegacyKeys lists the deprecated v1 keys present in the manifest (sorted).
	LegacyKeys []string
}

// DetectV1Deprecation inspects a loaded manifest and reports whether it uses a
// legacy v1 shape — either an old schema version or one of the deprecated v1
// keys folded silently on load. A nil manifest yields the zero (undetected)
// warning. This is a pure inspection: it never mutates rc and never fails.
func DetectV1Deprecation(rc *AgentsRC) V1DeprecationWarning {
	if rc == nil {
		return V1DeprecationWarning{}
	}
	legacyVersion := rc.Version < CurrentManifestVersion
	legacyKeys := append([]string(nil), rc.LegacyKeys...)
	w := V1DeprecationWarning{
		Version:       rc.Version,
		LegacyVersion: legacyVersion,
		LegacyKeys:    legacyKeys,
	}
	w.Detected = legacyVersion || len(legacyKeys) > 0
	return w
}

// Message renders the deprecation warning as a single human-readable line
// suitable for a `da doctor` / `da init` warn bullet. Returns "" when nothing
// was detected so callers can guard on the empty string.
func (w V1DeprecationWarning) Message() string {
	if !w.Detected {
		return ""
	}
	var reasons []string
	if w.LegacyVersion {
		reasons = append(reasons, fmt.Sprintf("schema version %d (current is %d)", w.Version, CurrentManifestVersion))
	}
	if len(w.LegacyKeys) > 0 {
		reasons = append(reasons, "deprecated keys: "+strings.Join(w.LegacyKeys, ", "))
	}
	return fmt.Sprintf("legacy v1 .agentsrc.json detected (%s); it still loads but is deprecated — migrate to v2",
		strings.Join(reasons, "; "))
}

// MigrationResult reports the outcome of a v1→v2 migration plan. It is produced
// by MigrateAgentsRC and is the single value `da config migrate` renders, so the
// command path has no migration logic of its own — it only formats this result.
//
// The result is computed identically for a real migration and a --dry-run
// preview; only the WroteFile/WroteBackup flags differ (both false on a dry run).
type MigrationResult struct {
	// ManifestPath is the absolute path to the .agentsrc.json that was inspected.
	ManifestPath string
	// BackupPath is where the original v1 bytes were (or would be) copied.
	BackupPath string
	// AlreadyV2 is true when the manifest is already clean v2 — no version bump
	// and no legacy keys — so migration is a no-op.
	AlreadyV2 bool
	// FromVersion is the manifest's declared schema version before migration.
	FromVersion int
	// ToVersion is the schema version after migration (always CurrentManifestVersion
	// when a migration is needed; equal to FromVersion on a no-op).
	ToVersion int
	// FoldedKeys lists the deprecated v1 keys that were folded away (dropped from
	// the rewritten manifest); empty when only a version bump was needed.
	FoldedKeys []string
	// V2JSON is the rewritten v2 manifest bytes (with a trailing newline). On a
	// no-op it is the manifest's current canonical serialization.
	V2JSON []byte
	// DryRun is true when this result was produced without writing.
	DryRun bool
	// WroteFile / WroteBackup report whether the migration actually wrote the v2
	// manifest and the backup sidecar. Both false on a no-op or a dry run.
	WroteFile   bool
	WroteBackup bool
}

// MigrateAgentsRC plans (and, unless dryRun, performs) an opt-in v1→v2 migration
// of the .agentsrc.json in projectPath. It is the testable core behind
// `da config migrate`.
//
// Behavior:
//   - Loads the manifest. LoadAgentsRC already folds the deprecated v1 keys
//     (verifier_profiles / reviewer_profiles / app_type_verifier_map) into the
//     unified v2 stage_profiles / execution_profile model, and MarshalJSON never
//     re-emits them — so the migration is "load → bump version → re-serialize".
//   - Idempotent: a clean v2 manifest (current version, no legacy keys) is a no-op;
//     AlreadyV2 is set and nothing is written.
//   - When migration is needed and dryRun is false, the ORIGINAL v1 bytes are
//     copied to .agentsrc.json.v1.bak BEFORE the v2 manifest is written, so the
//     pre-migration file is always recoverable. The version is bumped to
//     CurrentManifestVersion.
//
// This is intentionally non-destructive to v1 loading: it does not touch the
// loader or the silent-fold/warn path. It is an explicit, opt-in rewrite a
// maintainer runs per-repo during the 2-release deprecation soak.
func MigrateAgentsRC(projectPath string, dryRun bool) (MigrationResult, error) {
	manifestPath := filepath.Join(projectPath, AgentsRCFile)
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("reading %s: %w", AgentsRCFile, err)
	}

	rc, err := LoadAgentsRC(projectPath)
	if err != nil {
		return MigrationResult{}, err
	}

	res := MigrationResult{
		ManifestPath: manifestPath,
		BackupPath:   manifestPath + V1BackupSuffix,
		FromVersion:  rc.Version,
		ToVersion:    rc.Version,
		FoldedKeys:   append([]string(nil), rc.LegacyKeys...),
		DryRun:       dryRun,
	}

	warn := DetectV1Deprecation(rc)
	if !warn.Detected {
		res.AlreadyV2 = true
		res.V2JSON = original
		return res, nil
	}

	rc.Version = CurrentManifestVersion
	res.ToVersion = CurrentManifestVersion
	res.V2JSON, err = marshalManifest(rc)
	if err != nil {
		return MigrationResult{}, err
	}

	if dryRun {
		return res, nil
	}
	if err := writeMigration(manifestPath, res.BackupPath, original, res.V2JSON); err != nil {
		return MigrationResult{}, err
	}
	res.WroteBackup = true
	res.WroteFile = true
	return res, nil
}

// marshalManifest renders rc the same way Save does (indented, trailing newline)
// so the migrated file matches the canonical on-disk shape every other writer
// produces.
func marshalManifest(rc *AgentsRC) ([]byte, error) {
	data, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling %s: %w", AgentsRCFile, err)
	}
	return append(data, '\n'), nil
}

// writeMigration copies the original bytes to backupPath, then writes the v2
// bytes to manifestPath. The backup is written first so a failure leaves the
// original intact (no v2 file without a recoverable v1 sidecar).
func writeMigration(manifestPath, backupPath string, original, v2 []byte) error {
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return fmt.Errorf("writing backup %s: %w", filepath.Base(backupPath), err)
	}
	if err := os.WriteFile(manifestPath, v2, 0o644); err != nil {
		return fmt.Errorf("writing migrated %s: %w", AgentsRCFile, err)
	}
	return nil
}
