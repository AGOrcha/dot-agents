package lifecycle

// Managed-.gitignore maintenance shared by `da install` and `da refresh`
// (config-distribution-model §15 / D14 / R8).
//
// Both commands project the same generated outputs into a consuming repo, so
// both must leave the same managed block behind — otherwise an install followed
// by a refresh (or vice versa) would churn the file. The knob check, the
// projected-path collection, and the write/remove decision therefore live here
// once, and each command contributes only its own dry-run and UI phrasing.
//
// The projected-path set is never enumerated here: it comes from the platforms
// themselves via platform.CollectManagedOutputs, so a platform that changes its
// repo-local surface updates the .gitignore block by updating its own
// ManagedOutputs/staticManagedOutputs entry, with no second list to keep in sync.

import (
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// Status lines the callers surface for each outcome. Named so install and
// refresh report the identical thing for the identical action.
const (
	managedGitignoreWroteMsg   = "managed .gitignore block updated"
	managedGitignoreRemovedMsg = "managed .gitignore block removed (gitignore_projections: false)"
	managedGitignoreSkipMsg    = "managed .gitignore skipped (effective config unreadable)"
)

// MaintainManagedGitignore brings the consuming project's managed `.gitignore`
// block in line with what dot-agents projects into it, and returns the status
// line describing what it did.
//
// The EFFECTIVE `gitignore_projections` knob selects the direction: enabled
// (the default, including for a manifest-less project) regenerates the block
// from the enabled platforms' declared outputs; an explicit false removes any
// block a previous run left.
//
// snap is the layered snapshot the caller already resolved before projecting
// (install/refresh both hold one). Passing it — rather than reloading a flat
// manifest here — is what makes an org/team layer's `gitignore_projections`
// actually govern the block: the knob is a plain scalar, so the highest-
// precedence layer that declares it wins, and a repo that declares nothing
// inherits its layer's answer instead of silently defaulting to on.
//
// This function performs no dry-run check of its own — callers gate it.
func MaintainManagedGitignore(projectPath string, enabled []platform.Platform, snap *config.Snapshot) (string, error) {
	on, known := EffectiveGitignoreProjections(projectPath, snap)
	if !known {
		return managedGitignoreSkipMsg, nil
	}
	if !on {
		if err := links.RemoveManagedGitignore(projectPath); err != nil {
			return "", err
		}
		return managedGitignoreRemovedMsg, nil
	}
	if err := links.EnsureManagedGitignore(projectPath, platform.CollectManagedOutputs(enabled)); err != nil {
		return "", err
	}
	return managedGitignoreWroteMsg, nil
}

// EffectiveGitignoreProjections resolves the tri-state `gitignore_projections`
// knob against the layer stack, returning (enabled, known).
//
// It prefers the snapshot the caller already resolved. When the caller has none
// — refresh whose pass-1 resolve failed, or a caller outside the install/refresh
// flow — it resolves READ-ONLY (EnsureOpts{Frozen}: no fetch, no lock write, no
// cache write), so consulting the knob can never itself produce a lock a
// dry-run or best-effort path was supposed to avoid.
//
// known=false is the deliberate third state: an UNREADABLE manifest, or an
// `extends` project whose layers cannot be replayed offline, leaves the knob's
// value genuinely unknown. Reporting a skip is correct there — writing a block
// against a guessed default, or retracting one the project asked for, are both
// worse than doing nothing. A MISSING manifest is not that case: a project can
// be managed without one and still gets the default-on block.
func EffectiveGitignoreProjections(projectPath string, snap *config.Snapshot) (enabled, known bool) {
	if snap != nil {
		return snap.Effective.GitignoreProjectionsEnabled(), true
	}
	if res, err := config.EnsureResolved(projectPath, config.EnsureOpts{Frozen: true}); err == nil {
		return res.Snapshot.Effective.GitignoreProjectionsEnabled(), true
	}
	if _, err := os.Stat(filepath.Join(projectPath, config.AgentsRCFile)); os.IsNotExist(err) {
		return true, true
	}
	return false, false
}
