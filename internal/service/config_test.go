package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/tasks"
)

// TestDefaults proves the zero Config resolves every field to its documented
// per-repo default: cwd repo, .agents/active locations, the OQ3 loopback
// HTTP bind, and both builtin tasks enabled.
func TestDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	got, err := Config{}.withDefaults()
	if err != nil {
		t.Fatalf("withDefaults: %v", err)
	}
	// t.TempDir may sit behind a symlink (macOS /var -> /private/var); cwd
	// resolution goes through os.Getwd, so compare resolved paths.
	wantRepo, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotRepo, err := filepath.EvalSymlinks(got.RepoDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(RepoDir): %v", err)
	}
	if gotRepo != wantRepo {
		t.Errorf("RepoDir = %q, want %q", gotRepo, wantRepo)
	}
	active := filepath.Join(got.RepoDir, ".agents", "active")
	if want := filepath.Join(active, "iteration-log"); got.IterLogDir != want {
		t.Errorf("IterLogDir = %q, want %q", got.IterLogDir, want)
	}
	if want := filepath.Join(active, ControlSocketName); got.ControlSocket != want {
		t.Errorf("ControlSocket = %q, want %q", got.ControlSocket, want)
	}
	if got.HTTPAddr != DefaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", got.HTTPAddr, DefaultHTTPAddr)
	}
	wantTasks := []string{tasks.IterLogIngesterName, tasks.RescoreName}
	if !reflect.DeepEqual(got.EnabledTasks, wantTasks) {
		t.Errorf("EnabledTasks = %v, want %v", got.EnabledTasks, wantTasks)
	}
}

// TestExplicitFieldsPreserved proves withDefaults never overwrites a field
// the caller set explicitly.
func TestExplicitFieldsPreserved(t *testing.T) {
	bus := events.NewInProcBus()
	defer func() { _ = bus.Close() }()
	in := Config{
		ControlSocket:   "/tmp/x.sock",
		HTTPAddr:        "127.0.0.1:0",
		IterLogDir:      "/tmp/iterlog",
		RepoDir:         "/tmp/repo",
		RescoreInterval: time.Hour,
		EnabledTasks:    []string{tasks.RescoreName},
		Bus:             bus,
	}
	got, err := in.withDefaults()
	if err != nil {
		t.Fatalf("withDefaults: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("withDefaults mutated explicit config:\n got %+v\nwant %+v", got, in)
	}
}

// TestRepoDirDefaultOnly proves a Config with only RepoDir set derives the
// per-repo paths from it rather than from the working directory.
func TestRepoDirDefaultOnly(t *testing.T) {
	repo := t.TempDir()
	got, err := Config{RepoDir: repo}.withDefaults()
	if err != nil {
		t.Fatalf("withDefaults: %v", err)
	}
	if want := filepath.Join(repo, ".agents", "active", "iteration-log"); got.IterLogDir != want {
		t.Errorf("IterLogDir = %q, want %q", got.IterLogDir, want)
	}
	if want := filepath.Join(repo, ".agents", "active", ControlSocketName); got.ControlSocket != want {
		t.Errorf("ControlSocket = %q, want %q", got.ControlSocket, want)
	}
}

// TestGetwdFailure proves the one error branch of withDefaults: RepoDir must
// be defaulted but the working directory is gone.
func TestGetwdFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows locks the working directory; it cannot be removed while in use")
	}
	dir := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Skipf("cannot remove working directory on this platform: %v", err)
	}
	if _, err := (Config{}).withDefaults(); err == nil {
		// macOS getcwd can still resolve a deleted working directory from
		// the vnode cache; the error branch is exercised on Linux (where
		// getcwd(2) reports ENOENT) and credited by the merged multi-OS
		// coverage profile.
		t.Skip("platform resolves a deleted working directory; cannot force Getwd failure")
	}
	// Run must surface the same failure through its defaults resolution.
	if err := Run(context.Background(), Config{}); err == nil {
		t.Fatal("Run succeeded with an unresolvable working directory, want error")
	}
}
