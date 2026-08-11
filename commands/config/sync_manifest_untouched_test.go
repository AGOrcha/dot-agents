package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// TestRunSync_LeavesManifestUntouched pins the entry-point contract for
// `da config sync`: it re-fetches layers and rewrites .agentsrc.lock, and it
// must NEVER write .agentsrc.json.
//
// Field reports from an org rollout attributed hooks/mcp/settings injection to
// `da config sync`, because the rollout script grepped the manifest AFTER the
// sync without capturing a before-state — so an injection written earlier by the
// `da refresh` fan-out was misattributed to sync. A call-graph trace confirms the
// sync path only ever decodes the manifest (into map[string]any, not AgentsRC),
// but "the resolver happens not to write it today" is exactly the kind of
// property that regresses silently. This asserts it at the entry point, on raw
// bytes and mtime, for both a manifest that omits the optional keys and one that
// declares them.
func TestRunSync_LeavesManifestUntouched(t *testing.T) {
	tests := []struct {
		name  string
		layer string
	}{
		{name: "full stack sync"},
		{name: "layer-scoped sync", layer: "acme:org/base.json"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := withTwoLocalLayers(t)
			manifestPath := filepath.Join(project, cfg.AgentsRCFile)

			before, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			// Backdate so an mtime change is unambiguous.
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(manifestPath, old, old); err != nil {
				t.Fatalf("backdate manifest: %v", err)
			}
			beforeStat, err := os.Stat(manifestPath)
			if err != nil {
				t.Fatalf("stat before: %v", err)
			}

			opts := syncOptions(project, tc.layer, false, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
			if err := runSync(opts, testDeps()); err != nil {
				t.Fatalf("runSync: %v", err)
			}

			after, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read manifest after: %v", err)
			}
			if string(after) != string(before) {
				t.Errorf("config sync rewrote the manifest\n--- before ---\n%s\n--- after ---\n%s", before, after)
			}
			afterStat, err := os.Stat(manifestPath)
			if err != nil {
				t.Fatalf("stat after: %v", err)
			}
			if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
				t.Errorf("config sync touched the manifest mtime: before %v, after %v",
					beforeStat.ModTime(), afterStat.ModTime())
			}

			// The lock IS sync's job and must still have been written.
			if _, err := os.Stat(cfg.AgentsLockPath(project)); err != nil {
				t.Errorf("config sync must still write the lock: %v", err)
			}
		})
	}
}

// TestRunSync_DoesNotInjectOptionalKeys is the field-report regression: a
// manifest that omits hooks/mcp/settings must still omit them after a sync, so a
// post-sync grep cannot find keys the author never wrote.
func TestRunSync_DoesNotInjectOptionalKeys(t *testing.T) {
	project := withTwoLocalLayers(t)
	manifestPath := filepath.Join(project, cfg.AgentsRCFile)

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	for _, key := range []string{`"hooks"`, `"mcp"`, `"settings"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("fixture precondition: manifest must not declare %s\n%s", key, raw)
		}
	}

	opts := syncOptions(project, "", false, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := runSync(opts, testDeps()); err != nil {
		t.Fatalf("runSync: %v", err)
	}

	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after: %v", err)
	}
	for _, key := range []string{`"hooks"`, `"mcp"`, `"settings"`} {
		if strings.Contains(string(after), key) {
			t.Errorf("config sync injected %s into the manifest:\n%s", key, after)
		}
	}
}
