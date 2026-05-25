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

// fakeReadlinker is the agents-package readlinker fake. Its nil readlink
// field delegates to os.Readlink (the nil-delegates-to-real convention from
// docs/TEST_SEAMS.md), so a test only has to define the failure point it
// wants to exercise.
type fakeReadlinker struct {
	readlink func(name string) (string, error)
}

func (f *fakeReadlinker) Readlink(name string) (string, error) {
	if f.readlink != nil {
		return f.readlink(name)
	}
	return os.Readlink(name)
}

// newFakeReadlinkerError returns a fake whose Readlink always returns err.
func newFakeReadlinkerError(err error) *fakeReadlinker {
	return &fakeReadlinker{
		readlink: func(string) (string, error) { return "", err },
	}
}

// TestEnsureImportRepoAgentsSlot_ReadlinkErrorSeam covers the defensive
// os.Readlink error branch in ensureImportRepoAgentsSlot. The branch follows
// a successful os.Lstat that already confirmed the path is a symlink, so the
// only way to exercise it is to inject a fake readlinker that returns an
// error.
func TestEnsureImportRepoAgentsSlot_ReadlinkErrorSeam(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: needs a real symlink so os.Lstat reports os.ModeSymlink, no managed-link analogue")
	}
	agentsHome, projectPath := testutil.NewTempProject(t, "seamproj")
	canonical := testutil.WriteCanonicalAgent(t, agentsHome, "seamproj", "seam-agent")

	// Symlink to a path OTHER than canonical so links.IsManagedLink is
	// false (the happy-path short-circuit does not fire) and control
	// reaches the mispoint branch, where the injected readlinker is
	// invoked to build the error message — the seam under test.
	repoLocal := filepath.Join(projectPath, ".agents", "agents", "seam-agent")
	if err := os.MkdirAll(filepath.Dir(repoLocal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(projectPath, "elsewhere"), repoLocal); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("synthetic readlink failure")
	rl := newFakeReadlinkerError(sentinel)

	err := ensureImportRepoAgentsSlot(rl, "seam-agent", canonical, projectPath)
	if err == nil {
		t.Fatal("expected error from injected readlinker")
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
	rl := newFakeReadlinkerError(sentinel)

	d := stubDeps(false)
	err := cleanupManagedAgentRepoPath(d, rl, link, agentsHome, "rm-seam")
	if err == nil {
		t.Fatal("expected error from injected readlinker")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v; want sentinel", err)
	}
}

// TestStdReadlinker_DelegatesToOSReadlink covers the production stdReadlinker
// happy path — it must delegate to os.Readlink and surface real targets/errors.
func TestStdReadlinker_DelegatesToOSReadlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: needs a real symlink to assert Readlink target equality")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real-target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "the-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := stdReadlinker{}.Readlink(link)
	if err != nil {
		t.Fatalf("stdReadlinker.Readlink(link): %v", err)
	}
	if got != target {
		t.Errorf("Readlink = %q; want %q", got, target)
	}

	_, err = stdReadlinker{}.Readlink(filepath.Join(dir, "does-not-exist"))
	if err == nil {
		t.Fatal("expected error reading nonexistent path")
	}
}
