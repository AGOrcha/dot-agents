package projectsync_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
)

// TestWriteRefreshToLock_LeavesManifestByteIdentical is the regression guard for
// the manifest-rewrite defect.
//
// `da refresh` used to load-and-re-save .agentsrc.json on every run in order to
// opportunistically strip a legacy top-level "refresh" key. That round trip
// re-serialized the whole manifest, injecting `"hooks": false, "mcp": false,
// "settings": false` into any manifest that had omitted them — which then beat
// the org layer in the key-presence-driven merge and disabled hooks/mcp/settings
// projection. The manifest is user-authored; only the lock is machine-written.
//
// The assertion is deliberately on raw BYTES (and mtime), not on a parsed shape:
// even a cosmetic reformat of a user-authored, version-controlled file is a
// defect, and byte equality is the only assertion that catches it.
func TestWriteRefreshToLock_LeavesManifestByteIdentical(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name: "minimal v2 manifest omitting hooks/mcp/settings",
			manifest: `{
  "version": 2,
  "repo_id": "github.com/acme/fixture",
  "sources": [
    { "id": "acme", "type": "local", "path": "../orglayer" }
  ],
  "extends": [
    "acme:base.json"
  ]
}
`,
		},
		{
			name: "manifest with explicit declarations",
			manifest: `{
  "version": 2,
  "hooks": ["PreToolUse"],
  "mcp": false,
  "sources": [{ "type": "local" }]
}
`,
		},
		{
			name:     "unusual but valid formatting is not reflowed",
			manifest: `{"version":2,"sources":[{"type":"local"}]}`,
		},
		{
			name: "legacy top-level refresh key is NOT stripped by refresh",
			// Stripping this is a migration. It belongs to the explicit,
			// backed-up `da config migrate` path — never to a projection command
			// run as a side effect.
			manifest: `{
  "version": 2,
  "refresh": { "version": "0.0.1" },
  "sources": [{ "type": "local" }]
}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectPath := t.TempDir()
			manifestPath := filepath.Join(projectPath, config.AgentsRCFile)
			if err := os.WriteFile(manifestPath, []byte(tc.manifest), 0644); err != nil {
				t.Fatalf("seeding manifest: %v", err)
			}
			// Backdate so an mtime change is unambiguous rather than racing the
			// filesystem's timestamp granularity.
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(manifestPath, old, old); err != nil {
				t.Fatalf("backdating manifest: %v", err)
			}
			beforeStat, err := os.Stat(manifestPath)
			if err != nil {
				t.Fatalf("stat before: %v", err)
			}

			if err := projectsync.WriteRefreshToLock(projectPath, "1.2.3", "deadbeef", "v1.2.3"); err != nil {
				t.Fatalf("WriteRefreshToLock: %v", err)
			}

			after, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("reading manifest after: %v", err)
			}
			if string(after) != tc.manifest {
				t.Errorf("manifest was rewritten by refresh\n--- before ---\n%s\n--- after ---\n%s", tc.manifest, after)
			}
			afterStat, err := os.Stat(manifestPath)
			if err != nil {
				t.Fatalf("stat after: %v", err)
			}
			if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
				t.Errorf("manifest mtime changed: before %v, after %v — refresh must not write the manifest at all",
					beforeStat.ModTime(), afterStat.ModTime())
			}

			// The lock IS the machine-written artifact and must still be produced.
			if _, err := os.Stat(filepath.Join(projectPath, ".agentsrc.lock")); err != nil {
				t.Errorf("refresh must still write the lock: %v", err)
			}
		})
	}
}

// TestWriteRefreshToLock_DoesNotCreateManifestWhenAbsent pins the other half of
// the policy: refresh must not bootstrap a manifest either. Creating
// .agentsrc.json is `da install --generate`'s explicit job, not a side effect of
// a projection command.
func TestWriteRefreshToLock_DoesNotCreateManifestWhenAbsent(t *testing.T) {
	projectPath := t.TempDir()

	if err := projectsync.WriteRefreshToLock(projectPath, "1.2.3", "deadbeef", "v1.2.3"); err != nil {
		t.Fatalf("WriteRefreshToLock: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectPath, config.AgentsRCFile)); !os.IsNotExist(err) {
		t.Errorf("refresh must not create %s (err=%v)", config.AgentsRCFile, err)
	}
	if _, err := os.Stat(filepath.Join(projectPath, ".agentsrc.lock")); err != nil {
		t.Errorf("refresh must still write the lock: %v", err)
	}
}
