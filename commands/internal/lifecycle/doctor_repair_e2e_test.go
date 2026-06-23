package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/linktest"
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

// assertDoctorStdoutContainsBroken runs doctor and asserts the captured
// output contains (or does not contain) the literal "broken" token.
func assertDoctorStdoutContainsBroken(t *testing.T, label string, wantBroken bool) {
	t.Helper()
	out := captureDoctorOutput(t, func() {
		if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
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

// runDoctorReportsBrokenLeavesTreeUnchanged runs doctor against a project that
// has a broken link and asserts (a) doctor reports the breakage and (b) doctor
// did not mutate the project tree. This is the positive coverage for §7A.6's
// read-only contract: doctor detects, it never repairs.
func runDoctorReportsBrokenLeavesTreeUnchanged(t *testing.T, projectPath string) {
	t.Helper()
	before := doctorE2ESnapshotTree(t, projectPath)
	assertDoctorStdoutContainsBroken(t, "broken-detected", true)
	after := doctorE2ESnapshotTree(t, projectPath)
	if msg, ok := doctorE2ESnapshotsEqual(before, after); !ok {
		t.Fatalf("doctor must be read-only but mutated the project repo: %s", msg)
	}
}

// seedResourcesAndRestore seeds agentsHome/resources/proj/AGENTS.md and runs
// RestoreFromResourcesCountedWithDeps — the explicit remediation step a user
// performs AFTER doctor reports the breakage (doctor itself never repairs per
// §7A.6). It asserts the broken target + link are recovered.
func seedResourcesAndRestore(t *testing.T, agentsHome, projectPath, linkPath, target string) {
	t.Helper()
	resources := filepath.Join(agentsHome, "resources", "proj")
	if err := os.MkdirAll(resources, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "AGENTS.md"), []byte("# rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreFromResourcesCountedWithDeps("proj", projectPath, StdAddDeps{}); err != nil {
		t.Fatalf("RestoreFromResourcesCountedWithDeps: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected restore to recreate %s: %v", target, err)
	}
	if _, err := os.Stat(linkPath); err != nil {
		t.Fatalf("expected link to resolve after restore: %v", err)
	}
}

// doctorE2ESnapshotEntry captures one path under a root with a hash of its
// contents (or empty hash for directories / symlinks). Inlined from
// commands/refresh_idempotency_test.go's snapshotEntry — that helper lives
// in package commands and is not visible from package lifecycle. After t11
// splits seams_test into per-cluster files this is the place to land a
// shared lifecycle testutil; until then a small local copy is the simplest
// path.
type doctorE2ESnapshotEntry struct {
	rel  string
	kind string // "dir", "file", "symlink"
	hash string
}

// doctorE2ESnapshotTree walks root and records every entry with a
// deterministic signature.
func doctorE2ESnapshotTree(t *testing.T, root string) []doctorE2ESnapshotEntry {
	t.Helper()
	var out []doctorE2ESnapshotEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		entry := doctorE2ESnapshotEntry{rel: rel}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.kind = "symlink"
			if dest, err := os.Readlink(path); err == nil {
				h := sha256.Sum256([]byte(dest))
				entry.hash = hex.EncodeToString(h[:])
			}
		case info.IsDir():
			entry.kind = "dir"
		default:
			entry.kind = "file"
			data, err := os.ReadFile(path)
			if err == nil {
				h := sha256.Sum256(data)
				entry.hash = hex.EncodeToString(h[:])
			}
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

func doctorE2ESnapshotsEqual(a, b []doctorE2ESnapshotEntry) (string, bool) {
	if len(a) != len(b) {
		return "snapshot length differs", false
	}
	for i := range a {
		if a[i] != b[i] {
			return "entry differs: " + a[i].rel, false
		}
	}
	return "", true
}

// TestDoctorE2E_ReportsBrokenLinkAndIsReadOnly walks the full
// add → break → doctor (read-only detect) → user-runs-restore → doctor cycle
// without depending on any installed platform CLIs. Per §7A.6 doctor only
// detects: the recovery in phase 4 is an explicit RestoreFromResources call
// (the `da refresh`-equivalent) the USER runs after doctor surfaces the
// breakage — doctor itself never mutates the tree (asserted in phase 3).
func TestDoctorE2E_ReportsBrokenLinkAndIsReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: this E2E breaks a managed link by deleting its target and asserts it dangles. A Windows managed *file* link is a hard link with no reparse point — removing the canonical source only decrements nlink, the content persists, so it cannot dangle and managedLinkBroken correctly reports it non-broken by design (see doctor.go managedLinkBroken doc). Windows healthy hard-link counting is covered by TestCountProjectLinks_AllHealthyVariants and internal/linktest/linktest_test.go.")
	}
	_, agentsHome, projectPath, linkPath, target := seedManagedClaudeLink(t)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	// Phase 1: baseline doctor → no broken links.
	assertDoctorStdoutContainsBroken(t, "baseline", false)

	// Phase 2: break the link by deleting its target.
	breakAndConfirmBrokenLink(t, agentsHome, projectPath, target)

	// Phase 3: doctor reports the breakage and leaves the tree untouched
	// (read-only — it does NOT repair).
	runDoctorReportsBrokenLeavesTreeUnchanged(t, projectPath)

	// Phase 4: the user runs the remediation command (restore from resources)
	// to recreate the deleted target — this is NOT doctor's job.
	seedResourcesAndRestore(t, agentsHome, projectPath, linkPath, target)

	// Phase 5: doctor reports clean again.
	assertDoctorStdoutContainsBroken(t, "post-restore", false)
}

// TestDoctorE2E_DoesNotMutateRepo verifies doctor never touches the project
// repo when broken links are present — the core §7A.6 read-only guarantee.
// Before the reshape doctor would re-run CreateLinks / shared-target projection
// here; now it must leave the tree byte-identical.
func TestDoctorE2E_DoesNotMutateRepo(t *testing.T) {
	_, _, projectPath, _, _ := seedManagedClaudeLink(t)

	// Introduce a dangling AGENTS.md symlink — the only path the old doctor
	// repair would have touched.
	agentsMD := filepath.Join(projectPath, "AGENTS.md")
	linktest.DanglingLink(t, agentsMD)

	before := doctorE2ESnapshotTree(t, projectPath)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	_ = captureDoctorOutput(t, func() {
		if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
			t.Fatalf("doctor: %v", err)
		}
	})

	after := doctorE2ESnapshotTree(t, projectPath)
	if msg, ok := doctorE2ESnapshotsEqual(before, after); !ok {
		t.Fatalf("doctor mutated the project repo (must be read-only): %s", msg)
	}
}
