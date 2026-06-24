package config

import (
	"fmt"
	"strings"
)

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
