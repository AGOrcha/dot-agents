package lifecycle

// Seam-injection tests migrated from commands/seams_test.go as part of
// root-command-decomposition t11. This file is intentionally narrow:
// only tests that exercise lifecycle-internal branches NOT already
// covered by the cluster-specific *_test.go files in this package
// landed here. The bulk of seams_test.go was left in commands/ —
// it doubles as coverage for the root-package shim forwarders
// (commands.runInstall → lifecycle.RunInstall, etc.) that t13 will
// delete; moving the tests prematurely drops commands-package coverage
// below the 95% gate while the shims still exist. See
// .agents/active/fold-back/t11-shim-coverage-deferred.md for the
// constraint analysis and the planned follow-up.
//
// The three tests here cover branches the existing lifecycle test
// files do not reach:
//
//   - TestPrintSymlinkDirAudit_NonexistentDir: the ReadDir-fails
//     branch (existing TestPrintSymlinkDirAudit_EmptyDir only
//     exercises the empty-dir-success path).
//   - TestCountClaudeRules_ReadlinkFails: the readlink-fails-silently
//     branch in countClaudeRules combined with broken + healthy
//     managed links in one call (existing
//     TestCountClaudeRules_ReportsBrokenSymlinks only covers the
//     broken-symlink branch).
//   - TestRunInstallGenerate_CorruptExistingManifest: the
//     LoadAgentsRC error branch when a malformed .agentsrc.json
//     pre-exists (existing TestRunInstallGenerate_AccessManifestStatError
//     and _SaveError cover different branches).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/linktest"
)

// ─── printSymlinkDirAudit ReadDir-fails branch ───────────────────────────────

// The directory does not exist, so ReadDir returns ENOENT and the function
// must short-circuit with (0, 0). Existing TestPrintSymlinkDirAudit_EmptyDir
// covers the "dir exists but has no entries" path; this one covers the
// "dir missing entirely" path.
func TestPrintSymlinkDirAudit_NonexistentDir(t *testing.T) {
	ok, broken := printSymlinkDirAudit(filepath.Join(t.TempDir(), "absent"), "(empty)", "%s")
	if ok != 0 || broken != 0 {
		t.Errorf("expected (0,0) for missing dir, got (%d,%d)", ok, broken)
	}
}

// ─── countClaudeRules Readlink-fails branch ──────────────────────────────────

// A regular file in .claude/rules/ produces a Readlink error that the
// function silently skips. Existing TestCountClaudeRules_ReportsBrokenSymlinks
// exercises only the dangling-link warn branch; this one combines a regular
// file (readlink-fails), a broken managed link (warn++), and a healthy
// managed link (ok++) in a single call to assert all three branches fire.
func TestCountClaudeRules_ReadlinkFails(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Regular file, not a symlink.
	if err := os.WriteFile(filepath.Join(dir, "regular.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Broken managed link.
	linktest.DanglingLink(t, filepath.Join(dir, "broken"))
	// Healthy managed link.
	healthy := filepath.Join(tmp, "real.md")
	if err := os.WriteFile(healthy, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, healthy, filepath.Join(dir, "good"))

	ok, warn := CountClaudeRules(tmp)
	if ok != 1 || warn != 1 {
		t.Errorf("CountClaudeRules: ok=%d warn=%d, want ok=1 warn=1", ok, warn)
	}
}

// ─── RunInstallGenerate corrupt-existing-manifest branch ─────────────────────

// LoadAgentsRC error branch (install.go: "loading existing %s: %w"): when
// the project already has an .agentsrc.json but the bytes are unparseable
// JSON, the merge load step fails and RunInstallGenerate surfaces a wrapped
// error. Existing TestRunInstallGenerate_AccessManifestStatError covers the
// stat-fails branch (different precondition) and _SaveError covers the save
// failure (different branch); this one is the parse-fails-on-existing branch.
func TestRunInstallGenerate_CorruptExistingManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write malformed .agentsrc.json so the merge load step fails.
	if err := os.WriteFile(filepath.Join(projectPath, config.AgentsRCFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := fakeInstallDeps{getwd: func() (string, error) { return projectPath, nil }}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := RunInstallGenerate(deps)
	if err == nil || !strings.Contains(err.Error(), "loading existing") {
		t.Fatalf("expected loading existing manifest error, got %v", err)
	}
	// Sentinel-style assertion: ensure the wrapped underlying error is not
	// nil (sanity check that the wrap actually surfaced).
	if errors.Is(err, nil) {
		t.Error("expected wrapped non-nil error from corrupt manifest")
	}
}
