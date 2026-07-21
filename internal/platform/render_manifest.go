package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
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

// --- command-scoped render-manifest read cache (H6) ---
//
// The managed-hook write path (writeManagedFile → renderManifestHash +
// recordRenderHash) consults the render-manifest once per destination. Without
// a cache each consult is a full ReadFile + JSON decode of the whole (growing)
// manifest, so a single refresh writing N managed files pays O(N) redundant
// reads/parses of the same file (measured O(N²) allocs as the manifest grows).
//
// recordRenderHash is the SOLE in-process writer of the manifest, so it keeps
// the cache coherent by mutating the cached manifest IN PLACE under the lock and
// refreshing the on-disk signature after each persist — the "load → record →
// load sees the new hash" path never relies on filesystem mtime resolution. A
// path + stat signature (size + mtime + existence) additionally guards against
// ANY other writer: a test seeding the manifest directly, a different
// XDG_STATE_HOME between calls (the manifest path changes), or an out-of-process
// edit all shift the signature and force a reload, so a mutation between reads
// is always observed and a stale manifest is never served.
var (
	renderManifestMu       sync.Mutex
	renderManifestCache    *renderManifest
	renderManifestCacheSig renderManifestSig
	renderManifestReloads  int // instrumentation: disk (re)loads actually served
)

// renderManifestSig fingerprints the manifest file cheaply (one os.Stat). A
// change to path/size/mtime/existence between two reads invalidates the cache.
type renderManifestSig struct {
	path    string
	size    int64
	modNsec int64
	exists  bool
}

func renderManifestStatSig(path string) renderManifestSig {
	sig := renderManifestSig{path: path}
	if info, err := os.Stat(path); err == nil {
		sig.size = info.Size()
		sig.modNsec = info.ModTime().UnixNano()
		sig.exists = true
	}
	return sig
}

// loadRenderManifest returns the process-cached manifest when the on-disk
// signature is unchanged, else (re)reads and reparses it.
func loadRenderManifest() *renderManifest {
	renderManifestMu.Lock()
	defer renderManifestMu.Unlock()
	return loadRenderManifestLocked()
}

// loadRenderManifestLocked is loadRenderManifest's body; callers MUST hold
// renderManifestMu. The returned pointer is the LIVE cached value — read-only
// for every caller except recordRenderHash, which mutates it in place (as the
// sole in-process writer) under the same lock.
func loadRenderManifestLocked() *renderManifest {
	path := renderManifestPath()
	sig := renderManifestStatSig(path)
	if renderManifestCache != nil && renderManifestCacheSig == sig {
		return renderManifestCache
	}
	m := readRenderManifest(path)
	renderManifestCache = m
	renderManifestCacheSig = sig
	renderManifestReloads++
	return m
}

// readRenderManifest is the uncached disk read + schema-validated decode.
func readRenderManifest(path string) *renderManifest {
	m := &renderManifest{SchemaVersion: renderManifestSchemaVersion, Entries: map[string]renderManifestEntry{}}
	data, err := os.ReadFile(path)
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
	renderManifestMu.Lock()
	defer renderManifestMu.Unlock()
	return loadRenderManifestLocked().Entries[manifestKey(dst)].SHA256
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
	renderManifestMu.Lock()
	defer renderManifestMu.Unlock()
	m := loadRenderManifestLocked()
	m.Entries[manifestKey(dst)] = renderManifestEntry{
		SHA256:     hash,
		RenderedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	path := renderManifestPath()
	if io.MkdirAll(filepath.Dir(path), 0755) != nil {
		return
	}
	_ = io.WriteFile(path, append(data, '\n'), 0644)
	// The cache already holds the mutation (m is the live cached pointer);
	// refresh the signature to the just-written file so the next read serves
	// the cache instead of reparsing. A failed write leaves the file (and thus
	// the signature) unchanged, and the mutated in-memory entry stays
	// authoritative for this process — matching the best-effort contract.
	renderManifestCacheSig = renderManifestStatSig(path)
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

// backupSuffix is the sibling suffix dot-agents appends to a genuine user file
// it preserves before installing a managed link over the owned path (the
// established repo convention: <path>.dot-agents-backup).
const backupSuffix = ".dot-agents-backup"

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
	bak := dst + backupSuffix
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
