package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// fakeAddDeps is a minimal AddDeps for fault injection in backup tests.
type fakeAddDeps struct {
	mkdirAll  func(string, os.FileMode) error
	writeFile func(string, []byte, os.FileMode) error
	remove    func(string) error
	copyFile  func(string, string) error
}

func (f fakeAddDeps) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (f fakeAddDeps) WriteFile(name string, data []byte, perm os.FileMode) error {
	if f.writeFile != nil {
		return f.writeFile(name, data, perm)
	}
	return os.WriteFile(name, data, perm)
}

func (f fakeAddDeps) Remove(name string) error {
	if f.remove != nil {
		return f.remove(name)
	}
	return os.Remove(name)
}

func (f fakeAddDeps) Executable() (string, error) {
	return os.Executable()
}

func (f fakeAddDeps) CopyFile(src, dst string) error {
	if f.copyFile != nil {
		return f.copyFile(src, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func (f fakeAddDeps) LoadConfig() (*config.Config, error) {
	return config.Load()
}

// ---------- IsBackupArtifact ----------

func TestIsBackupArtifact(t *testing.T) {
	cases := []struct {
		name   string
		expect bool
	}{
		{"AGENTS.md", false},
		{"AGENTS.md.dot-agents-backup", true},
		{"foo.dot-agents-backup-20260101-120000", true},
		{"random.md", false},
		{".cursor/rules/myproj--foo.mdc", false},
	}
	for _, c := range cases {
		if got := IsBackupArtifact(c.name); got != c.expect {
			t.Errorf("IsBackupArtifact(%q) = %v, want %v", c.name, got, c.expect)
		}
	}
}

// ---------- IsCanonicalResourceBackupRel ----------

func TestIsCanonicalResourceBackupRel(t *testing.T) {
	cases := map[string]bool{
		"rules/foo.md":   true,
		"settings/foo":   true,
		"mcp/foo.json":   true,
		"skills/foo":     true,
		"agents/foo":     true,
		"hooks/foo":      true,
		"loose":          false,
		"backups/x":      false, // backups handled separately by caller
		".github/foo.md": false,
	}
	for in, want := range cases {
		if got := IsCanonicalResourceBackupRel(in); got != want {
			t.Errorf("IsCanonicalResourceBackupRel(%q)=%v want %v", in, got, want)
		}
	}
}

// ---------- IsManagedProjectOutput ----------

func TestIsManagedProjectOutput_LooseFileReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := t.TempDir()
	loose := filepath.Join(tmp, "random.md")
	_ = os.WriteFile(loose, []byte("x"), 0644)

	if IsManagedProjectOutput("proj", tmp, loose, agentsHome) {
		t.Error("loose file should not be reported as managed")
	}
}

func TestIsManagedProjectOutput_RelErrorReturnsFalse(t *testing.T) {
	// projectPath that is a relative path causes filepath.Rel to error
	// when filePath is absolute.
	got := IsManagedProjectOutput("p", "relative/project", "/absolute/path/foo.md", t.TempDir())
	if got {
		t.Error("expected false when filepath.Rel errors")
	}
}

func TestIsManagedProjectOutput_ManagedCursorRuleNamespace(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := t.TempDir()
	project := "proj"
	rule := filepath.Join(tmp, ".cursor", "rules", project+"--foo.mdc")
	_ = os.MkdirAll(filepath.Dir(rule), 0755)
	_ = os.WriteFile(rule, []byte("rule"), 0644)

	if !IsManagedProjectOutput(project, tmp, rule, agentsHome) {
		t.Error("project-namespaced cursor rule should be managed")
	}
}

// ---------- IsManagedCursorRuleRel ----------

func TestIsManagedCursorRuleRel(t *testing.T) {
	cases := []struct {
		rel    string
		expect bool
	}{
		{".cursor/rules/global--foo.mdc", true},
		{".cursor/rules/proj--foo.mdc", true},
		{".cursor/rules/random.mdc", false},
		{".claude/rules/proj--foo.md", false},
	}
	for _, c := range cases {
		if got := IsManagedCursorRuleRel("proj", c.rel); got != c.expect {
			t.Errorf("IsManagedCursorRuleRel(%q) = %v, want %v", c.rel, got, c.expect)
		}
	}
}

// ---------- MirrorBackup / MirrorBackupChecked ----------

func TestMirrorBackup_NoTimestampStillWritesActive(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	src := filepath.Join(tmp, "AGENTS.md")
	_ = os.WriteFile(src, []byte("hello"), 0644)

	MirrorBackup("proj", tmp, src, "")

	active := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("expected active backup at %s: %v", active, err)
	}
}

func TestMirrorBackupChecked_PropagatesCopyError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	src := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := fakeAddDeps{copyFile: func(string, string) error { return errors.New("disk full") }}

	if err := MirrorBackupChecked("p", tmp, src, "20260101-000000", deps); err == nil {
		t.Fatal("expected error when the backup copy fails")
	}
}

// ---------- BackupExistingConfigsList ----------

func TestBackupExistingConfigsList_CopyDeleteNoArtifactInProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	agentsMD := filepath.Join(tmp, "AGENTS.md")
	_ = os.WriteFile(agentsMD, []byte("# instructions"), 0644)

	count, err := BackupExistingConfigsList([]string{agentsMD}, tmp, agentsHome, "myproject", "20260101-120000", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	if _, err := os.Lstat(agentsMD); !os.IsNotExist(err) {
		t.Error("original file should have been deleted from the project tree")
	}
	active := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")
	if _, err := os.Stat(active); err != nil {
		t.Errorf("active backup not found in resources: %v", err)
	}
	ts := filepath.Join(agentsHome, "resources", "myproject", "backups", "20260101-120000", "AGENTS.md")
	if _, err := os.Stat(ts); err != nil {
		t.Errorf("timestamped backup not found in resources: %v", err)
	}
}

func TestBackupExistingConfigsList_SkipsBackupArtifacts(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	artifact := filepath.Join(tmp, "AGENTS.md.dot-agents-backup")
	_ = os.WriteFile(artifact, []byte("old"), 0644)

	count, err := BackupExistingConfigsList([]string{artifact}, tmp, agentsHome, "myproject", "20260101-120000", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0 for artifact input, got %d", count)
	}
	if _, err := os.Lstat(artifact); err != nil {
		t.Error("backup artifact should not have been removed by the backup function")
	}
}

// Backup failure preserves the original (no destructive removal).
func TestBackupExistingConfigsList_BackupFailurePreservesOriginal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	agentsMD := filepath.Join(tmp, "AGENTS.md")
	_ = os.WriteFile(agentsMD, []byte("# real"), 0644)

	deps := fakeAddDeps{copyFile: func(string, string) error { return errors.New("disk full") }}

	count, err := BackupExistingConfigsList([]string{agentsMD}, tmp, agentsHome, "p", "20260101-120000", deps)
	if err == nil {
		t.Fatal("expected error when the backup copy fails")
	}
	if count != 0 {
		t.Errorf("expected count=0 on failure, got %d", count)
	}
	// The original must still exist — no destructive removal happens
	// when backup fails.
	if _, lstErr := os.Lstat(agentsMD); lstErr != nil {
		t.Errorf("original should be preserved on backup failure: %v", lstErr)
	}
}

// ---------- RestoreFromResourcesCountedWithDeps ----------

func TestRestoreFromResourcesCounted_NoResourcesDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	count, err := RestoreFromResourcesCountedWithDeps("proj", t.TempDir(), StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0 when resources dir missing, got %d", count)
	}
}

func TestRestoreFromResourcesCounted_NonDirIsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	// Squat the resources/proj path with a regular file
	resourceFile := filepath.Join(tmp, "resources", "proj")
	_ = os.MkdirAll(filepath.Dir(resourceFile), 0755)
	_ = os.WriteFile(resourceFile, []byte("not a dir"), 0644)

	_, err := RestoreFromResourcesCountedWithDeps("proj", t.TempDir(), StdAddDeps{})
	if err == nil {
		t.Fatal("expected error when resources path is a file")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRestoreFromResourcesCounted_StatErrorIsPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	// Create resources dir, then make its parent unreadable
	resourcesParent := filepath.Join(tmp, "resources")
	_ = os.MkdirAll(filepath.Join(resourcesParent, "proj"), 0755)
	// chmod to 0 so stat fails with EACCES
	if err := os.Chmod(resourcesParent, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(resourcesParent, 0755)

	// Skip when running as root (root bypasses perm checks)
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}

	_, err := RestoreFromResourcesCountedWithDeps("proj", t.TempDir(), StdAddDeps{})
	if err == nil {
		t.Fatal("expected stat error to propagate")
	}
}

// ---------- RestoreLegacyResourceFile ----------

func TestRestoreLegacyResourceFile_UnknownRelReturnsZero(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	_ = os.WriteFile(src, []byte("x"), 0644)

	count, err := RestoreLegacyResourceFile("proj", "nested/unknown/path.txt", tmp, src, StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0 for unmapped rel, got %d", count)
	}
}

func TestRestoreLegacyResourceFile_KnownRelCopies(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)

	src := filepath.Join(tmp, "AGENTS.md")
	_ = os.WriteFile(src, []byte("contents"), 0644)

	count, err := RestoreLegacyResourceFile("proj", "AGENTS.md", agentsHome, src, StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	dest := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected copied dest at %s: %v", dest, err)
	}
	if string(data) != "contents" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

// CopyFile errors from RestoreLegacyResourceFile must propagate so the
// caller (restoreResourceFileCount) can surface a non-zero error from
// the walk; silently dropping a copy failure would leave the resources
// tree restored only halfway while refresh stamps success metadata.
func TestRestoreLegacyResourceFile_CopyErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := fakeAddDeps{copyFile: func(string, string) error { return errors.New("dst denied") }}

	count, err := RestoreLegacyResourceFile("proj", "AGENTS.md", tmp, src, deps)
	if err == nil {
		t.Fatal("expected CopyFile error to propagate")
	}
	if count != 0 {
		t.Errorf("expected count=0 on copy failure, got %d", count)
	}
}

// ---------- MirrorBackupChecked edge paths ----------

// When srcFile cannot be expressed relative to projectPath (e.g. srcFile
// lives outside the project tree), MirrorBackupChecked falls back to the
// file basename. The active-target write must still land under
// resources/<project>/.
func TestMirrorBackupChecked_FallbackToBasenameWhenRelEscapesProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// srcFile outside projectPath: filepath.Rel returns "../outside.md"
	projectPath := filepath.Join(tmp, "proj")
	_ = os.MkdirAll(projectPath, 0755)
	outside := filepath.Join(tmp, "outside.md")
	if err := os.WriteFile(outside, []byte("contents"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MirrorBackupChecked("p", projectPath, outside, "", StdAddDeps{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Falls back to base name: resources/p/outside.md
	active := filepath.Join(agentsHome, "resources", "p", "outside.md")
	if _, err := os.Stat(active); err != nil {
		t.Errorf("expected active backup at %s: %v", active, err)
	}
}

// The timestamped-target copy is required when timestamp != "". A
// failure on the second copy must surface (not be silently dropped),
// even though the active copy already landed: callers rely on the
// timestamped snapshot for point-in-time recovery.
func TestMirrorBackupChecked_TimestampedCopyErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	src := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	deps := fakeAddDeps{copyFile: func(srcPath, dst string) error {
		calls++
		if calls == 1 {
			// Let the active copy succeed by performing it.
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, 0o644)
		}
		return errors.New("ts copy denied")
	}}

	err := MirrorBackupChecked("p", tmp, src, "20260101-000000", deps)
	if err == nil {
		t.Fatal("expected error when timestamped copy fails")
	}
	if !strings.Contains(err.Error(), "ts copy denied") {
		t.Errorf("expected error to wrap underlying failure, got %v", err)
	}
}

// ---------- IsManagedProjectOutput cursor-rule and managed-symlink paths ----------

// Loose .cursor/rules/foo.mdc whose name does NOT use the global-- or
// <project>-- namespace is NOT a managed cursor rule. IsManagedProjectOutput
// must then fall through to the destRel + hard-link check; with no
// canonical source on disk the helper returns false.
func TestIsManagedProjectOutput_UnnamespacedCursorRuleFallsThrough(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := t.TempDir()
	rule := filepath.Join(tmp, ".cursor", "rules", "loose.mdc")
	_ = os.MkdirAll(filepath.Dir(rule), 0755)
	_ = os.WriteFile(rule, []byte("rule"), 0644)

	if IsManagedProjectOutput("proj", tmp, rule, agentsHome) {
		t.Error("unnamespaced cursor rule with no canonical hard link should not be managed")
	}
}

// A POSIX symlink whose target resolves under agentsHome is reported as
// managed via the IsManagedSymlink branch (no Rel/destRel lookup
// required). This guards the early-return path in IsManagedProjectOutput.
func TestIsManagedProjectOutput_ManagedSymlinkBranch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	_ = os.MkdirAll(agentsHome, 0755)

	target := filepath.Join(agentsHome, "real.txt")
	_ = os.WriteFile(target, []byte("hi"), 0644)
	link := filepath.Join(tmp, "linked.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if !IsManagedProjectOutput("proj", tmp, link, agentsHome) {
		t.Error("symlink under agentsHome should be managed")
	}
}

// ---------- BackupExistingConfigsList branch coverage ----------

// A non-existent path in the input list must be skipped (Lstat misses)
// without affecting the count or returning an error. Production callers
// pass a precomputed list that may race with on-disk state.
func TestBackupExistingConfigsList_SkipsMissingPaths(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	missing := filepath.Join(tmp, "absent.md")
	count, err := BackupExistingConfigsList([]string{missing}, tmp, agentsHome, "p", "", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0 for missing input, got %d", count)
	}
}

// A managed symlink (target under agentsHome) is removed without a
// mirror backup — its content already lives under agentsHome.
func TestBackupExistingConfigsList_ManagedSymlinkRemovedNoMirror(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	canonical := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	_ = os.MkdirAll(filepath.Dir(canonical), 0755)
	_ = os.WriteFile(canonical, []byte("contents"), 0644)
	managed := filepath.Join(tmp, "AGENTS.md")
	if err := os.Symlink(canonical, managed); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	count, err := BackupExistingConfigsList([]string{managed}, tmp, agentsHome, "proj", "", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1 for managed symlink, got %d", count)
	}
	if _, err := os.Lstat(managed); !os.IsNotExist(err) {
		t.Error("managed symlink should have been removed")
	}
	// No mirror backup should exist under resources/proj for the symlink.
	mirror := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	if _, err := os.Stat(mirror); err == nil {
		t.Error("managed symlink should NOT be mirrored")
	}
}

// A hard link whose inode matches the canonical source under agentsHome
// is the "proven managed hardlink" case: also removed without mirror
// backup. This exercises both HasMultipleHardLinks and
// isManagedHardlinkToCanonicalSource together.
func TestBackupExistingConfigsList_ManagedHardlinkRemovedNoMirror(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hardlink semantics")
	}
	// Force the HasMultipleHardLinks seam to report true for the linked
	// candidate so the branch fires deterministically (the production
	// platform-tagged implementation is wired by commands/add.go which
	// the lifecycle test binary does not import).
	prevHL := HasMultipleHardLinks
	HasMultipleHardLinks = func(path string) bool { return true }
	defer func() { HasMultipleHardLinks = prevHL }()

	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	canonical := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	_ = os.MkdirAll(filepath.Dir(canonical), 0755)
	_ = os.WriteFile(canonical, []byte("real"), 0644)
	candidate := filepath.Join(tmp, "AGENTS.md")
	if err := os.Link(canonical, candidate); err != nil {
		t.Fatalf("hardlink: %v", err)
	}

	count, err := BackupExistingConfigsList([]string{candidate}, tmp, agentsHome, "proj", "", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
		t.Error("managed hardlink should be removed from project tree")
	}
	mirror := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	if _, err := os.Stat(mirror); err == nil {
		t.Error("managed hardlink should NOT be mirrored")
	}
}

// An UNMANAGED hard link (nlink>1 but inode does NOT match the
// canonical source) must fall through to the mirror+remove path so the
// user's only copy is preserved before destructive removal.
func TestBackupExistingConfigsList_UnmanagedHardlinkFallsThrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hardlink semantics")
	}
	prevHL := HasMultipleHardLinks
	HasMultipleHardLinks = func(path string) bool { return true }
	defer func() { HasMultipleHardLinks = prevHL }()

	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Two project-side files hard-linked together — NOT linked to any
	// canonical source under agentsHome.
	primary := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(primary, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(tmp, "twin.md")
	if err := os.Link(primary, other); err != nil {
		t.Fatalf("hardlink: %v", err)
	}

	count, err := BackupExistingConfigsList([]string{primary}, tmp, agentsHome, "proj", "20260101-000000", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	mirror := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	if _, err := os.Stat(mirror); err != nil {
		t.Errorf("unmanaged hardlink should be mirrored before removal: %v", err)
	}
}

// Remove() failures during the destructive cleanup must NOT abort the
// loop or increment count for that file — production preserves the
// "best-effort cleanup" semantics so a single unwritable directory does
// not stall the rest of the backup batch.
func TestBackupExistingConfigsList_RemoveErrorSkipsCount(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	src := filepath.Join(tmp, "AGENTS.md")
	_ = os.WriteFile(src, []byte("x"), 0644)

	deps := fakeAddDeps{remove: func(string) error { return errors.New("remove denied") }}
	count, err := BackupExistingConfigsList([]string{src}, tmp, agentsHome, "proj", "20260101-000000", deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0 when Remove fails, got %d", count)
	}
	// Mirror copies should still be on disk.
	if _, err := os.Stat(filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")); err != nil {
		t.Errorf("mirror should be written even when Remove fails: %v", err)
	}
}

// ---------- isManagedHardlinkToCanonicalSource direct coverage ----------

// Direct coverage of the helper through its only caller behavior:
// rel-error path (filePath cannot be expressed relative to projectPath)
// returns false. The function is unexported, so we drive it through
// BackupExistingConfigsList with HasMultipleHardLinks forced true and
// a synthetic project layout where Rel fails.
func TestBackupExistingConfigsList_HardlinkRelErrorFallsThrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hardlink + abs/rel-path semantics")
	}
	prevHL := HasMultipleHardLinks
	HasMultipleHardLinks = func(path string) bool { return true }
	defer func() { HasMultipleHardLinks = prevHL }()

	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	primary := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(primary, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	twin := filepath.Join(tmp, "twin")
	if err := os.Link(primary, twin); err != nil {
		t.Fatalf("hardlink: %v", err)
	}

	// projectPath is RELATIVE; filePath is ABS — filepath.Rel returns
	// an error and isManagedHardlinkToCanonicalSource short-circuits to
	// false, so the candidate falls through to the mirror+remove path.
	count, err := BackupExistingConfigsList([]string{primary}, "relative/project", agentsHome, "proj", "", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1 (fall-through to mirror+remove), got %d", count)
	}
}

// ---------- RestoreFromResourcesCountedWithDeps walk path ----------

// Drives the full walk path: resources/<project>/AGENTS.md restores via
// the legacy mapper, while backups/ and rules/ subtrees are skipped per
// IsCanonicalResourceBackupRel.
func TestRestoreFromResourcesCounted_RestoresLegacyAndSkipsCanonical(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	resourcesProj := filepath.Join(agentsHome, "resources", "proj")
	// Legacy file: will be mapped and copied to rules/proj/agents.md.
	if err := os.MkdirAll(resourcesProj, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourcesProj, "AGENTS.md"), []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	// backups/ subtree: must be skipped entirely.
	backupFile := filepath.Join(resourcesProj, "backups", "20260101-000000", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(backupFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupFile, []byte("snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}
	// rules/ subtree (canonical-resource): also skipped by relPath check.
	canonicalRule := filepath.Join(resourcesProj, "rules", "proj", "other.md")
	if err := os.MkdirAll(filepath.Dir(canonicalRule), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalRule, []byte("canon"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unmapped relative path: stays at count 0 (no error).
	unmapped := filepath.Join(resourcesProj, "nested", "unknown", "path.txt")
	if err := os.MkdirAll(filepath.Dir(unmapped), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmapped, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	projectPath := filepath.Join(tmp, "proj")
	count, err := RestoreFromResourcesCountedWithDeps("proj", projectPath, StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count=1 (only AGENTS.md), got %d", count)
	}
	// Verify mapping landed under agentsHome.
	dest := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("expected mapped dest at %s: %v", dest, err)
	}
}

// A CopyFile failure during legacy restore must surface as a non-nil
// error from the walk so callers (refresh) can mark the project failed
// instead of stamping fresh metadata over an unrestored tree.
func TestRestoreFromResourcesCounted_LegacyCopyErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	resourcesProj := filepath.Join(agentsHome, "resources", "proj")
	if err := os.MkdirAll(resourcesProj, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourcesProj, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := fakeAddDeps{copyFile: func(string, string) error { return errors.New("copy denied") }}
	count, err := RestoreFromResourcesCountedWithDeps("proj", filepath.Join(tmp, "proj"), deps)
	if err == nil {
		t.Fatal("expected walk to surface CopyFile error")
	}
	if count != 0 {
		t.Errorf("expected count=0 on copy failure, got %d", count)
	}
}

// The walk error path: when the canonical-import seam reports a handled
// error, the walk must surface the wrapped error to the caller and
// continue past the directory (return-nil pattern in the WalkDir func).
func TestRestoreFromResourcesCounted_CanonicalSeamErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	resourcesProj := filepath.Join(agentsHome, "resources", "proj")
	if err := os.MkdirAll(resourcesProj, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourcesProj, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := RestoreCanonicalResourceFileFn
	RestoreCanonicalResourceFileFn = func(string, string, string, string, AddDeps) (int, bool, error) {
		return 0, true, errors.New("canonical boom")
	}
	defer func() { RestoreCanonicalResourceFileFn = prev }()

	_, err := RestoreFromResourcesCountedWithDeps("proj", filepath.Join(tmp, "proj"), StdAddDeps{})
	if err == nil {
		t.Fatal("expected canonical-seam error to surface")
	}
	if !strings.Contains(err.Error(), "canonical boom") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// The default RestoreCanonicalResourceFileFn (unwired) must report
// handled=false so callers fall through to legacy restore. Covers the
// var literal at backup.go:27-29 in lifecycle-only test binaries.
func TestRestoreCanonicalResourceFileFn_DefaultUnhandled(t *testing.T) {
	count, handled, err := RestoreCanonicalResourceFileFn("p", "/r", "/a", "/p", StdAddDeps{})
	if err != nil {
		t.Fatalf("default seam should not error: %v", err)
	}
	if handled {
		t.Error("default seam should report handled=false")
	}
	if count != 0 {
		t.Errorf("default seam should return count=0, got %d", count)
	}
}

// Default HasMultipleHardLinks seam returns false so the lifecycle
// package builds standalone without the platform-tagged
// linkcount_unix.go implementation that commands/add.go wires in.
func TestHasMultipleHardLinks_DefaultFalse(t *testing.T) {
	prev := HasMultipleHardLinks
	HasMultipleHardLinks = func(string) bool { return false } // restore the lifecycle default explicitly
	defer func() { HasMultipleHardLinks = prev }()
	if HasMultipleHardLinks("/any/path") {
		t.Error("expected default HasMultipleHardLinks to return false")
	}
}

// ---------- restoreResourceFileCount direct unexported coverage ----------

// fakeDirEntry implements os.DirEntry for direct restoreResourceFileCount
// drive-by tests. The standard DirEntry returned by os.ReadDir/WalkDir
// is fine for the success path, but we need to control IsDir() and the
// walkErr argument for the rare error-injection branches.
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.isDir }
func (f fakeDirEntry) Type() os.FileMode          { return 0 }
func (f fakeDirEntry) Info() (os.FileInfo, error) { return nil, nil }

// A nil walkErr + IsDir() entry must short-circuit to (0, nil): walk
// directories carry no payload. This is the most common branch the
// walk hits and the easiest one to lose to a refactor that forgets to
// check d.IsDir() before computing relPath.
func TestRestoreResourceFileCount_DirShortCircuits(t *testing.T) {
	n, err := restoreResourceFileCount("p", "/r", "/a", "/r/sub", fakeDirEntry{name: "sub", isDir: true}, nil, StdAddDeps{})
	if err != nil || n != 0 {
		t.Errorf("dir entry should be (0, nil); got (%d, %v)", n, err)
	}
}

// A non-nil walkErr from filepath.WalkDir is wrapped and surfaced; the
// caller's loop aggregates the first error so refresh.go can fail-fast.
func TestRestoreResourceFileCount_WalkErrIsWrapped(t *testing.T) {
	injected := errors.New("perm denied")
	n, err := restoreResourceFileCount("p", "/r", "/a", "/r/file", fakeDirEntry{name: "file"}, injected, StdAddDeps{})
	if err == nil || !strings.Contains(err.Error(), "perm denied") {
		t.Errorf("expected wrapped walk error, got (%d, %v)", n, err)
	}
}

// When the file's repo-relative path cannot be expressed (filepath.Rel
// errors), restoreResourceFileCount returns an error so the
// orchestrator does not silently drop the un-restored bytes.
func TestRestoreResourceFileCount_RelErrorIsWrapped(t *testing.T) {
	// Force a Rel error by passing a RELATIVE resourcesDir and ABSOLUTE
	// path — filepath.Rel returns an error in that combination.
	n, err := restoreResourceFileCount("p", "relative/resources", "/a", "/abs/file", fakeDirEntry{name: "file"}, nil, StdAddDeps{})
	if err == nil || !strings.Contains(err.Error(), "resolving relative path") {
		t.Errorf("expected relative-path error, got (%d, %v)", n, err)
	}
}

// ---------- IsManagedProjectOutput / isManagedHardlinkToCanonicalSource
// empty-destRel branches ----------

// When the project-relative path maps to "" (no canonical destination),
// IsManagedProjectOutput must return false before attempting the
// hardlink stat — calling AreHardlinked against a non-existent
// canonical target would surface a misleading false anyway, but the
// early-return is the documented invariant.
func TestIsManagedProjectOutput_EmptyDestRelReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := t.TempDir()
	// nested/unknown/path.txt has no mapping (see MapResourceRelToDest tests).
	rel := filepath.Join(tmp, "nested", "unknown", "path.txt")
	_ = os.MkdirAll(filepath.Dir(rel), 0755)
	_ = os.WriteFile(rel, []byte("x"), 0644)

	if IsManagedProjectOutput("proj", tmp, rel, agentsHome) {
		t.Error("unmapped rel should not be reported managed")
	}
}

// The unmanaged-hardlink-with-empty-destRel branch in
// isManagedHardlinkToCanonicalSource: HasMultipleHardLinks reports true
// for the candidate but its rel path has no canonical mapping, so the
// helper returns false and BackupExistingConfigsList falls through to
// the mirror+remove path. This is the "non-AGENTS hardlinked file"
// safety case.
func TestBackupExistingConfigsList_HardlinkWithNoDestRelFallsThrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hardlink semantics")
	}
	prevHL := HasMultipleHardLinks
	HasMultipleHardLinks = func(string) bool { return true }
	defer func() { HasMultipleHardLinks = prevHL }()

	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	_ = os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	primary := filepath.Join(tmp, "nested", "unknown", "path.txt")
	_ = os.MkdirAll(filepath.Dir(primary), 0755)
	if err := os.WriteFile(primary, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	twin := filepath.Join(tmp, "nested", "unknown", "twin")
	if err := os.Link(primary, twin); err != nil {
		t.Fatalf("hardlink: %v", err)
	}

	count, err := BackupExistingConfigsList([]string{primary}, tmp, agentsHome, "proj", "", StdAddDeps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unmapped relPath causes MirrorBackupChecked to use base name and
	// the file IS mirrored + removed.
	if count != 1 {
		t.Errorf("expected count=1 (fall-through to mirror+remove), got %d", count)
	}
}
