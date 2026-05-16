package agents

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// TestEnsureImportRepoAgentsSlot_ReadlinkErrorSeam covers the defensive
// os.Readlink error branch in ensureImportRepoAgentsSlot. The branch follows
// a successful os.Lstat that already confirmed the path is a symlink, so the
// only way to exercise it is to swap the package-level osReadlink seam.
func TestEnsureImportRepoAgentsSlot_ReadlinkErrorSeam(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: needs a real symlink so os.Lstat reports os.ModeSymlink, no managed-link analogue")
	}
	agentsHome, projectPath := testutil.NewTempProject(t, "seamproj")
	canonical := testutil.WriteCanonicalAgent(t, agentsHome, "seamproj", "seam-agent")

	// Set up a real symlink so Lstat reports a symlink, then force Readlink
	// to fail through the seam.
	repoLocal := filepath.Join(projectPath, ".agents", "agents", "seam-agent")
	if err := os.MkdirAll(filepath.Dir(repoLocal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, repoLocal); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("synthetic readlink failure")
	t.Cleanup(func() { osReadlink = os.Readlink })
	osReadlink = func(string) (string, error) { return "", sentinel }

	err := ensureImportRepoAgentsSlot("seam-agent", canonical, projectPath)
	if err == nil {
		t.Fatal("expected error from osReadlink seam")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v; want wrapped sentinel", err)
	}
	if !strings.Contains(err.Error(), "reading symlink") {
		t.Errorf("error = %q; want context about reading symlink", err.Error())
	}
}

// TestCleanupManagedAgentRepoPath_ReadlinkErrorSeam covers the defensive
// os.Readlink error branch in cleanupManagedAgentRepoPath. The Lstat call
// has just confirmed the path is a symlink, so the branch is otherwise
// unreachable without a TOCTOU race.
func TestCleanupManagedAgentRepoPath_ReadlinkErrorSeam(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: needs a real symlink so os.Lstat reports os.ModeSymlink, no managed-link analogue")
	}
	agentsHome, projectPath := testutil.NewTempProject(t, "rmseamproj")

	// Create an unmanaged symlink pointing outside agentsHome so the
	// pre-cleanup links.RemoveIfSymlinkUnder leaves it in place.
	target := filepath.Join(projectPath, "external-target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(projectPath, "unmanaged-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("synthetic readlink failure")
	t.Cleanup(func() { osReadlink = os.Readlink })
	osReadlink = func(string) (string, error) { return "", sentinel }

	d := stubDeps(false)
	err := cleanupManagedAgentRepoPath(d, link, agentsHome, "rm-seam")
	if err == nil {
		t.Fatal("expected error from osReadlink seam")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v; want sentinel", err)
	}
}
