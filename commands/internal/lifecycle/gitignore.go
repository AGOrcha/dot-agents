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

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// Status lines the callers surface for each outcome. Named so install and
// refresh report the identical thing for the identical action.
const (
	managedGitignoreWroteMsg   = "managed .gitignore block updated"
	managedGitignoreRemovedMsg = "managed .gitignore block removed (gitignore_projections: false)"
	managedGitignoreSkipMsg    = "managed .gitignore skipped (manifest unreadable)"
)

// MaintainManagedGitignore brings the consuming project's managed `.gitignore`
// block in line with what dot-agents projects into it, and returns the status
// line describing what it did.
//
// The manifest's `gitignore_projections` knob selects the direction: enabled
// (the default, including for a manifest-less project) regenerates the block
// from the enabled platforms' declared outputs; an explicit false removes any
// block a previous run left. A missing `.agentsrc.json` is not an error — a
// project can be managed without one, and it should still get the block.
//
// An UNREADABLE manifest is different from a missing one: the knob's value is
// genuinely unknown, so this reports a skip rather than writing a block against
// a guessed default or failing the run. (`da install` never reaches this case —
// it aborts on a corrupt manifest much earlier — and `da refresh` already
// tolerates one everywhere else, so skipping keeps refresh's failure semantics
// unchanged.) Only a real write failure is returned as an error.
//
// Callers own dry-run gating; this function always writes.
func MaintainManagedGitignore(projectPath string, enabled []platform.Platform) (string, error) {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return managedGitignoreSkipMsg, nil
		}
		// Manifest-less: fall through with a nil rc, which reads as the
		// default-on knob rather than as "opted out".
		rc = nil
	}
	if !rc.GitignoreProjectionsEnabled() {
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
