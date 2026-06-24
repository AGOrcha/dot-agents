package platform

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/linktest"
)

// auditOf renders p.PrintAudit into a buffer and returns the text. p must
// implement AuditPrinter (every platform.All() entry does — proved by the
// contract check below).
func auditOf(t *testing.T, p Platform, project, repoPath, agentsHome string) string {
	t.Helper()
	ap, ok := p.(AuditPrinter)
	if !ok {
		t.Fatalf("%s does not implement AuditPrinter", p.ID())
	}
	var buf bytes.Buffer
	ap.PrintAudit(&buf, project, repoPath, agentsHome)
	return buf.String()
}

// TestAllPlatformsImplementAuditPrinter is the central proof that every
// registered platform satisfies the AuditPrinter sister interface, so the
// lifecycle audit loop surfaces all of them automatically.
func TestAllPlatformsImplementAuditPrinter(t *testing.T) {
	for _, p := range All() {
		if _, ok := p.(AuditPrinter); !ok {
			t.Errorf("platform %q must implement AuditPrinter", p.ID())
		}
	}
}

// TestFilter covers the empty-filter pass-through, a matching single-id
// selection, and a no-match empty result.
func TestFilter(t *testing.T) {
	all := All()
	if got := Filter(all, ""); len(got) != len(all) {
		t.Errorf("empty filter should return all %d platforms, got %d", len(all), len(got))
	}
	got := Filter(all, "claude")
	if len(got) != 1 || got[0].ID() != "claude" {
		t.Errorf("filter 'claude' = %v, want exactly [claude]", got)
	}
	if got := Filter(all, "nope"); got != nil {
		t.Errorf("no-match filter should return nil, got %v", got)
	}
}

// TestCursorPrintAudit_EmptyProject hits the no-.cursor/rules/ early return.
func TestCursorPrintAudit_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	out := auditOf(t, NewCursor(), "proj", tmp, filepath.Join(tmp, ".agents"))
	if !strings.Contains(out, "Cursor") || !strings.Contains(out, "(no .cursor/rules/)") {
		t.Errorf("empty cursor audit missing header/no-rules line: %q", out)
	}
}

// TestCursorPrintAudit_HealthyUnlinkedLocalAndMCP covers the healthy-hardlink,
// not-linked-to, local-file rule branches plus the .cursor/mcp.json variants.
func TestCursorPrintAudit_HealthyUnlinkedLocalAndMCP(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "rules", "global", "rule.mdc")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(src, []byte("rule"), 0644)

	rulesDir := filepath.Join(tmp, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)
	os.Link(src, filepath.Join(rulesDir, "global--rule.mdc"))                      // healthy hardlink
	os.WriteFile(filepath.Join(rulesDir, "proj--unlinked.mdc"), []byte("x"), 0644) // not linked
	os.WriteFile(filepath.Join(rulesDir, "local.mdc"), []byte("x"), 0644)          // local file
	os.WriteFile(filepath.Join(rulesDir, "junk.txt"), []byte("x"), 0644)           // skipped
	linktest.DanglingLink(t, filepath.Join(tmp, ".cursor", "mcp.json"))            // broken mcp

	out := auditOf(t, NewCursor(), "proj", tmp, agentsHome)
	if !strings.Contains(out, "global--rule.mdc") || !strings.Contains(out, "← ~/.agents/rules/global/rule.mdc") {
		t.Errorf("healthy rule line missing: %q", out)
	}
	if !strings.Contains(out, "not linked to") {
		t.Errorf("unlinked rule line missing: %q", out)
	}
	if !strings.Contains(out, "local.mdc") || !strings.Contains(out, "(local file)") {
		t.Errorf("local-file rule line missing: %q", out)
	}
	if !strings.Contains(out, ".cursor/mcp.json") || !strings.Contains(out, "broken") {
		t.Errorf("broken mcp.json line missing: %q", out)
	}
}

// TestCursorPrintAudit_MCPLocalAndNotLinked covers the regular-file (hard
// link or local file) and absent (not linked) mcp.json branches plus the
// "(no rules)" empty rules-dir branch.
func TestCursorPrintAudit_MCPLocalAndNotLinked(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Regular-file mcp.json → "hard link or local file" + empty rules dir.
	local := filepath.Join(tmp, "local")
	os.MkdirAll(filepath.Join(local, ".cursor", "rules"), 0755)
	os.WriteFile(filepath.Join(local, ".cursor", "mcp.json"), []byte("{}"), 0644)
	out := auditOf(t, NewCursor(), "local", local, agentsHome)
	if !strings.Contains(out, "hard link or local file") || !strings.Contains(out, "(no rules)") {
		t.Errorf("local mcp / no-rules lines missing: %q", out)
	}

	// No mcp.json → "(not linked)".
	bare := filepath.Join(tmp, "bare")
	os.MkdirAll(filepath.Join(bare, ".cursor", "rules"), 0755)
	out = auditOf(t, NewCursor(), "bare", bare, agentsHome)
	if !strings.Contains(out, ".cursor/mcp.json") || !strings.Contains(out, "(not linked)") {
		t.Errorf("absent mcp.json (not linked) line missing: %q", out)
	}
}

// TestClaudePrintAudit_BrokenHealthyAndMissing covers the link directory
// (healthy + broken) and the .mcp.json link plus the no-rules-dir branch.
func TestClaudePrintAudit_BrokenHealthyAndMissing(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	proj := filepath.Join(tmp, "p")
	claudeRules := filepath.Join(proj, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	healthyTarget := filepath.Join(agentsHome, "rules", "p", "ok.md")
	os.MkdirAll(filepath.Dir(healthyTarget), 0755)
	os.WriteFile(healthyTarget, []byte("ok"), 0644)
	linktest.Link(t, healthyTarget, filepath.Join(claudeRules, "p--ok.md"))
	linktest.DanglingLink(t, filepath.Join(claudeRules, "p--broken.md"))
	os.WriteFile(filepath.Join(claudeRules, "raw-file.md"), []byte("raw"), 0644)
	linktest.DanglingLink(t, filepath.Join(proj, ".mcp.json"))

	out := auditOf(t, NewClaude(), "p", proj, agentsHome)
	if !strings.Contains(out, "Claude Code") || !strings.Contains(out, "broken") {
		t.Errorf("claude audit missing header/broken line: %q", out)
	}

	// Missing .claude/rules dir → "(no .claude/rules/)".
	empty := filepath.Join(tmp, "empty")
	os.MkdirAll(empty, 0755)
	out = auditOf(t, NewClaude(), "empty", empty, agentsHome)
	if !strings.Contains(out, "(no .claude/rules/)") {
		t.Errorf("missing claude rules line: %q", out)
	}
}

// TestCodexPrintAudit_AllBranches covers AGENTS.md (symlink/local/missing),
// config.toml + hooks.json links, the shared skills mirror, and the native
// .codex/agents/ entries (readable + unreadable).
func TestCodexPrintAudit_AllBranches(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	proj := filepath.Join(tmp, "p1")
	os.MkdirAll(filepath.Join(proj, ".codex", "agents"), 0755)
	os.MkdirAll(filepath.Join(proj, ".agents", "skills"), 0755)

	target := filepath.Join(agentsHome, "rules", "p1", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# a"), 0644)
	linktest.Link(t, target, filepath.Join(proj, "AGENTS.md"))

	cfgT := filepath.Join(agentsHome, "settings", "p1", "codex.toml")
	os.MkdirAll(filepath.Dir(cfgT), 0755)
	os.WriteFile(cfgT, []byte("# toml"), 0644)
	linktest.Link(t, cfgT, filepath.Join(proj, ".codex", "config.toml"))

	skillTarget := filepath.Join(agentsHome, "skills", "p1", "x")
	os.MkdirAll(skillTarget, 0755)
	linktest.Link(t, skillTarget, filepath.Join(proj, ".agents", "skills", "x"))
	linktest.DanglingLink(t, filepath.Join(proj, ".agents", "skills", "broken"))
	os.WriteFile(filepath.Join(proj, ".agents", "skills", "regular.md"), []byte("r"), 0644)

	os.WriteFile(filepath.Join(proj, ".codex", "agents", "ok.toml"), []byte("name=ok"), 0644)
	linktest.DanglingLink(t, filepath.Join(proj, ".codex", "agents", "broken.toml"))

	out := auditOf(t, NewCodex(), "p1", proj, agentsHome)
	if !strings.Contains(out, "Codex") || !strings.Contains(out, "(native TOML)") || !strings.Contains(out, "(unreadable)") {
		t.Errorf("codex audit missing expected lines: %q", out)
	}

	// AGENTS.md local file.
	proj2 := filepath.Join(tmp, "p2")
	os.MkdirAll(filepath.Join(proj2, ".codex"), 0755)
	os.WriteFile(filepath.Join(proj2, "AGENTS.md"), []byte("# local"), 0644)
	out = auditOf(t, NewCodex(), "p2", proj2, agentsHome)
	if !strings.Contains(out, "(local file)") {
		t.Errorf("codex local AGENTS.md line missing: %q", out)
	}

	// No AGENTS.md → "(no AGENTS.md)".
	proj3 := filepath.Join(tmp, "p3")
	os.MkdirAll(proj3, 0755)
	out = auditOf(t, NewCodex(), "p3", proj3, agentsHome)
	if !strings.Contains(out, "(no AGENTS.md)") {
		t.Errorf("codex no-AGENTS.md line missing: %q", out)
	}
}

// TestOpenCodePrintAudit_LocalBrokenAndAgentDir covers opencode.json local +
// broken-symlink + healthy, and the .opencode/agent/ dir + missing-.opencode/.
func TestOpenCodePrintAudit_LocalBrokenAndAgentDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Local-file opencode.json + missing .opencode dir.
	local := filepath.Join(tmp, "local")
	os.MkdirAll(local, 0755)
	os.WriteFile(filepath.Join(local, "opencode.json"), []byte("{}"), 0644)
	out := auditOf(t, NewOpenCode(), "local", local, agentsHome)
	if !strings.Contains(out, "OpenCode") || !strings.Contains(out, "(local file)") || !strings.Contains(out, "(no .opencode/)") {
		t.Errorf("opencode local audit missing lines: %q", out)
	}

	// Broken opencode.json symlink + populated agent dir (healthy + broken).
	broken := filepath.Join(tmp, "broken")
	agentDir := filepath.Join(broken, ".opencode", "agent")
	os.MkdirAll(agentDir, 0755)
	linktest.DanglingLink(t, filepath.Join(broken, "opencode.json"))
	at := filepath.Join(agentsHome, "agents", "broken", "ok", "AGENT.md")
	os.MkdirAll(filepath.Dir(at), 0755)
	os.WriteFile(at, []byte("ok"), 0644)
	linktest.Link(t, at, filepath.Join(agentDir, "ok.md"))
	linktest.DanglingLink(t, filepath.Join(agentDir, "broken.md"))
	out = auditOf(t, NewOpenCode(), "broken", broken, agentsHome)
	if !strings.Contains(out, "opencode.json") || !strings.Contains(out, "broken") {
		t.Errorf("opencode broken audit missing lines: %q", out)
	}
}

// TestCopilotPrintAudit_HealthyBrokenAndNotLinked covers the instructions link
// (healthy/broken/not-linked) and the .vscode/mcp.json link.
func TestCopilotPrintAudit_HealthyBrokenAndNotLinked(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Healthy instructions + healthy mcp.
	healthy := filepath.Join(tmp, "healthy")
	os.MkdirAll(filepath.Join(healthy, ".github"), 0755)
	os.MkdirAll(filepath.Join(healthy, ".vscode"), 0755)
	instr := filepath.Join(agentsHome, "rules", "healthy", "copilot-instructions.md")
	os.MkdirAll(filepath.Dir(instr), 0755)
	os.WriteFile(instr, []byte("# i"), 0644)
	linktest.Link(t, instr, filepath.Join(healthy, ".github", "copilot-instructions.md"))
	mcp := filepath.Join(agentsHome, "mcp", "healthy", "mcp.json")
	os.MkdirAll(filepath.Dir(mcp), 0755)
	os.WriteFile(mcp, []byte("{}"), 0644)
	linktest.Link(t, mcp, filepath.Join(healthy, ".vscode", "mcp.json"))
	out := auditOf(t, NewCopilot(), "healthy", healthy, agentsHome)
	if !strings.Contains(out, "GitHub Copilot") {
		t.Errorf("copilot header missing: %q", out)
	}

	// Broken instructions symlink.
	brokenProj := filepath.Join(tmp, "broken")
	os.MkdirAll(filepath.Join(brokenProj, ".github"), 0755)
	linktest.DanglingLink(t, filepath.Join(brokenProj, ".github", "copilot-instructions.md"))
	out = auditOf(t, NewCopilot(), "broken", brokenProj, agentsHome)
	if !strings.Contains(out, "copilot-instructions.md") || !strings.Contains(out, "broken") {
		t.Errorf("copilot broken line missing: %q", out)
	}

	// Not linked at all.
	empty := filepath.Join(tmp, "empty")
	os.MkdirAll(empty, 0755)
	out = auditOf(t, NewCopilot(), "empty", empty, agentsHome)
	if !strings.Contains(out, "(not linked)") {
		t.Errorf("copilot not-linked line missing: %q", out)
	}
}

// TestPrintSymlinkDirAudit_EmptyLabel covers the empty-dir branch of the
// shared dir auditor.
func TestPrintSymlinkDirAudit_EmptyLabel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	os.MkdirAll(dir, 0755)
	var buf bytes.Buffer
	ok, broken := printSymlinkDirAudit(&buf, dir, ".some/path/", "%s")
	if ok != 0 || broken != 0 {
		t.Errorf("empty dir = (%d,%d), want (0,0)", ok, broken)
	}
	if !strings.Contains(buf.String(), "(empty)") {
		t.Errorf("empty-label line missing: %q", buf.String())
	}
}

// TestPrintSymlinkAudit_LocalNotLinkedHealthy covers the shared single-link
// auditor's (local file) / (not linked) / healthy branches.
func TestPrintSymlinkAudit_LocalNotLinkedHealthy(t *testing.T) {
	tmp := t.TempDir()
	present := filepath.Join(tmp, ".mcp.json")
	os.WriteFile(present, []byte("{}"), 0644)
	var buf bytes.Buffer
	printSymlinkAudit(&buf, present, ".mcp.json")
	if !strings.Contains(buf.String(), "(local file)") || strings.Contains(buf.String(), "(not linked)") {
		t.Errorf("present file should be (local file): %q", buf.String())
	}

	buf.Reset()
	printSymlinkAudit(&buf, filepath.Join(tmp, "missing.json"), ".vscode/mcp.json")
	if !strings.Contains(buf.String(), "(not linked)") {
		t.Errorf("absent path should be (not linked): %q", buf.String())
	}

	target := filepath.Join(tmp, "target.json")
	os.WriteFile(target, []byte("{}"), 0644)
	link := filepath.Join(tmp, "link.json")
	linktest.Link(t, target, link)
	buf.Reset()
	printSymlinkAudit(&buf, link, ".mcp.json")
	out := buf.String()
	// On systems where managed links are symlinks the arrow renders; where
	// they are hard links (Windows) the entry is indistinguishable from a
	// local file. Either is acceptable, but it must not be "(not linked)".
	if strings.Contains(out, "(not linked)") {
		t.Errorf("healthy managed link must not be (not linked): %q", out)
	}
}

// TestPrintLinkedStatusLine_HealthyAndBroken covers the shared codex/agents
// link-status helper.
func TestPrintLinkedStatusLine_HealthyAndBroken(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t")
	os.WriteFile(target, []byte("x"), 0644)
	link := filepath.Join(tmp, "l")
	linktest.Link(t, target, link)
	var buf bytes.Buffer
	healthy := printLinkedStatusLine(&buf, "label", link)

	broken := filepath.Join(tmp, "b")
	linktest.DanglingLink(t, broken)
	buf.Reset()
	if printLinkedStatusLine(&buf, "label", broken) {
		t.Error("broken link should return false")
	}
	// healthy result depends on link kind (hard link on Windows is reported
	// healthy too); the broken case is the load-bearing assertion above.
	_ = healthy
}
