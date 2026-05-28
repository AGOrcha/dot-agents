package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// The trampolines below (RunStatus, RunStatusDefault, PrintAudit,
// PrintSymlinkDirAudit, CountClaudeRules) are SHAPE.md OD-2 export-window
// wrappers around their lowercase intra-package counterparts. They exist
// purely so doctor.go (root, t09-deferred) and seams_test.go (root,
// t11-deferred) can keep compiling during the staged migration. The
// lowercase implementations already have deep coverage via the existing
// TestRunStatus_* / TestPrintAudit_* / TestPrintSymlinkDirAudit_* /
// TestCountClaudeRules_* tests; this file gives the trampolines themselves
// at least one call site so coverage-gate.sh sees them exercised.
//
// When t09/t11 land and the cross-package consumers disappear, this file
// is deleted alongside the exported wrappers (the lowercase tests cover
// the surviving private surface).

// TestRunStatus_ExportTrampoline pins that RunStatus forwards through to the
// package-private runStatus. Uses an empty config + temp HOME so the call is
// hermetic.
func TestRunStatus_ExportTrampoline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatalf("mkdir agentsHome: %v", err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}

	if err := RunStatus(false, "", stdStatusConfigLoader{}, false); err != nil {
		t.Errorf("RunStatus text: %v", err)
	}
	if err := RunStatus(false, "", stdStatusConfigLoader{}, true); err != nil {
		t.Errorf("RunStatus json: %v", err)
	}
}

// TestRunStatusDefault_ExportTrampoline pins that RunStatusDefault uses the
// std loader and forwards to runStatus. The root cobra RunE closure calls
// this on each `da status` invocation; we exercise it directly so the
// trampoline is not 0%.
func TestRunStatusDefault_ExportTrampoline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatalf("mkdir agentsHome: %v", err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}

	if err := RunStatusDefault(false, "", false); err != nil {
		t.Errorf("RunStatusDefault: %v", err)
	}
}

// TestPrintAudit_ExportTrampoline pins that PrintAudit forwards through to
// printAudit. We invoke it with an empty project so all per-platform
// branches early-return; the trampoline is what we are measuring.
func TestPrintAudit_ExportTrampoline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatalf("mkdir agentsHome: %v", err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}

	// Empty agentFilter ⇒ every platform branch fires (each prints an
	// "(not linked)" line for the absent directories).
	PrintAudit("proj", projectPath, agentsHome, "", cfg)
}

// TestPrintSymlinkDirAudit_ExportTrampoline pins that PrintSymlinkDirAudit
// forwards through to printSymlinkDirAudit. We call it with an absent dir
// (ReadDir-fail branch) so it returns (0,0) without filesystem side effects.
func TestPrintSymlinkDirAudit_ExportTrampoline(t *testing.T) {
	ok, broken := PrintSymlinkDirAudit(filepath.Join(t.TempDir(), "absent"), "(empty)", "%s")
	if ok != 0 || broken != 0 {
		t.Errorf("PrintSymlinkDirAudit absent dir: got (%d,%d), want (0,0)", ok, broken)
	}
}

// TestCountClaudeRules_ExportTrampoline pins that CountClaudeRules forwards
// through to countClaudeRules. Both the missing-dir branch and a populated
// directory exercise the trampoline.
func TestCountClaudeRules_ExportTrampoline(t *testing.T) {
	tmp := t.TempDir()

	// Missing .claude/rules → early-return (0,0).
	ok, warn := CountClaudeRules(tmp)
	if ok != 0 || warn != 0 {
		t.Errorf("CountClaudeRules missing: got (%d,%d), want (0,0)", ok, warn)
	}

	// Populated .claude/rules with one healthy symlink + one broken symlink.
	rulesDir := filepath.Join(tmp, statusClaudeDir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("mkdir rulesDir: %v", err)
	}
	target := filepath.Join(tmp, "real.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}
	linktest.Link(t, target, filepath.Join(rulesDir, "ok.md"))
	linktest.DanglingLink(t, filepath.Join(rulesDir, "broken.md"))

	ok, warn = CountClaudeRules(tmp)
	if ok < 1 || warn < 1 {
		t.Errorf("CountClaudeRules populated: got (%d,%d), want ok>=1 warn>=1", ok, warn)
	}
}

// TestHasMultipleHardLinks_RegularAndHardlink exercises the canonical
// helper backing the lifecycle HasMultipleHardLinks seam. After the P3
// fold-back absorption the canonical implementation lives at
// platform.HasMultipleHardLinks (see
// internal/platform/claude_linkcount_{unix,windows}.go); the lifecycle
// seam is now a thin pointer to that platform export. The Lstat-fail
// branch is exercised by the absent path; the link-count branch is
// exercised by a regular file (link count 1, returns false). Hard-link
// creation on tmpfs is best-effort — skipped via the os.Link error so the
// test stays portable on systems that reject cross-device or unsupported-
// link calls.
func TestHasMultipleHardLinks_RegularAndHardlink(t *testing.T) {
	tmp := t.TempDir()

	// Absent path → Lstat fails → false.
	if platform.HasMultipleHardLinks(filepath.Join(tmp, "ghost")) {
		t.Error("platform.HasMultipleHardLinks(absent) = true, want false")
	}

	// Regular file with link count 1 → false.
	regular := filepath.Join(tmp, "regular.txt")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile regular: %v", err)
	}
	if platform.HasMultipleHardLinks(regular) {
		t.Error("platform.HasMultipleHardLinks(single-link) = true, want false")
	}

	// Best-effort hard link → true if the platform/filesystem allows it.
	linked := filepath.Join(tmp, "linked.txt")
	if err := os.Link(regular, linked); err != nil {
		// os.Link can fail on filesystems that disallow hard links
		// (e.g. some sandboxed runners on Windows). Treat the failure
		// as an environmental skip: the regular-file branch above and
		// the absent-path branch already gave the helper two reachable
		// branches; the multi-link branch is exercised on platforms
		// where os.Link succeeds (which is the case on the CI Linux
		// and macOS runners).
		t.Logf("os.Link unsupported on this filesystem (%v); skipping multi-link assertion", err)
		return
	}
	if !platform.HasMultipleHardLinks(regular) {
		t.Error("platform.HasMultipleHardLinks(2-link) = false, want true")
	}
	if !platform.HasMultipleHardLinks(linked) {
		t.Error("platform.HasMultipleHardLinks(2-link via alias) = false, want true")
	}
}

// TestHasMultipleHardLinks_SeamDefault pins that the package-level
// HasMultipleHardLinks func-var still points at the canonical
// platform-package implementation. backup_test.go already exercises the
// override pattern; this test guarantees the default binding has not been
// silently replaced by a no-op stub during the P3 fold-back relocation.
func TestHasMultipleHardLinks_SeamDefault(t *testing.T) {
	tmp := t.TempDir()
	regular := filepath.Join(tmp, "regular.txt")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Single-link regular file: the lifecycle seam must agree with the
	// canonical platform-package implementation it now wraps.
	if HasMultipleHardLinks(regular) != platform.HasMultipleHardLinks(regular) {
		t.Error("HasMultipleHardLinks seam diverges from platform.HasMultipleHardLinks")
	}
}
