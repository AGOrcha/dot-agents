package config

import (
	"github.com/AGOrcha/dot-agents/internal/agentslock"
)

// LockSectionRefresh is the agentslock section name for the latest da
// install/refresh stamp on a project (config-distribution-model §7A /
// refresh-metadata-to-lock). Refresh metadata (version, commit, describe,
// refreshedAt) is resolved STATE about the project — not manifest content —
// so it lives alongside inputs_digest/units in .agentsrc.lock rather than the
// committed .agentsrc.json. It mirrors the pattern the install pipeline
// already established (commands/internal/lifecycle/install.go's
// installLockSection): one opaque top-level section per writer, preserved
// verbatim by every other section writer via agentslock.SetSection.
const LockSectionRefresh = "refresh"

// WriteRefreshLock stamps the given refresh metadata into the project's
// .agentsrc.lock "refresh" section, preserving every sibling section (units,
// install, adapters, …). It is the §7A home for what
// AgentsRC.SetRefreshMetadata used to write into the committed manifest.
func WriteRefreshLock(projectPath string, meta RefreshMetadata) error {
	lf, err := agentslock.Open(AgentsLockPath(projectPath))
	if err != nil {
		return err
	}
	if err := lf.SetSection(LockSectionRefresh, meta); err != nil {
		return err
	}
	return lf.Flush()
}

// ReadRefreshLock loads the "refresh" section of a project's .agentsrc.lock.
// ok is false when the lockfile carries no refresh stamp (no da
// install/refresh has ever stamped this project's lock).
func ReadRefreshLock(projectPath string) (RefreshMetadata, bool, error) {
	lf, err := agentslock.Open(AgentsLockPath(projectPath))
	if err != nil {
		return RefreshMetadata{}, false, err
	}
	var meta RefreshMetadata
	ok, err := lf.Section(LockSectionRefresh, &meta)
	if err != nil {
		return RefreshMetadata{}, false, err
	}
	return meta, ok, nil
}
