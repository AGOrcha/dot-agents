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
