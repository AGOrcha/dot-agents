package platform

// Second-wave coverage tests targeting the largest remaining gaps after
// coverage_gap_test.go raised the floor to ~86%. Focus areas:
//   - plugin scope listing (listPluginSpecsInScope / ListPluginSpecs).
//   - cursor SQLite stats reader (cursorReadUsageStats) seeded via modernc.
//   - exhaustive Validate / validateEnums branches on ResourceIntent.
//   - additional CreateLinks branches for claude / cursor / codex / opencode
//     that the lifecycle smoke tests do not exercise.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
)

// TestListPluginSpecs_WithMultipleScopes drives the no-scope-walk branch which
// recurses over each subdirectory in agentsHome/plugins/.
func TestListPluginSpecs_WithMultipleScopes(t *testing.T) {
	tmp := t.TempDir()
	mk := func(scope, name string) {
		dir := filepath.Join(tmp, "plugins", scope, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		manifest := `schema_version: 1
kind: native
name: ` + name + `
platforms:
  - claude
`
		if err := os.WriteFile(filepath.Join(dir, PluginManifestName), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mk("global", "alpha")
	mk("global", "beta")
	mk("proj", "gamma")

	specs, err := ListPluginSpecs(tmp, "")
	if err != nil {
		t.Fatalf("ListPluginSpecs: %v", err)
	}
	if len(specs) != 3 {
		t.Errorf("expected 3 specs, got %d", len(specs))
	}

	// Scope-scoped listing.
	specs, err = ListPluginSpecs(tmp, "global")
	if err != nil {
		t.Fatalf("scope listing: %v", err)
	}
	if len(specs) != 2 {
		t.Errorf("expected 2 specs in global, got %d", len(specs))
	}
	if specs[0].Scope != "global" {
		t.Errorf("Scope = %q, want global", specs[0].Scope)
	}
}

func TestListPluginSpecs_BadManifestPropagates(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "plugins", "global", "broken")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PluginManifestName), []byte(":\n  -bad-yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListPluginSpecs(tmp, ""); err == nil {
		t.Error("expected error to propagate from broken manifest")
	}
	if _, err := ListPluginSpecs(tmp, "global"); err == nil {
		t.Error("expected scoped error to propagate")
	}
}

func TestListPluginSpecs_SkipsNonDirInsideScope(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "plugins", "global")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	specs, err := ListPluginSpecs(tmp, "global")
	if err != nil {
		t.Fatalf("ListPluginSpecs: %v", err)
	}
	if len(specs) != 0 {
		t.Errorf("expected 0 specs, got %d", len(specs))
	}
}

// TestCursorReadUsageStats_DrivesSQLitePath seeds an in-tree SQLite database
// matching cursor's schema and verifies cursorReadUsageStats returns rows.
func TestCursorReadUsageStats_DrivesSQLitePath(t *testing.T) {
	home := t.TempDir()
	dbDir := filepath.Join(home, ".cursor", "ai-tracking")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "ai-code-tracking.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE scored_commits (
		commitHash TEXT, branchName TEXT, scoredAt INTEGER,
		linesAdded INTEGER, linesDeleted INTEGER,
		composerLinesAdded INTEGER, composerLinesDeleted INTEGER,
		humanLinesAdded INTEGER, v2AiPercentage REAL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO scored_commits VALUES
		('hash1', 'main', 1715000000000, 100, 10, 60, 5, 40, 60.0),
		('hash2', 'feat', 1714000000000, 50, 5, 20, 0, 30, 40.0)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	stats := cursorReadUsageStats(home)
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.PlatformID != "cursor" {
		t.Errorf("PlatformID = %q", stats.PlatformID)
	}
	if len(stats.CommitAttribution) != 2 {
		t.Errorf("expected 2 commits, got %d", len(stats.CommitAttribution))
	}
}

func TestCursorReadUsageStats_NoDB(t *testing.T) {
	if stats := cursorReadUsageStats(t.TempDir()); stats != nil {
		t.Errorf("expected nil for missing DB, got %+v", stats)
	}
}

func TestCursorReadUsageStats_QueryError(t *testing.T) {
	home := t.TempDir()
	dbDir := filepath.Join(home, ".cursor", "ai-tracking")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Empty file → sql.Open succeeds but Query errors (no table).
	if err := os.WriteFile(filepath.Join(dbDir, "ai-code-tracking.db"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if stats := cursorReadUsageStats(home); stats != nil {
		t.Errorf("expected nil for missing table, got %+v", stats)
	}
}

// TestResourceIntentValidateEnums_AllBadVariants drives each enum-mismatch
// branch in validateEnums.
func TestResourceIntentValidateEnums_AllBadVariants(t *testing.T) {
	good := validSharedSkillIntent(".agents/skills/review", "test")
	if err := good.Validate(); err != nil {
		t.Fatalf("baseline valid: %v", err)
	}
	cases := []struct {
		name string
		bad  func(ResourceIntent) ResourceIntent
		want string
	}{
		{"empty-ownership", func(r ResourceIntent) ResourceIntent { r.Ownership = ""; return r }, "ownership"},
		{"bad-ownership", func(r ResourceIntent) ResourceIntent { r.Ownership = "weird"; return r }, "ownership"},
		{"empty-shape", func(r ResourceIntent) ResourceIntent { r.Shape = ""; return r }, "shape"},
		{"bad-shape", func(r ResourceIntent) ResourceIntent { r.Shape = "weird"; return r }, "shape"},
		{"empty-transport", func(r ResourceIntent) ResourceIntent { r.Transport = ""; return r }, "transport"},
		{"bad-transport", func(r ResourceIntent) ResourceIntent { r.Transport = "weird"; return r }, "transport"},
		{"empty-replace", func(r ResourceIntent) ResourceIntent { r.ReplacePolicy = ""; return r }, "replace_policy"},
		{"bad-replace", func(r ResourceIntent) ResourceIntent { r.ReplacePolicy = "weird"; return r }, "replace_policy"},
		{"empty-prune", func(r ResourceIntent) ResourceIntent { r.PrunePolicy = ""; return r }, "prune_policy"},
		{"bad-prune", func(r ResourceIntent) ResourceIntent { r.PrunePolicy = "weird"; return r }, "prune_policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intent := tc.bad(good)
			err := intent.Validate()
			if err == nil {
				t.Fatalf("expected error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v missing %q", err, tc.want)
			}
		})
	}
}

// TestResourceSourceRefValidate_AllCases exhausts the kind switch.
func TestResourceSourceRefValidate_AllCases(t *testing.T) {
	base := ResourceSourceRef{
		Scope:        "proj",
		Bucket:       "skills",
		RelativePath: "x",
		Kind:         ResourceSourceCanonicalDir,
	}
	for _, kind := range []ResourceSourceKind{
		ResourceSourceCanonicalFile, ResourceSourceCanonicalDir, ResourceSourceCanonicalBundle,
	} {
		ref := base
		ref.Kind = kind
		if err := ref.Validate(); err != nil {
			t.Errorf("kind %q: %v", kind, err)
		}
	}
	bad := base
	bad.Kind = "weird"
	if err := bad.Validate(); err == nil {
		t.Error("expected error for unknown kind")
	}
	bad.Kind = ""
	if err := bad.Validate(); err == nil {
		t.Error("expected error for empty kind")
	}
	for _, missing := range []ResourceSourceRef{
		{Bucket: "x", RelativePath: "y", Kind: ResourceSourceCanonicalDir},
		{Scope: "x", RelativePath: "y", Kind: ResourceSourceCanonicalDir},
		{Scope: "x", Bucket: "y", Kind: ResourceSourceCanonicalDir},
	} {
		if err := missing.Validate(); err == nil {
			t.Errorf("expected error for missing field in %+v", missing)
		}
	}
}

// TestResourceIntentValidate_MissingMaterializer covers the empty-materializer branch.
func TestResourceIntentValidate_MissingMaterializer(t *testing.T) {
	intent := validSharedSkillIntent(".agents/skills/review", "test")
	intent.Materializer = ""
	if err := intent.Validate(); err == nil || !strings.Contains(err.Error(), "materializer") {
		t.Errorf("expected materializer error, got %v", err)
	}
}

// TestResourceIntentValidate_BadSourceRef propagates from SourceRef.Validate.
func TestResourceIntentValidate_BadSourceRef(t *testing.T) {
	intent := validSharedSkillIntent(".agents/skills/review", "test")
	intent.SourceRef.Kind = ""
	if err := intent.Validate(); err == nil {
		t.Error("expected propagated source_ref error")
	}
}

// TestValidateEnum_Direct exercises the helper with both success and failure paths.
func TestValidateEnum_Direct(t *testing.T) {
	if err := validateEnum("color", "red", []string{"red", "blue"}); err != nil {
		t.Errorf("valid: %v", err)
	}
	if err := validateEnum("color", "", []string{"red"}); err == nil {
		t.Error("expected required error for empty value")
	}
	if err := validateEnum("color", "green", []string{"red", "blue"}); err == nil {
		t.Error("expected unsupported error for unknown value")
	}
}

// TestSameStrings_Differences ensures the helper is symmetric on shuffles and
// rejects mismatched slices.
func TestSameStrings_Differences(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a"}, []string{}, false},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
	}
	for i, tc := range cases {
		if got := sameStrings(tc.a, tc.b); got != tc.want {
			t.Errorf("[%d] sameStrings(%v, %v) = %v, want %v", i, tc.a, tc.b, got, tc.want)
		}
	}
}

// TestCodexCreateLinks_FullRulesAndSettings drives the rules→AGENTS.md and
// settings→config.toml branches of (*codex).CreateLinks.
func TestCodexCreateLinks_FullRulesAndSettings(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed global rules and project override.
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "global", "agents.md"), []byte("# global\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "proj", "agents.md"), []byte("# proj\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed settings/codex.toml.
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "codex.toml"), []byte("model = \"x\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}

	// AGENTS.md should be linked (project override wins).
	if !links.IsManagedLink(filepath.Join(repo, "AGENTS.md"), filepath.Join(agentsHome, "rules", "proj", "agents.md")) {
		t.Error("AGENTS.md should be a managed link to the project override")
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex", "config.toml")); err != nil {
		t.Errorf("config.toml missing: %v", err)
	}
}

// TestClaudeCreateLinks_LegacyHooksAndAgents drives more branches of
// (*claude).CreateLinks via populated fixtures (legacy hook file + project
// settings + project rules).
// mustMkdirAllT calls os.MkdirAll, fatalling via the testing.T helper.
func mustMkdirAllT(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

// mustWriteFileT writes a file, fatalling via the testing.T helper.
func mustWriteFileT(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeAgentsHomeFile is a convenience helper that ensures the parent dir
// exists then writes a file under agentsHome/<rel>.
func writeAgentsHomeFile(t *testing.T, agentsHome, rel, content string) {
	t.Helper()
	full := filepath.Join(agentsHome, rel)
	mustMkdirAllT(t, filepath.Dir(full))
	mustWriteFileT(t, full, content)
}

// setupClaudeFullFixture provisions the full fixture used by the Claude
// CreateLinks test: rules, mcp, legacy hook, agent dir, skill dir, global rule.
func setupClaudeFullFixture(t *testing.T, agentsHome string) {
	t.Helper()
	writeAgentsHomeFile(t, agentsHome, filepath.Join("rules", "proj", "rule.md"), "# rule\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("mcp", "proj", "claude.json"), "{}")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("hooks", "proj", "claude-code.json"), `{"hooks":{}}`)
	writeAgentsHomeFile(t, agentsHome, filepath.Join("agents", "proj", "reviewer", "AGENT.md"), "---\nname: reviewer\n---\nbody\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("skills", "global", "tidy", "SKILL.md"), "---\nname: tidy\n---\nbody\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("rules", "global", "claude-code.md"), "# global\n")
}

func TestClaudeCreateLinks_FullFixture(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	mustMkdirAllT(t, home)

	setupClaudeFullFixture(t, agentsHome)

	repo := filepath.Join(tmp, "repo")
	mustMkdirAllT(t, repo)
	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	// Project rule should be linked.
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "rules", "proj--rule.md")); err != nil {
		t.Errorf("project rule symlink missing: %v", err)
	}
	// .mcp.json symlink.
	if _, err := os.Lstat(filepath.Join(repo, ".mcp.json")); err != nil {
		t.Errorf(".mcp.json missing: %v", err)
	}
	// Legacy hook settings.local.json symlink to legacy file.
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "settings.local.json")); err != nil {
		t.Errorf("settings.local.json missing: %v", err)
	}
}

// TestCursorCreateLinks_FullFixture drives all branches of cursor CreateLinks
// (rules, settings, mcp, cursorignore, hooks, agents).
// setupCursorFullFixture provisions the full fixture for the cursor
// CreateLinks test: rules for both scopes, settings, cursorignore, mcp, hooks.
func setupCursorFullFixture(t *testing.T, agentsHome string) {
	t.Helper()
	for _, scope := range []string{"global", "proj"} {
		writeAgentsHomeFile(t, agentsHome, filepath.Join("rules", scope, "x.md"), "# rule\n")
	}
	writeAgentsHomeFile(t, agentsHome, filepath.Join("settings", "proj", "cursor.json"), "{}")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("settings", "proj", "cursorignore"), "node_modules\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("mcp", "proj", "cursor.json"), "{}")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("hooks", "proj", "cursor.json"), "{}")
}

func TestCursorCreateLinks_FullFixture(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	mustMkdirAllT(t, filepath.Join(tmp, "home"))

	setupCursorFullFixture(t, agentsHome)

	repo := filepath.Join(tmp, "repo")
	mustMkdirAllT(t, repo)
	if err := NewCursor().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	for _, expect := range []string{
		filepath.Join(repo, ".cursor", "rules"),
		filepath.Join(repo, ".cursor", "settings.json"),
		filepath.Join(repo, ".cursor", "mcp.json"),
		filepath.Join(repo, ".cursorignore"),
		filepath.Join(repo, ".cursor", "hooks.json"),
	} {
		if _, err := os.Stat(expect); err != nil {
			t.Errorf("expected %s: %v", expect, err)
		}
	}
}

// TestOpencodeCreateLinks_FullFixture drives ensureUserAgents + settings link.
func TestOpencodeCreateLinks_FullFixture(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Agent dir with marker (for ensureUserAgents).
	d := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "AGENT.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	// Project opencode.json
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "opencode.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewOpenCode().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "opencode.json")); err != nil {
		t.Errorf("opencode.json link missing: %v", err)
	}
	// User home should have agent symlink under .opencode/agent.
	if _, err := os.Lstat(filepath.Join(home, ".opencode", "agent", "reviewer.md")); err != nil {
		t.Errorf("expected user-home agent symlink: %v", err)
	}
}

// TestOpencodeSharedTargetIntents covers the all-branches code path
// (skills + plugins + agents) by seeding the buckets.
func TestOpencodeSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	// Seed skill, plugin, agent buckets for proj.
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"plugins", "proj", "rt-plugin", "PLUGIN.yaml"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		content := "name: x"
		if p[3] == "PLUGIN.yaml" {
			content = "schema_version: 1\nkind: native\nname: rt-plugin\nplatforms: [opencode]\n"
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewOpenCode().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestCopilotSharedTargetIntentsPopulated drives the skills+agents combination.
func TestCopilotSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewCopilot().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestCodexSharedTargetIntentsPopulated drives skills + codex-agent-toml intents.
func TestCodexSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewCodex().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestRenderCursorHookEntry_TimeoutClampMinimum exercises the timeout clamp
// branch when TimeoutMS / 1000 == 0.
func TestRenderCursorHookEntry_TimeoutClampMinimum(t *testing.T) {
	event, entry, ok, err := renderCursorHookEntry(HookSpec{
		Name:      "tiny",
		When:      "pre_tool_use",
		Command:   "/bin/true",
		TimeoutMS: 500, // < 1s after integer division → should clamp to 1
	})
	if err != nil {
		t.Fatalf("renderCursorHookEntry: %v", err)
	}
	if !ok || event != "preToolUse" {
		t.Fatalf("unexpected event/ok: %q %v", event, ok)
	}
	if entry.Timeout != 1 {
		t.Errorf("Timeout = %d, want clamped to 1", entry.Timeout)
	}
}

func TestRenderCopilotHookFile_TimeoutClampMinimum(t *testing.T) {
	_, _, _, err := renderCopilotHookFile(HookSpec{
		Name:      "tiny",
		When:      "user_prompt_submit",
		Command:   "/bin/true",
		TimeoutMS: 500,
	})
	if err != nil {
		t.Errorf("renderCopilotHookFile clamp: %v", err)
	}
}

// TestRenderCodexHookConfigMatcherBranches covers session_start/pre_tool_use/
// post_tool_use which take the matcherForSpec branch versus stop which uses
// empty matcher.
func TestRenderCodexHookConfig_MatcherBranches(t *testing.T) {
	specs := []HookSpec{
		{Name: "a", When: "stop", Command: "/bin/x"},
		{Name: "b", When: "pre_tool_use", Command: "/bin/y", MatchExpression: "Bash"},
	}
	content, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCodexHookConfig: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "Stop") {
		t.Errorf("expected Stop event: %s", got)
	}
	if !strings.Contains(got, `"matcher": "Bash"`) {
		t.Errorf("expected Bash matcher: %s", got)
	}
}

// TestClaudeExtractBranchSession_UUIDFallback hits the sid-from-uuid branch.
func TestClaudeExtractBranchSession_UUIDFallback(t *testing.T) {
	line := `{"uuid":"uuid-xyz","timestamp":"2026-05-11T10:00:00Z","gitBranch":"main"}`
	sid, ts := claudeExtractBranchSession(line, "main")
	if sid != "uuid-xyz" {
		t.Errorf("sid = %q, want uuid-xyz", sid)
	}
	if ts == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestClaudeExtractBranchSession_NoSessionOrUUID(t *testing.T) {
	line := `{"timestamp":"2026-05-11T10:00:00Z","gitBranch":"main"}`
	sid, _ := claudeExtractBranchSession(line, "main")
	if sid != "" {
		t.Errorf("expected empty sid, got %q", sid)
	}
}

func TestClaudeExtractBranchSession_BadJSON(t *testing.T) {
	sid, _ := claudeExtractBranchSession("not-json", "main")
	if sid != "" {
		t.Errorf("expected empty for bad json, got %q", sid)
	}
}

// TestClaudeScanJSONLForBranch_AssistantCountReflectsLineHits adds an
// assistant-counting test case to drive the assistantLines++ branch.
func TestClaudeScanJSONLForBranch_AssistantCount(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	sess := "count-test"
	target := "main"
	lines := []string{
		`{"type":"assistant","sessionId":"count-test","gitBranch":"main","timestamp":"2026-05-11T10:00:00Z","message":{"content":""}}`,
		`{"type":"assistant","sessionId":"count-test","gitBranch":"main","timestamp":"2026-05-11T11:00:00Z","message":{"content":""}}`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)
	slug := strings.ReplaceAll(project, "/", "-")
	path := filepath.Join(home, ".claude", "projects", slug, sess+".jsonl")
	marker := `"gitBranch":"` + target + `"`
	got := claudeScanJSONLForBranch(path, marker, target)
	if got == nil {
		t.Fatal("expected match")
	}
	if got.MessageCount < 2 {
		t.Errorf("MessageCount = %d, want >= 2", got.MessageCount)
	}
}

// TestCopilotScanSessionTokens_MtimeFilter exercises the time filter for
// session-state directories.
func TestCopilotScanSessionTokens_MtimeFilter(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".copilot", "session-state", "abc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(events, []byte(`{"type":"session.shutdown","data":{"modelMetrics":{"x":{"usage":{"inputTokens":1}}}}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Future cutoff → filtered out.
	got := copilotScanSessionTokens(home, "2099-01-01T00:00:00Z")
	if got.InputTokens != 0 {
		t.Errorf("expected filter to skip, got %+v", got)
	}
}

// TestOpencodeScanSessionTokensMissingDB drives the missing-db path.
func TestOpencodeScanSessionTokensMissingDB(t *testing.T) {
	got := opencodeScanSessionTokens(t.TempDir(), "")
	if got.InputTokens != 0 {
		t.Errorf("expected zero for missing db, got %+v", got)
	}
}

// TestSyncResourceDirEntries_HardError drives the mkdir error branch.
func TestSyncResourceDirEntries_MkdirError(t *testing.T) {
	// Try to use a path that contains a regular file as parent dir.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// dstRoot below a regular file → MkdirAll errors.
	dst := filepath.Join(blocker, "child")
	err := syncResourceDirEntries(stdPlatformIO{}, []resourceDir{{Name: "x", Dir: "/no/where"}}, dst)
	if err == nil {
		t.Error("expected mkdir error")
	}
}
