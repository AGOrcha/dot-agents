package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/internal/lifecycle"
	"github.com/NikashPrakash/dot-agents/internal/config"
)

// fakeInstallDeps is the interface-DI test double for lifecycle.InstallDeps
// (per docs/TEST_SEAMS.md). A nil func field delegates to the real
// implementation, so a test overrides only the operation it wants to
// fault-inject.
//
// Kept in package commands after t13b deleted the install shim because
// seams_test.go's install-pipeline seam tests stay in this package and call
// lifecycle.RunInstall / lifecycle.RunInstallGenerate / etc. directly,
// passing fakeInstallDeps. The fake implements every method on
// lifecycle.InstallDeps so it satisfies the interface via duck typing.
type fakeInstallDeps struct {
	getwd      func() (string, error)
	mkdirAll   func(string, os.FileMode) error
	symlink    func(string, string) error
	loadConfig func() (*config.Config, error)
}

func (f fakeInstallDeps) Getwd() (string, error) {
	if f.getwd != nil {
		return f.getwd()
	}
	return os.Getwd()
}

func (f fakeInstallDeps) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (f fakeInstallDeps) Symlink(oldname, newname string) error {
	if f.symlink != nil {
		return f.symlink(oldname, newname)
	}
	return os.Symlink(oldname, newname)
}

func (f fakeInstallDeps) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestFakeInstallDeps_NilDelegatesToReal pins the nil-delegates-to-real
// contract for every method of the fake. Without this, a future change to
// the fake's default branch could regress every happy-path-but-not-overridden
// test without any of them failing.
func TestFakeInstallDeps_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	f := fakeInstallDeps{}
	if _, err := f.Getwd(); err != nil {
		t.Fatalf("nil-getwd delegate: %v", err)
	}
	target := filepath.Join(tmp, "delegate", "nested")
	if err := f.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("nil-mkdirAll delegate: %v", err)
	}
	src := filepath.Join(tmp, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := f.Symlink(src, link); err != nil {
		t.Fatalf("nil-symlink delegate: %v", err)
	}
	if cfg, err := f.LoadConfig(); err != nil || cfg == nil {
		t.Fatalf("nil-loadConfig delegate: cfg=%v err=%v", cfg, err)
	}
}

// TestLifecycleInstallCmd_FlagsAndArgs exercises the production wiring
// path root.go uses: lifecycle.NewInstallCmd(buildLifecycleDeps()). This
// is a regression guard on the deps factory + constructor call rather
// than on the deep install behavior (which is covered in
// commands/lifecycle/install_test.go).
func TestLifecycleInstallCmd_FlagsAndArgs(t *testing.T) {
	cmd := lifecycle.NewInstallCmd(buildLifecycleDeps())
	if cmd.Flags().Lookup("generate") == nil {
		t.Error("missing --generate flag")
	}
	if cmd.Flags().Lookup("strict") == nil {
		t.Error("missing --strict flag")
	}
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Error("install should reject positional args")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("install should accept zero args, got: %v", err)
	}
}
