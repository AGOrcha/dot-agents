package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// snapshotEntry captures one path under a root with a hash of its contents
// (or empty hash for directories / symlinks).
type snapshotEntry struct {
	rel  string
	kind string // "dir", "file", "symlink"
	hash string
}

// snapshotTree walks root and records every entry with a deterministic
// signature (path → kind + content hash) so two snapshots can be compared
// byte-for-byte after a second refresh pass.
func snapshotTree(t *testing.T, root string) []snapshotEntry {
	t.Helper()
	var out []snapshotEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		entry := snapshotEntry{rel: rel}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.kind = "symlink"
			if dest, err := os.Readlink(path); err == nil {
				h := sha256.Sum256([]byte(dest))
				entry.hash = hex.EncodeToString(h[:])
			}
		case info.IsDir():
			entry.kind = "dir"
		default:
			entry.kind = "file"
			data, err := os.ReadFile(path)
			if err == nil {
				h := sha256.Sum256(data)
				entry.hash = hex.EncodeToString(h[:])
			}
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

func snapshotsEqual(a, b []snapshotEntry) (string, bool) {
	if len(a) != len(b) {
		return "snapshot length differs", false
	}
	for i := range a {
		if a[i] != b[i] {
			return "entry differs: " + a[i].rel, false
		}
	}
	return "", true
}

// scaffoldRefreshProject seeds ~/.agents/resources/<proj>/ with a small set of
// canonical files refresh's restoreFromResources knows how to map back into
// canonical ~/.agents/ buckets.
func scaffoldRefreshProject(t *testing.T) (tmp, agentsHome, projectPath string) {
	t.Helper()
	tmp = t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	projectPath = filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)

	resources := filepath.Join(agentsHome, "resources", "proj")
	if err := os.MkdirAll(resources, 0755); err != nil {
		t.Fatal(err)
	}
	// A plain top-level AGENTS.md (legacy mapping → rules/proj/agents.md)
	if err := os.WriteFile(filepath.Join(resources, "AGENTS.md"), []byte("# legacy rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A pass-through canonical rules entry
	canonicalRules := filepath.Join(resources, "rules", "proj")
	if err := os.MkdirAll(canonicalRules, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalRules, "extra.md"), []byte("# extra\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Register project + an empty config so runRefresh has something to walk.
	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{},
		Agents:   map[string]config.Agent{},
	}
	cfg.AddProject("proj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return tmp, agentsHome, projectPath
}

// TestRunRefresh_IdempotentAgentsHome verifies that two back-to-back refresh
// passes leave ~/.agents/ byte-equal — refresh must never churn the canonical
// state when nothing has changed.
func TestRunRefresh_IdempotentAgentsHome(t *testing.T) {
	_, agentsHome, _ := scaffoldRefreshProject(t)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh(refreshScope{AllProjects: true}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("first runRefresh: %v", err)
	}
	first := snapshotTree(t, agentsHome)

	if err := runRefresh(refreshScope{AllProjects: true}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("second runRefresh: %v", err)
	}
	second := snapshotTree(t, agentsHome)

	if msg, ok := snapshotsEqual(first, second); !ok {
		t.Fatalf("refresh not idempotent: %s\nfirst=%d entries second=%d entries",
			msg, len(first), len(second))
	}
}

// TestRunRefresh_RestoresDeletedManagedFile checks that when a managed file is
// removed from ~/.agents/, the next refresh re-creates it from resources/.
func TestRunRefresh_RestoresDeletedManagedFile(t *testing.T) {
	_, agentsHome, _ := scaffoldRefreshProject(t)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh(refreshScope{AllProjects: true}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}

	managedRule := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if _, err := os.Stat(managedRule); err != nil {
		t.Fatalf("expected managed rule materialized: %v", err)
	}
	if err := os.Remove(managedRule); err != nil {
		t.Fatalf("delete managed rule: %v", err)
	}

	if err := runRefresh(refreshScope{AllProjects: true}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("repair refresh: %v", err)
	}
	if _, err := os.Stat(managedRule); err != nil {
		t.Errorf("expected refresh to restore deleted managed rule, got: %v", err)
	}
}

// TestRunRefresh_StableProjectFilter verifies path normalization stays stable
// across repeated single-project refreshes — the same project arg should
// produce no drift even when other projects are present in config.
func TestRunRefresh_StableProjectFilter(t *testing.T) {
	tmp, agentsHome, _ := scaffoldRefreshProject(t)

	// Register a second project that doesn't physically exist on disk; refresh
	// must silently skip it without affecting filter-driven runs.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("missing", filepath.Join(tmp, "missing"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh(refreshScope{Project: "proj"}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("first filtered refresh: %v", err)
	}
	first := snapshotTree(t, agentsHome)
	if err := runRefresh(refreshScope{Project: "proj"}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("second filtered refresh: %v", err)
	}
	second := snapshotTree(t, agentsHome)
	if msg, ok := snapshotsEqual(first, second); !ok {
		t.Fatalf("filtered refresh not idempotent: %s", msg)
	}
}
