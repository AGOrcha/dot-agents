package config

import (
	"fmt"
	"path/filepath"
)

// overlay.go adds the project-local overlay scope (config-distribution-model
// §7A.1 / D9): an 8th resolution scope between the committed repo-local manifest
// and runtime. It is a da-managed, gitignored `.agentsrc.local.json` stored in
// the repo — the `.git/config` analog: personal, machine-local, per-project, and
// never committed. Because it sits ABOVE repo-local committed in precedence, an
// override authored there gets the last word among the local manifests; because
// it is not the repo-local layer, the existing protected-field guard
// (resolveSnapshot) drops any `repo_id`/`project` it tries to set, exactly as it
// does for imported and user-local layers.
//
// The overlay is also a local input folded into `inputs_digest` — that side is
// owned by staleness.go (localScopeManifests already includes its slot), so this
// file is purely the resolver-merge half of the scope. The two share the single
// AgentsRCLocalFile constant defined in staleness.go.

// LayerProjectLocal is the project-local overlay scope identifier (§7A.1 / D9):
// the gitignored `.agentsrc.local.json` that resolves ABOVE LayerRepoLocal and
// below runtime. It is the highest-precedence local manifest scope, so it is the
// last writer among the on-disk layers and `da config explain` renders it as a
// distinct stack entry between repo-local and any runtime override.
const LayerProjectLocal = "project-local"

// projectLocalOverlayPath returns the absolute path of the project-local overlay
// manifest for projectPath: the gitignored `.agentsrc.local.json` sibling of the
// committed `.agentsrc.json`. It is the single definition of the overlay's
// location, mirroring AgentsLockPath for the lockfile, so the resolver and the
// staleness digest can never disagree on where the overlay lives.
func projectLocalOverlayPath(projectPath string) string {
	return filepath.Join(projectPath, AgentsRCLocalFile)
}

// loadProjectLocalOverlayLayer loads the optional project-local overlay manifest
// as a ResolvedLayer slotted ABOVE repo-local committed. The overlay is a
// CONDITIONAL scope, like an imported `extends` layer rather than the
// always-present user-local slot: it only joins the stack when the file exists
// (ok=true). An absent overlay returns ok=false and no layer, so projects
// without one resolve exactly as before — the overlay never appears in the
// effective layer stack until a personal `.agentsrc.local.json` is authored.
//
// An existing-but-unparseable overlay is fatal — a broken personal manifest must
// surface loudly rather than be silently skipped, which would let a malformed
// override resolve as if the scope were empty.
func loadProjectLocalOverlayLayer(projectPath string) (ResolvedLayer, bool, error) {
	overlayPath := projectLocalOverlayPath(projectPath)
	raw, ok, err := decodeObjectFile(overlayPath)
	if err != nil {
		return ResolvedLayer{}, false, fmt.Errorf("parsing project-local overlay %s: %w", overlayPath, err)
	}
	if !ok {
		return ResolvedLayer{}, false, nil
	}
	return ResolvedLayer{ID: LayerProjectLocal, Present: true, Raw: raw}, true, nil
}
