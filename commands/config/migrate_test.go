package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

const migrateV1Manifest = `{
  "version": 1,
  "project": "demo",
  "sources": [{"type": "local"}],
  "verifier_profiles": {"unit": {"label": "Unit"}}
}`

// migrateOptions builds a runMigrateOptions wired to in-memory streams pointed at
// project, with the json/dry-run flags set.
func migrateOptions(project string, jsonOut, dryRun bool) (*runMigrateOptions, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return &runMigrateOptions{
		runContext: runContext{
			jsonOut: jsonOut,
			dryRun:  dryRun,
			stdout:  out,
			stderr:  &bytes.Buffer{},
			cwd:     project,
		},
	}, out
}

// writeProjectManifest writes body as the project's .agentsrc.json in a fresh
// temp dir and returns the project root.
func writeProjectManifest(t *testing.T, body string) string {
	t.Helper()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, cfg.AgentsRCFile), []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return project
}

func TestRunMigrate_HumanRewrite(t *testing.T) {
	project := writeProjectManifest(t, migrateV1Manifest)
	opts, out := migrateOptions(project, false, false)

	if err := runMigrate(opts, testDeps()); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
	got := out.String()
	for _, want := range []string{"rewrote", "version 1 -> 2", "verifier_profiles", "backed up to"} {
		if !strings.Contains(got, want) {
			t.Errorf("human output missing %q:\n%s", want, got)
		}
	}
	// The file was rewritten to v2 and the backup created.
	if _, err := os.Stat(filepath.Join(project, cfg.AgentsRCFile+cfg.V1BackupSuffix)); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestRunMigrate_JSON(t *testing.T) {
	project := writeProjectManifest(t, migrateV1Manifest)
	opts, out := migrateOptions(project, true, false)

	if err := runMigrate(opts, testDeps()); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
	var rep MigrateReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("decode json report: %v\n%s", err, out.String())
	}
	if !rep.OK || rep.AlreadyV2 || !rep.WroteFile || !rep.WroteBackup {
		t.Errorf("unexpected report: %+v", rep)
	}
	if rep.FromVersion != 1 || rep.ToVersion != 2 {
		t.Errorf("version: got %d->%d, want 1->2", rep.FromVersion, rep.ToVersion)
	}
}

func TestRunMigrate_DryRunWritesNothing(t *testing.T) {
	project := writeProjectManifest(t, migrateV1Manifest)
	opts, out := migrateOptions(project, false, true)

	if err := runMigrate(opts, testDeps()); err != nil {
		t.Fatalf("runMigrate dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("dry-run output missing 'dry run':\n%s", out.String())
	}
	// Manifest untouched, no backup.
	onDisk, _ := os.ReadFile(filepath.Join(project, cfg.AgentsRCFile))
	if string(onDisk) != migrateV1Manifest {
		t.Errorf("dry-run mutated manifest:\n%s", onDisk)
	}
	if _, err := os.Stat(filepath.Join(project, cfg.AgentsRCFile+cfg.V1BackupSuffix)); !os.IsNotExist(err) {
		t.Errorf("dry-run created backup (err=%v)", err)
	}
}

func TestRunMigrate_AlreadyV2NoOp(t *testing.T) {
	project := writeProjectManifest(t, `{
  "version": 2,
  "project": "demo",
  "repo_id": "github.com/acme/demo",
  "sources": [{"type": "local", "id": "self"}]
}`)
	opts, out := migrateOptions(project, false, false)

	if err := runMigrate(opts, testDeps()); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
	if !strings.Contains(out.String(), "already v2") {
		t.Errorf("expected 'already v2' message, got:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(project, cfg.AgentsRCFile+cfg.V1BackupSuffix)); !os.IsNotExist(err) {
		t.Errorf("no-op created a backup (err=%v)", err)
	}
}

func TestRunMigrate_MissingManifestErrors(t *testing.T) {
	opts, _ := migrateOptions(t.TempDir(), false, false)
	if err := runMigrate(opts, testDeps()); err == nil {
		t.Error("expected error when no .agentsrc.json is present")
	}
}
