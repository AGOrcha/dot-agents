package config

import (
	"fmt"
	"path/filepath"
)

// overlay.go adds the project-local overlay scope (config-distribution-model
// §15 D9 / §7A.1, "Axis A"): an 8th scope between the committed repo-local
// manifest and runtime. It is a da-managed, gitignored .agentsrc.local.json in
// the repo — the .git/config analog: personal, machine-local, per-project, and
// never committed. It merges ABOVE repo-local committed, so a developer's local
// preferences win over the committed manifest without altering version control.
//
// The overlay is also one of the local scopes folded into inputs_digest. That
// hashing side is owned by staleness.go (cb-content-hash-staleness), which
// already reads the same AgentsRCLocalFile through localScopeManifests; this file
// owns the resolver MERGE side so the overlay's fields actually participate in
// effective config. The two share the single AgentsRCLocalFile constant so the
// path they key on can never drift.

// LayerProjectLocal is the project-local overlay layer identifier — the
// .agentsrc.local.json scope that sits just above LayerRepoLocal in precedence.
// The value matches the "project-local" slot name staleness.go namespaces the
// overlay under in inputs_digest, so the same scope reads consistently across the
// merge surface and the hash surface.
const LayerProjectLocal = "project-local"

// projectLocalOverlayPath returns the absolute path of a project's overlay
// manifest: the gitignored .agentsrc.local.json sibling of the committed
// .agentsrc.json. It is the single definition the resolver keys on; staleness's
// localScopeManifests derives the same path from the same AgentsRCLocalFile
// constant.
func projectLocalOverlayPath(projectPath string) string {
	return filepath.Join(projectPath, AgentsRCLocalFile)
}

// loadProjectLocalOverlay loads the optional project-local overlay layer.
//
// It returns (layer, true, nil) when .agentsrc.local.json exists and parses as a
// JSON object, (ResolvedLayer{}, false, nil) when the file is absent (the overlay
// is optional — an absent overlay is the common case and never an error), and
// (ResolvedLayer{}, false, err) when the file exists but does not parse, so a
// corrupt personal overlay fails loudly rather than being silently ignored.
//
// The returned layer carries ID=LayerProjectLocal, so resolveSnapshot's existing
// protected-field guard automatically drops any repo_id/project the overlay tries
// to set (only LayerRepoLocal may own those) — the overlay honors protected-field
// rules without any special-casing here.
func loadProjectLocalOverlay(projectPath string) (ResolvedLayer, bool, error) {
	path := projectLocalOverlayPath(projectPath)
	raw, ok, err := decodeObjectFile(path)
	if err != nil {
		return ResolvedLayer{}, false, fmt.Errorf("parsing project-local overlay %s: %w", path, err)
	}
	if !ok {
		return ResolvedLayer{}, false, nil
	}
	return ResolvedLayer{ID: LayerProjectLocal, Present: true, Raw: raw}, true, nil
}
