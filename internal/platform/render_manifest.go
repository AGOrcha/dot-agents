package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// Render-provenance manifest.
//
// writeManagedFile must never silently overwrite a rendered file the user
// hand-edited. We cannot tell "user edited it" from "our template changed"
// by content alone, so we persist the sha256 of the content WE last
// rendered per destination. On a divergent existing file: if its hash
// matches our recorded render, it is ours → overwrite freely; otherwise it
// is a user edit (or unknown provenance) → preserve it via
// BackupBeforeOverwrite before replacing.
//
// FORWARD-COMPAT: this is a deliberate stopgap. It lives in its own
// versioned, path-keyed, hashed file under the XDG state dir so the
// upcoming config-distribution model + lock file can absorb these entries
// (same shape: path → {hash, timestamp, schema_version}) without a
// disruptive migration. Do not couple new readers to the file location;
// go through the helpers here.
const renderManifestSchemaVersion = 1

type renderManifestEntry struct {
	SHA256     string `json:"sha256"`
	RenderedAt string `json:"rendered_at"`
}

type renderManifest struct {
	SchemaVersion int                            `json:"schema_version"`
	Entries       map[string]renderManifestEntry `json:"entries"`
}

func renderManifestPath() string {
	return filepath.Join(config.AgentsStateDir(), "render-manifest.json")
}

func renderContentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func manifestKey(dst string) string {
	if abs, err := filepath.Abs(dst); err == nil {
		return abs
	}
	return dst
}

func loadRenderManifest() *renderManifest {
	m := &renderManifest{SchemaVersion: renderManifestSchemaVersion, Entries: map[string]renderManifestEntry{}}
	data, err := os.ReadFile(renderManifestPath())
	if err != nil {
		return m // absent/unreadable → empty (nothing is provably ours yet)
	}
	var loaded renderManifest
	if json.Unmarshal(data, &loaded) != nil || loaded.Entries == nil {
		return m // corrupt → treat as empty; never block a render on it
	}
	// Schema-skew guard: an older binary must never trust a future schema's
	// hashes. Doing so would let it skip BackupBeforeOverwrite for a divergent
	// file whose entry semantics it no longer understands — the opposite of a
	// conservative data-loss guard. Only a manifest whose schema_version
	// exactly matches what this binary writes is trusted; a missing/0 version
	// or any version other than the current one is treated as untrusted →
	// empty manifest, so divergent files are conservatively backed up rather
	// than silently overwritten. (Currently only v1 exists; when a v2 ships,
	// add explicit forward-/backward-compatible handling for known older
	// versions here.)
	if loaded.SchemaVersion != renderManifestSchemaVersion {
		return m
	}
	return &loaded
}

// renderManifestHash returns the hash we last recorded for dst, or "".
func renderManifestHash(dst string) string {
	return loadRenderManifest().Entries[manifestKey(dst)].SHA256
}

// recordRenderHash persists that we rendered dst with the given content
// hash. Best-effort: a manifest-write failure must not fail the render
// (the file is already correct on disk); it only weakens future
// provenance, which BackupBeforeOverwrite still makes safe.
//
// io is the injected platform IO (production passes stdPlatformIO{}); the
// two best-effort mkdir/write call sites that follow used to be the
// osMkdirAll / osWriteFile func-var seams.
func recordRenderHash(io platformIO, dst, hash string) {
	m := loadRenderManifest()
	m.Entries[manifestKey(dst)] = renderManifestEntry{
		SHA256:     hash,
		RenderedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if io.MkdirAll(filepath.Dir(renderManifestPath()), 0755) != nil {
		return
	}
	_ = io.WriteFile(renderManifestPath(), append(data, '\n'), 0644)
}

// BackupBeforeOverwrite preserves an existing managed-file destination
// before writeManagedFile replaces it with freshly rendered content. The
// default writes a sibling <dst>.dot-agents-backup (the existing repo
// backup convention, headless-safe, no layering inversion). The future
// config-distribution / lock-file model can wire a richer mirror-backup
// adapter through this seam without touching internal/platform.
//
// This is a deliberate forward-compat extension point (NOT a test seam) —
// see the docstring above. Tests swap it via the legacy func-var pattern
// because that pattern matches its intended runtime-swap contract; the
// seam-interface-di-migration plan does not target this var.
var BackupBeforeOverwrite = func(dst string) error { return sidecarBackup(stdPlatformIO{}, dst) }

// sidecarBackup is the default BackupBeforeOverwrite impl. It is also the
// production-side call when render_manifest internals invoke the backup
// path; in either case the injected platformIO governs the WriteFile that
// emits the backup sibling. Tests inject a fakePlatformIO whose WriteFile
// returns a sentinel to exercise the write-fail branch.
func sidecarBackup(io platformIO, dst string) error {
	data, err := os.ReadFile(dst)
	if err != nil {
		return fmt.Errorf("read %s for backup: %w", dst, err)
	}
	bak := dst + ".dot-agents-backup"
	if err := io.WriteFile(bak, data, 0644); err != nil {
		return fmt.Errorf("write backup %s: %w", bak, err)
	}
	return nil
}

// backupSidecar is the canonical preservation step passed as the `backup`
// argument to links.SymlinkReplacing / links.HardlinkReplacing at every
// internal/platform call site that LEGITIMATELY (re)creates a dot-agents
// managed link over a path it owns. It preserves a genuine user file as a
// sibling <path>.dot-agents-backup (the established repo convention) before
// links removes the entry and installs the managed link.
//
// Behaviour by occupant kind (links decides which path is taken):
//   - our own stale managed hard link / regular file: backed up (a harmless,
//     idempotent copy of bytes identical to the canonical source) then
//     relinked — keeps refresh idempotent.
//   - a real user file: preserved as <path>.dot-agents-backup, then replaced.
//   - backup failure: links aborts and leaves the original entry intact.
//
// It delegates to BackupBeforeOverwrite so the future config-distribution /
// lock-file model can swap in a richer mirror-backup adapter through the one
// existing seam without touching call sites.
func backupSidecar(path string) error {
	return BackupBeforeOverwrite(path)
}
