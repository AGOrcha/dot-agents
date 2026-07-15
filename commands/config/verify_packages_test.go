package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/commands/internal/lifecycle"
	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// seedPackagesArtifactProject sets up an isolated ~/.agents + project pair
// with one materialized, locked `packages[]` artifact — driven through the
// SAME HydratePackagesUnits pass-2 driver `da install`/`da refresh` use — and
// returns the project path plus the resolved digest for the caller to
// tamper with.
func seedPackagesArtifactProject(t *testing.T) (project, digest string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("AGENTS_HOME", filepath.Join(home, ".agents"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	srcRoot := filepath.Join(root, "src")
	skillDir := filepath.Join(srcRoot, "skill", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	project = filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	sources := []cfg.Source{{Type: "local", ID: "da-agc", Path: srcRoot}}
	rc := &cfg.AgentsRC{Version: 1, Project: "project", Sources: sources, Packages: []cfg.PackageRef{{Ref: "da-agc:skill/demo@1"}}}
	if err := rc.Save(project); err != nil {
		t.Fatal(err)
	}

	res := &cfg.EnsureResult{
		Snapshot:   &cfg.Snapshot{Effective: cfg.AgentsRC{Sources: sources, Packages: rc.Packages}},
		ReResolved: true,
	}
	units, err := lifecycle.HydratePackagesUnits(project, "project", res)
	if err != nil {
		t.Fatalf("seed HydratePackagesUnits: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 seeded unit, got %d", len(units))
	}
	digest = units[0].Digest

	// Re-key the lock entry under the PINNED form the H7 resolver can verify
	// offline (an "@1" tag-shaped ref cannot be re-checked without network).
	existing, err := cfg.ReadUnits(project)
	if err != nil {
		t.Fatal(err)
	}
	pinnedRef := "da-agc:skill/demo@pinned:" + digest
	existing.Units[pinnedRef] = existing.Units["da-agc:skill/demo@1"]
	delete(existing.Units, "da-agc:skill/demo@1")
	rc.Packages = []cfg.PackageRef{{Ref: pinnedRef}}
	if err := rc.Save(project); err != nil {
		t.Fatal(err)
	}
	// inputs_digest is a pass-1 (config resolution) concern this fixture
	// bypasses by seeding pass-2 directly; compute it against the manifest's
	// final (pinned-ref) state so verifyStaleness sees a lock that matches
	// local scope — isolating the FAIL this test proves to unit-digest-
	// mismatch alone, not a coincidental local-scope drift.
	inputsDigest, err := cfg.ComputeInputsDigest(project, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteUnitsLock(project, cfg.UnitsLock{Units: existing.Units, InputsDigest: inputsDigest}); err != nil {
		t.Fatal(err)
	}
	return project, digest
}

// TestVerifyStaleness_DetectsPostInstallStoreTamper is H7's `config verify`
// acceptance test: after a packages artifact is installed and its CAS content
// is tampered with in place, `da config verify` must FAIL — not silently
// report OK — because verifyStaleness now threads a real artifact-store
// integrity resolver into cfg.Staleness instead of nil.
func TestVerifyStaleness_DetectsPostInstallStoreTamper(t *testing.T) {
	project, digest := seedPackagesArtifactProject(t)

	clean := verifyStaleness(project)
	if len(clean) != 1 || clean[0].Status != verifyPass {
		t.Fatalf("expected a clean pass before tampering, got %+v", clean)
	}

	casPath := cfg.ArtifactStorePath(cfg.AgentsHome(), "skills", digest)
	if err := os.WriteFile(filepath.Join(casPath, "SKILL.md"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("tamper CAS content: %v", err)
	}

	tampered := verifyStaleness(project)
	if len(tampered) != 1 || tampered[0].Status != verifyFail {
		t.Fatalf("expected a FAIL check after a post-install store tamper, got %+v", tampered)
	}
}

// TestBuildVerifyReport_PostInstallStoreTamperFlipsOK proves the tamper
// actually flips the top-level report OK — the observable "config verify
// blocks" signal a CI gate or operator would see.
func TestBuildVerifyReport_PostInstallStoreTamperFlipsOK(t *testing.T) {
	project, digest := seedPackagesArtifactProject(t)

	casPath := cfg.ArtifactStorePath(cfg.AgentsHome(), "skills", digest)
	if err := os.WriteFile(filepath.Join(casPath, "SKILL.md"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("tamper CAS content: %v", err)
	}

	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if report.OK {
		t.Fatalf("expected a post-install store tamper to flip report.OK to false, got %+v", report)
	}
}
