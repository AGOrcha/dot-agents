package platform

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/linktest"
)

// ---- identity / probes -----------------------------------------------------

func TestAntigravityIdentityAndStubs(t *testing.T) {
	a := NewAntigravity().(*antigravity)
	if a.ID() != "antigravity" {
		t.Errorf("ID() = %q, want antigravity", a.ID())
	}
	if a.DisplayName() != antigravityDisplayName {
		t.Errorf("DisplayName() = %q, want %q", a.DisplayName(), antigravityDisplayName)
	}
	if a.AIAgentPrefix() != "antigravity" {
		t.Errorf("AIAgentPrefix() = %q", a.AIAgentPrefix())
	}
	if got := a.SessionEnvs(); len(got) != 1 || got[0] != "ANTIGRAVITY_SESSION_ID" {
		t.Errorf("SessionEnvs() = %v", got)
	}
	if a.EntrypointEnvs() != nil {
		t.Errorf("EntrypointEnvs() = %v, want nil", a.EntrypointEnvs())
	}
	if a.ResolveModel("", "", "") != "" {
		t.Error("ResolveModel must be empty stub")
	}
	if a.HasDeprecatedFormat(t.TempDir()) {
		t.Error("HasDeprecatedFormat must be false")
	}
	if a.DeprecatedDetails(t.TempDir()) != "" {
		t.Error("DeprecatedDetails must be empty")
	}
}

// TestAntigravityProbesAbsent: with an empty PATH the CLI probes report absent.
func TestAntigravityProbesAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := NewAntigravity().(*antigravity)
	if a.IsInstalled() {
		t.Error("IsInstalled() expected false with empty PATH")
	}
	if v := a.Version(); v != "" {
		t.Errorf("Version() = %q, want empty", v)
	}
}

// TestAntigravityRegisteredInAll confirms touchpoint #1 wiring.
func TestAntigravityRegisteredInAll(t *testing.T) {
	if ByID("antigravity") == nil {
		t.Fatal("antigravity not discoverable via ByID/All()")
	}
}

// ---- CreateLinks / RemoveLinks ---------------------------------------------

func seedScoped(t *testing.T, agentsHome, bucket, scope, name, content string) string {
	t.Helper()
	dir := filepath.Join(agentsHome, bucket, scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAntigravityCreateLinks_FullFixture drives settings + mcp + legacy hooks.
func TestAntigravityCreateLinks_FullFixture(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	seedScoped(t, agentsHome, "settings", "proj", antigravityJSON, "{}")
	seedScoped(t, agentsHome, "mcp", "proj", antigravityJSON, `{"mcpServers":{}}`)
	// Legacy single-file hook source (drives the emitHookSpec hardlink path for
	// both repo + user-home).
	seedScoped(t, agentsHome, "hooks", "global", antigravityJSON, "{}")

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewAntigravity().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	for _, name := range []string{antigravitySettingsFile, antigravityMCPFile, antigravityHooksFile} {
		if _, err := os.Lstat(filepath.Join(repo, antigravityDir, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, antigravityDir, antigravityHooksFile)); err != nil {
		t.Errorf("expected user-home hooks.json: %v", err)
	}
}

// TestAntigravityCreateLinks_EmptyHomeNoSources: absent sources => no files,
// no error (covers the src=="" early return and removeRendered fallbacks).
func TestAntigravityCreateLinks_EmptyHomeNoSources(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewAntigravity().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks on empty home: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, antigravityDir, antigravitySettingsFile)); !os.IsNotExist(err) {
		t.Error("settings.json must not exist without a source")
	}
}

// TestAntigravityCreateLinks_RenderedHooksBundle drives the canonical-bundle
// render path (renderAntigravityHookConfig) end-to-end through CreateLinks.
func TestAntigravityCreateLinks_RenderedHooksBundle(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "guard")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: guard\nwhen: pre_tool_use\nrun:\n  command: ./guard.sh\n"
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "guard.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewAntigravity().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repo, antigravityDir, antigravityHooksFile))
	if err != nil {
		t.Fatalf("read rendered hooks: %v", err)
	}
	if !strings.Contains(string(data), "PreToolUse") {
		t.Errorf("rendered hooks missing PreToolUse event: %s", data)
	}
	// Symmetric teardown removes the rendered file.
	if err := NewAntigravity().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, antigravityDir, antigravityHooksFile)); !os.IsNotExist(err) {
		t.Error("rendered hooks.json should be removed by RemoveLinks")
	}
}

// TestAntigravityRemoveLinks_FullPath exercises every RemoveLinks branch:
// settings/mcp hard links + skills/agents mirror symlinks.
func TestAntigravityRemoveLinks_FullPath(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, antigravityDir), 0755); err != nil {
		t.Fatal(err)
	}
	// settings hard link
	settingsSrc := seedScoped(t, agentsHome, "settings", "proj", antigravityJSON, "{}")
	linktest.Link(t, settingsSrc, filepath.Join(repo, antigravityDir, antigravitySettingsFile))
	// skill + agent mirror symlinks
	skillSrc := filepath.Join(agentsHome, "skills", "proj", "alpha")
	if err := os.MkdirAll(skillSrc, 0755); err != nil {
		t.Fatal(err)
	}
	skillDst := filepath.Join(repo, antigravityDir, "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(skillDst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, skillSrc, skillDst)
	agentSrc := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.MkdirAll(agentSrc, 0755); err != nil {
		t.Fatal(err)
	}
	agentDst := filepath.Join(repo, antigravityDir, "agents", "reviewer")
	if err := os.MkdirAll(filepath.Dir(agentDst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, agentSrc, agentDst)

	if err := NewAntigravity().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	for _, p := range []string{skillDst, agentDst} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("mirror symlink %s should be removed", p)
		}
	}
}

// TestAntigravityCreateLinks_MkdirBlocked forces the createScopedJSONLink
// MkdirAll error branch (and the CreateLinks settings-error propagation) by
// planting a regular file where the .antigravity dir must be created.
func TestAntigravityCreateLinks_MkdirBlocked(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, antigravityDir), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := NewAntigravity().CreateLinks("proj", repo); err == nil {
		t.Error("expected error when .antigravity is a regular file")
	}
}

// TestAntigravityCreateLinks_HooksCollectError forces the hooks collect-error
// path (writeRepoHooks → createHooksLinks → CreateLinks propagation) by making
// the canonical hooks/global bucket a regular file (ReadDir fails non-ENOENT).
func TestAntigravityCreateLinks_HooksCollectError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(filepath.Join(agentsHome, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "hooks", "global"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewAntigravity().CreateLinks("proj", repo); err == nil {
		t.Error("expected error when hooks/global is a regular file")
	}
}

// TestAntigravityWriteHooks_DirectErrorBranches covers the writeRepoHooks
// MkdirAll branch and the writeUserHomeHooks collect-error branch directly.
func TestAntigravityWriteHooks_DirectErrorBranches(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	a := &antigravity{io: stdPlatformIO{}}

	// writeRepoHooks MkdirAll failure: .antigravity is a regular file, no hook
	// bundles so collect succeeds first.
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, antigravityDir), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.writeRepoHooks("proj", repo, agentsHome); err == nil {
		t.Error("writeRepoHooks expected MkdirAll error")
	}

	// writeUserHomeHooks collect failure: hooks/global is a regular file.
	if err := os.MkdirAll(filepath.Join(agentsHome, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "hooks", "global"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.writeUserHomeHooks("proj", agentsHome); err == nil {
		t.Error("writeUserHomeHooks expected collect error")
	}
}

// TestRenderAntigravityHookConfig_PropagatesEntryError covers the render-loop
// error return when an entry is required but unrepresentable.
func TestRenderAntigravityHookConfig_PropagatesEntryError(t *testing.T) {
	specs := []HookSpec{{Name: "x", When: "subagent_start", RequiredOn: []string{"antigravity"}, Command: "/bin/true"}}
	if _, err := renderAntigravityHookConfig(specs); err == nil {
		t.Error("expected render error for required unrepresentable event")
	}
}

// ---- SharedTargetIntents ---------------------------------------------------

// TestAntigravitySharedTargetIntents_Errors covers both the skill-bucket and
// agent-bucket error returns (a regular file where a scoped bucket dir belongs
// makes listScopedResourceDirs fail non-ENOENT).
func TestAntigravitySharedTargetIntents_Errors(t *testing.T) {
	t.Run("skill bucket error", func(t *testing.T) {
		tmp := t.TempDir()
		agentsHome := filepath.Join(tmp, ".agents")
		t.Setenv("AGENTS_HOME", agentsHome)
		if err := os.MkdirAll(filepath.Join(agentsHome, "skills"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agentsHome, "skills", "proj"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewAntigravity().SharedTargetIntents("proj"); err == nil {
			t.Error("expected skill bucket error")
		}
	})
	t.Run("agent bucket error", func(t *testing.T) {
		tmp := t.TempDir()
		agentsHome := filepath.Join(tmp, ".agents")
		t.Setenv("AGENTS_HOME", agentsHome)
		// skills bucket absent (returns nil), agents bucket is a file.
		if err := os.MkdirAll(filepath.Join(agentsHome, "agents"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agentsHome, "agents", "proj"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewAntigravity().SharedTargetIntents("proj"); err == nil {
			t.Error("expected agent bucket error")
		}
	})
}

func TestAntigravitySharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(agentsHome, p[0], p[1], p[2])
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte("name: x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewAntigravity().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) != 2 {
		t.Fatalf("want 2 intents (skill+agent), got %d", len(intents))
	}
	var sawSkill, sawAgent bool
	for _, in := range intents {
		switch {
		case strings.Contains(filepath.ToSlash(in.TargetPath), ".antigravity/skills/"):
			sawSkill = true
		case strings.Contains(filepath.ToSlash(in.TargetPath), ".antigravity/agents/"):
			sawAgent = true
		}
	}
	if !sawSkill || !sawAgent {
		t.Errorf("intents missing skill/agent target: %+v", intents)
	}
}

func TestAntigravitySharedTargetIntents_Empty(t *testing.T) {
	t.Setenv("AGENTS_HOME", filepath.Join(t.TempDir(), ".agents"))
	intents, err := NewAntigravity().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("want 0 intents for empty home, got %d", len(intents))
	}
}

// TestAntigravityAllowlistsMirrorRoots confirms touchpoint #7: the skills and
// agents mirror roots are allowlisted for destructive imported-dir replace.
func TestAntigravityAllowlistsMirrorRoots(t *testing.T) {
	for _, target := range []string{".antigravity/skills/alpha", ".antigravity/agents/reviewer"} {
		if !isAllowlistedSharedMirrorTarget(target) {
			t.Errorf("%s should be allowlisted", target)
		}
	}
}

// ---- Diagnostics: BrokenLinks / CountLinks / Badge -------------------------

func TestAntigravityBrokenLinks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, antigravityDir), 0755); err != nil {
		t.Fatal(err)
	}
	a := &antigravity{io: stdPlatformIO{}}

	if got := a.BrokenLinks("proj", repo, agentsHome); len(got) != 0 {
		t.Errorf("empty project: want 0 broken, got %+v", got)
	}
	// Plain file is ignored.
	if err := os.WriteFile(filepath.Join(repo, antigravityDir, antigravityMCPFile), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := a.BrokenLinks("proj", repo, agentsHome); len(got) != 0 {
		t.Errorf("plain file must be ignored, got %+v", got)
	}
	// Dangling managed link is reported.
	linktest.DanglingLink(t, filepath.Join(repo, antigravityDir, antigravitySettingsFile))
	got := a.BrokenLinks("proj", repo, agentsHome)
	if len(got) != 1 || got[0].PlatformID != "antigravity" {
		t.Fatalf("want 1 antigravity broken link, got %+v", got)
	}
	if got[0].LinkPath == "" || got[0].DisplayDest == "" {
		t.Errorf("LinkPath/DisplayDest unset: %+v", got[0])
	}
}

func TestAntigravityCountLinksAndBadge(t *testing.T) {
	tmp := t.TempDir()
	a := &antigravity{io: stdPlatformIO{}}
	agentsHome := filepath.Join(tmp, "agents")

	b := a.Badge("proj", tmp, agentsHome)
	if b.Name != antigravityDisplayName || b.Present || b.Broken {
		t.Errorf("empty project Badge = %+v", b)
	}
	// A healthy managed symlink under .antigravity counts as present.
	target := seedScoped(t, agentsHome, "settings", "global", antigravityJSON, "{}")
	dst := filepath.Join(tmp, antigravityDir, antigravitySettingsFile)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, target, dst)
	ok, broken := a.CountLinks("proj", tmp, agentsHome)
	if ok < 1 || broken != 0 {
		t.Errorf("CountLinks = (%d,%d), want (>=1,0)", ok, broken)
	}
	if b := a.Badge("proj", tmp, agentsHome); !b.Present || b.Broken {
		t.Errorf("healthy Badge = %+v", b)
	}
}

// ---- Diagnostics: UserConfigReporter ---------------------------------------

func TestAntigravityUserBrokenLinksAndBadge(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, home string)
		wantBroken  int
		wantPresent bool
	}{
		{"empty home", func(t *testing.T, home string) {}, 0, false},
		{"broken hooks", func(t *testing.T, home string) {
			linktest.DanglingLink(t, filepath.Join(home, antigravityDir, antigravityHooksFile))
		}, 1, false},
		{"healthy hooks", func(t *testing.T, home string) {
			target := filepath.Join(home, ".agents", "hooks", "global", antigravityJSON)
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("{}"), 0644); err != nil {
				t.Fatal(err)
			}
			linktest.Link(t, target, filepath.Join(home, antigravityDir, antigravityHooksFile))
		}, 0, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)
			a := &antigravity{io: stdPlatformIO{}}
			assertUserBrokenLinks(t, "antigravity", a.UserBrokenLinks(home), tc.wantBroken)
			badge := a.UserBadge(home)
			if badge.Name != antigravityDisplayName {
				t.Errorf("UserBadge.Name = %q", badge.Name)
			}
			if badge.Present != tc.wantPresent {
				t.Errorf("UserBadge.Present = %v, want %v", badge.Present, tc.wantPresent)
			}
			if badge.Broken != (tc.wantBroken > 0) {
				t.Errorf("UserBadge.Broken = %v, want %v", badge.Broken, tc.wantBroken > 0)
			}
		})
	}
}

// ---- PrintAudit ------------------------------------------------------------

func TestAntigravityPrintAudit(t *testing.T) {
	tmp := t.TempDir()
	a := &antigravity{io: stdPlatformIO{}}
	var buf bytes.Buffer
	// Empty project: still renders header + empty dir markers without panicking.
	a.PrintAudit(&buf, "proj", tmp, filepath.Join(tmp, ".agents"))
	if !strings.Contains(buf.String(), antigravityDisplayName) {
		t.Errorf("audit missing header: %s", buf.String())
	}
	// Populated: a present skills mirror entry renders too.
	skillsDir := filepath.Join(tmp, antigravityDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	target := filepath.Join(agentsHome, "skills", "global", "alpha")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, target, filepath.Join(skillsDir, "alpha"))
	buf.Reset()
	a.PrintAudit(&buf, "proj", tmp, agentsHome)
	if !strings.Contains(buf.String(), "skills/") {
		t.Errorf("audit missing skills mirror: %s", buf.String())
	}
}

// ---- Hook rendering --------------------------------------------------------

func TestRenderAntigravityHookConfig(t *testing.T) {
	specs := []HookSpec{
		{Name: "guard", When: "pre_tool_use", Command: "/bin/guard", TimeoutMS: 2500, MatchTools: []string{"run_command"}},
		// Unmapped event without RequiredOn → silently skipped.
		{Name: "skip", When: "subagent_start", Command: "/bin/skip"},
	}
	data, err := renderAntigravityHookConfig(specs)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var out antigravityRenderedHooks
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	entries, ok := out.Hooks["PreToolUse"]
	if !ok || len(entries) != 1 {
		t.Fatalf("want 1 PreToolUse entry, got %+v", out.Hooks)
	}
	e := entries[0]
	if e.Matcher != "run_command" {
		t.Errorf("matcher = %q", e.Matcher)
	}
	if len(e.Hooks) != 1 || e.Hooks[0].Type != "command" || e.Hooks[0].Command != "/bin/guard" {
		t.Errorf("action = %+v", e.Hooks)
	}
	if e.Hooks[0].Timeout != 2 { // 2500ms / 1000 = 2
		t.Errorf("timeout = %d, want 2", e.Hooks[0].Timeout)
	}
	if _, ok := out.Hooks["subagent_start"]; ok {
		t.Error("unmapped event must be skipped")
	}
}

func TestRenderAntigravityHookEntry_Branches(t *testing.T) {
	// Unmapped event + RequiredOn → error.
	if _, _, _, err := renderAntigravityHookEntry(HookSpec{Name: "x", When: "subagent_start", RequiredOn: []string{"antigravity"}, Command: "/bin/true"}); err == nil {
		t.Error("expected error for unrepresentable required event")
	}
	// Mapped event but empty command, not required → skipped (include=false, no error).
	if _, _, include, err := renderAntigravityHookEntry(HookSpec{Name: "x", When: "stop"}); err != nil || include {
		t.Errorf("empty-command non-required: include=%v err=%v", include, err)
	}
	// Mapped event, empty command, required → error.
	if _, _, _, err := renderAntigravityHookEntry(HookSpec{Name: "x", When: "stop", RequiredOn: []string{"antigravity"}}); err == nil {
		t.Error("expected error for required hook with no command")
	}
	// Sub-second timeout rounds up to 1.
	_, entry, include, err := renderAntigravityHookEntry(HookSpec{Name: "x", When: "stop", Command: "/bin/true", TimeoutMS: 400})
	if err != nil || !include {
		t.Fatalf("valid stop hook: include=%v err=%v", include, err)
	}
	if entry.Hooks[0].Timeout != 1 {
		t.Errorf("sub-second timeout = %d, want 1", entry.Hooks[0].Timeout)
	}
}

func TestAntigravityEventNameMapping(t *testing.T) {
	cases := map[string]string{"pre_tool_use": "PreToolUse", "post_tool_use": "PostToolUse", "stop": "Stop"}
	for canonical, native := range cases {
		got, ok := antigravityEventName(HookSpec{When: canonical})
		if !ok || got != native {
			t.Errorf("antigravityEventName(%q) = (%q,%v), want %q", canonical, got, ok, native)
		}
		if !isKnownCanonicalEvent(canonical) {
			t.Errorf("%q should be a known canonical event", canonical)
		}
	}
	if _, ok := antigravityEventName(HookSpec{When: "subagent_start"}); ok {
		t.Error("subagent_start must not map for antigravity")
	}
}

func TestIsLikelyRenderedAntigravityHookConfig(t *testing.T) {
	good, _ := renderAntigravityHookConfig([]HookSpec{{Name: "g", When: "stop", Command: "/bin/true"}})
	if !isLikelyRenderedAntigravityHookConfig(good) {
		t.Error("rendered config should be recognized")
	}
	if isLikelyRenderedAntigravityHookConfig([]byte("not json")) {
		t.Error("invalid json must not be recognized")
	}
	if isLikelyRenderedAntigravityHookConfig([]byte(`{"hooks":{}}`)) {
		t.Error("empty hooks must not be recognized")
	}
}

// ---- interface conformance -------------------------------------------------

func TestAntigravityInterfaceConformance(t *testing.T) {
	var _ Platform = (*antigravity)(nil)
	var _ BrokenLinkReporter = (*antigravity)(nil)
	var _ LinkCounter = (*antigravity)(nil)
	var _ StatusBadger = (*antigravity)(nil)
	var _ UserConfigReporter = (*antigravity)(nil)
	var _ AuditPrinter = (*antigravity)(nil)
	var _ SessionReader = (*antigravity)(nil)
	// antigravity maintains no canonical store, so it must NOT be an
	// OrphanCanonicalReporter (mirrors the opencode negative contract).
	if _, ok := Platform(NewAntigravity()).(OrphanCanonicalReporter); ok {
		t.Error("antigravity must not implement OrphanCanonicalReporter")
	}
}
