package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
)

// captureDoctorOutput redirects stdout for the duration of fn and returns the
// captured bytes — used to assert doctor's user-visible output mentions
// broken links.
func captureDoctorOutput(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = oldStdout
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// seedManagedClaudeLink scaffolds a project + agentsHome where a Claude rule
// symlink points at an existing target — the "healthy" baseline doctor should
// report no broken links for.
func seedManagedClaudeLink(t *testing.T) (tmp, agentsHome, projectPath, linkPath, target string) {
	t.Helper()
	tmp = t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	projectPath = filepath.Join(tmp, "proj")

	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)

	target = filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	if err := os.MkdirAll(claudeRules, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath = filepath.Join(claudeRules, "proj--agents.md")
	linktest.Link(t, target, linkPath)

	// Register project in config.json so doctor includes it in its scan.
	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{},
		Agents:   map[string]config.Agent{},
	}
	cfg.AddProject("proj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return
}

// TestDoctorRepairE2E_ReportsAndRestoresBrokenLink walks the full add → break
// → doctor → refresh → doctor cycle without depending on any installed
// platform CLIs. It exercises doctor.go's link inspection + refresh.go's
// resource restoration on a synthetic project.
// assertDoctorStdoutContainsBroken runs doctor and asserts the captured
// output contains (or does not contain) the literal "broken" token.
func assertDoctorStdoutContainsBroken(t *testing.T, label string, wantBroken bool) {
	t.Helper()
	out := captureDoctorOutput(t, func() {
		if err := runDoctor(NewDoctorCmd(), nil); err != nil {
			t.Fatalf("%s runDoctor: %v", label, err)
		}
	})
	hasBroken := strings.Contains(out, "broken")
	if hasBroken != wantBroken {
		t.Fatalf("%s: wantBroken=%v gotBroken=%v output:\n%s", label, wantBroken, hasBroken, out)
	}
}

// breakAndConfirmBrokenLink removes target and asserts collectBrokenLinks
// reports exactly one claude-owned breakage.
func breakAndConfirmBrokenLink(t *testing.T, agentsHome, projectPath, target string) {
	t.Helper()
	if err := os.Remove(target); err != nil {
		t.Fatalf("break target: %v", err)
	}
	broken := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(broken) != 1 || broken[0].platformID != "claude" {
		t.Fatalf("expected 1 claude broken link, got %+v", broken)
	}
}

// runDoctorDryRunReportsBroken flips Flags into dry-run, runs doctor,
// restores Flags, and asserts the doctor output reports the breakage.
func runDoctorDryRunReportsBroken(t *testing.T) {
	t.Helper()
	Flags.DryRun = true
	defer func() { Flags.DryRun = false }()
	assertDoctorStdoutContainsBroken(t, "dry-run-broken", true)
}

// seedResourcesAndRestore seeds agentsHome/resources/proj/AGENTS.md and runs
// restoreFromResources, asserting the broken target + link are recovered.
func seedResourcesAndRestore(t *testing.T, agentsHome, projectPath, linkPath, target string) {
	t.Helper()
	resources := filepath.Join(agentsHome, "resources", "proj")
	if err := os.MkdirAll(resources, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "AGENTS.md"), []byte("# rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	restoreFromResources("proj", projectPath)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected restoreFromResources to recreate %s: %v", target, err)
	}
	if _, err := os.Stat(linkPath); err != nil {
		t.Fatalf("expected link to resolve after restore: %v", err)
	}
}

func TestDoctorRepairE2E_ReportsAndRestoresBrokenLink(t *testing.T) {
	_, agentsHome, projectPath, linkPath, target := seedManagedClaudeLink(t)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	// Phase 1: baseline doctor → no broken links.
	assertDoctorStdoutContainsBroken(t, "baseline", false)

	// Phase 2: break the link by deleting its target.
	breakAndConfirmBrokenLink(t, agentsHome, projectPath, target)

	// Phase 3: doctor in dry-run should report the breakage without erroring.
	runDoctorDryRunReportsBroken(t)

	// Phase 4: seed resources/ so refresh's restoreFromResources can recreate
	// the deleted target, then re-run.
	seedResourcesAndRestore(t, agentsHome, projectPath, linkPath, target)

	// Phase 5: doctor reports clean again.
	assertDoctorStdoutContainsBroken(t, "post-repair", false)
}

// TestDoctorRepairE2E_DryRunDoesNotMutateRepo verifies doctor --dry-run never
// re-runs CreateLinks against the project repo when broken links are present.
// Snapshotting only the project repo (not ~/.agents/, which doctor may rewrite
// for unrelated bookkeeping such as windows-mirror flags) keeps the assertion
// focused on the dry-run repair contract.
func TestDoctorRepairE2E_DryRunDoesNotMutateRepo(t *testing.T) {
	_, _, projectPath, _, _ := seedManagedClaudeLink(t)

	// Introduce a dangling AGENTS.md symlink — only the doctor repair path
	// would touch this file.
	agentsMD := filepath.Join(projectPath, "AGENTS.md")
	linktest.DanglingLink(t, agentsMD)

	before := snapshotTree(t, projectPath)

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	_ = captureDoctorOutput(t, func() {
		if err := runDoctor(NewDoctorCmd(), nil); err != nil {
			t.Fatalf("dry-run doctor: %v", err)
		}
	})

	after := snapshotTree(t, projectPath)
	if msg, ok := snapshotsEqual(before, after); !ok {
		t.Fatalf("dry-run doctor mutated the project repo: %s", msg)
	}
}
