package commands

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

// ---------- supportsCanonicalImportPath ----------

func TestSupportsCanonicalImportPath(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{relCursorHooksJSON, true},
		{relCodexHooksJSON, true},
		{relClaudeSettingsLocal, true},
		{relClaudeSettingsJSON, true},
		{".github/hooks/pre.json", true},
		{".opencode/plugins/myplugin/index.js", true},
		{relCopilotPluginManifest, true},
		{relGitHubPluginManifest, true},
		{".claude-plugin/plugin.json", true},
		{".cursor-plugin/plugin.json", true},
		{".codex-plugin/plugin.json", true},
		{"random/path.json", false},
		{"AGENTS.md", false},
		{"", false},
	}
	for _, c := range cases {
		got := supportsCanonicalImportPath(c.rel)
		if got != c.want {
			t.Errorf("supportsCanonicalImportPath(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

// ---------- isProjectImportRelCovered ----------

func TestIsProjectImportRelCovered(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{relCursorMCPJSON, true},
		{".cursor/rules/file.mdc", true},
		{".agents/skills/x/SKILL.md", true},
		{".claude/skills/x/SKILL.md", true},
		{".github/agents/agent.agent.md", true},
		{".opencode/plugins/p/file.js", true},
		{"some/random/path.json", false},
		{"", false},
	}
	for _, c := range cases {
		got := isProjectImportRelCovered(c.rel)
		if got != c.want {
			t.Errorf("isProjectImportRelCovered(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

// ---------- normalizeImportedPackagePluginPath ----------

func TestNormalizeImportedPackagePluginPath(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"./agents", "agents"},
		{"././agents/sub", "agents/sub"},
		{"agents/", "agents"},
		{".", ""},
		{"..", ""},
		{"../escape", ""},
		{"/absolute", ""},
		{"agents/sub", "agents/sub"},
	}
	for _, c := range cases {
		got := normalizeImportedPackagePluginPath(c.raw)
		if got != c.want {
			t.Errorf("normalizeImportedPackagePluginPath(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// ---------- packagePluginLayout ----------

func TestPackagePluginLayout(t *testing.T) {
	cases := []struct {
		rel      string
		platform string
		rootRel  string
		kind     string
	}{
		{relCopilotPluginManifest, "copilot", "", packagePluginManifestFile},
		{relGitHubPluginManifest, "copilot", ".github/plugin", packagePluginManifestFile},
		{relCopilotPluginMarket, "copilot", "", packagePluginMarketplaceFile},
		{relCodexPluginMarket, "codex", ".codex-plugin", packagePluginMarketplaceFile},
		{".claude-plugin/plugin.json", "claude", ".claude-plugin", packagePluginManifestFile},
		{".claude-plugin/marketplace.json", "claude", ".claude-plugin", packagePluginMarketplaceFile},
		{".claude-plugin/commands/x.md", "claude", ".claude-plugin", packagePluginComponentFile},
		{".claude-plugin/somefile", "claude", ".claude-plugin", packagePluginOverlayFile},
		{".cursor-plugin/plugin.json", "cursor", ".cursor-plugin", packagePluginManifestFile},
		{".cursor-plugin/mcp.json", "cursor", ".cursor-plugin", packagePluginComponentFile},
		{".codex-plugin/plugin.json", "codex", ".codex-plugin", packagePluginManifestFile},
		{"agents/x.md", "copilot", "", packagePluginComponentFile},
		{"skills/x/SKILL.md", "copilot", "", packagePluginComponentFile},
		{"commands/x.md", "copilot", "", packagePluginComponentFile},
		{"unrelated/path", "", "", ""},
	}
	for _, c := range cases {
		gotP, gotRoot, gotKind := packagePluginLayout(c.rel)
		if gotP != c.platform || gotRoot != c.rootRel || gotKind != c.kind {
			t.Errorf("packagePluginLayout(%q) = (%q,%q,%q), want (%q,%q,%q)",
				c.rel, gotP, gotRoot, gotKind, c.platform, c.rootRel, c.kind)
		}
	}
}

// ---------- packagePluginComponentPath ----------

func TestPackagePluginComponentPath(t *testing.T) {
	cases := []struct {
		trimmed   string
		platform  string
		component string
		rest      string
		ok        bool
	}{
		{"commands/x.md", "claude", "commands", "x.md", true},
		{"agents/x.md", "claude", "agents", "x.md", true},
		{"skills/x/SKILL.md", "claude", "skills", "x/SKILL.md", true},
		{"hooks/x.yaml", "claude", "hooks", "x.yaml", true},
		{"mcp/x.json", "claude", "mcp", "x.json", true},
		{"rules/x.md", "claude", "rules", "x.md", true},
		{"rules/x.md", "cursor", "rules", "x.md", true},
		{"mcp.json", "cursor", "mcp", "mcp.json", true},
		{".mcp.json", "cursor", "mcp", ".mcp.json", true},
		{"skills/x/SKILL.md", "codex", "skills", "x/SKILL.md", true},
		{"agents/x.md", "copilot", "agents", "x.md", true},
		{"unknown/x", "claude", "", "", false},
		{"x.md", "unknownplatform", "", "", false},
	}
	for _, c := range cases {
		c2, r, ok := packagePluginComponentPath(c.trimmed, c.platform)
		if c2 != c.component || r != c.rest || ok != c.ok {
			t.Errorf("packagePluginComponentPath(%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.trimmed, c.platform, c2, r, ok, c.component, c.rest, c.ok)
		}
	}
}

// ---------- loadImportedPackagePluginManifest ----------

func TestLoadImportedPackagePluginManifest(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	if _, ok, err := loadImportedPackagePluginManifest(missing); err != nil || ok {
		t.Errorf("missing path: err=%v ok=%v, want (nil,false)", err, ok)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadImportedPackagePluginManifest(bad); err != nil || ok {
		t.Errorf("invalid json: err=%v ok=%v, want (nil,false)", err, ok)
	}

	good := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(good, []byte(`{"name":"acme","version":"1.0","authors":["alice","bob"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	manifest, ok, err := loadImportedPackagePluginManifest(good)
	if err != nil || !ok {
		t.Fatalf("good manifest: err=%v ok=%v", err, ok)
	}
	if manifest.Name != "acme" || manifest.Version != "1.0" || len(manifest.Authors) != 2 {
		t.Errorf("unexpected parsed manifest: %#v", manifest)
	}
}

// ---------- nameFromMarketplace ----------

func TestNameFromMarketplace(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.json")
	if name, ok, err := nameFromMarketplace(missing, "copilot"); err != nil || ok || name != "" {
		t.Errorf("missing: name=%q ok=%v err=%v", name, ok, err)
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("not json"), 0644)
	if name, ok, _ := nameFromMarketplace(bad, "copilot"); ok || name != "" {
		t.Errorf("bad: name=%q ok=%v", name, ok)
	}

	empty := filepath.Join(dir, "empty.json")
	os.WriteFile(empty, []byte(`{"plugins":[]}`), 0644)
	if name, ok, _ := nameFromMarketplace(empty, "copilot"); ok || name != "" {
		t.Errorf("empty: name=%q ok=%v", name, ok)
	}

	good := filepath.Join(dir, "mp.json")
	os.WriteFile(good, []byte(`{"plugins":[{"name":"first"},{"name":"second"}]}`), 0644)
	name, ok, err := nameFromMarketplace(good, "copilot")
	if err != nil || !ok || name != "first" {
		t.Errorf("good: name=%q ok=%v err=%v", name, ok, err)
	}
}

// ---------- packagePluginManifestPath ----------

func TestPackagePluginManifestPath(t *testing.T) {
	root := "/tmp/proj"
	cases := []struct {
		platform string
		rootRel  string
		want     string
	}{
		{"copilot", "", filepath.Join(root, relCopilotPluginManifest)},
		{"copilot", ".github/plugin", filepath.Join(root, ".github/plugin", importPluginJSON)},
		{"codex", "", filepath.Join(root, ".codex-plugin", relCopilotPluginManifest)},
		{"codex", ".codex-plugin", filepath.Join(root, ".codex-plugin", relCopilotPluginManifest)},
		{"claude", ".claude-plugin", filepath.Join(root, ".claude-plugin", relCopilotPluginManifest)},
	}
	for _, c := range cases {
		got := packagePluginManifestPath(root, c.rootRel, c.platform)
		if got != c.want {
			t.Errorf("packagePluginManifestPath(%q,%q,%q) = %q, want %q",
				root, c.rootRel, c.platform, got, c.want)
		}
	}
}

// ---------- importedPackageAuthors ----------

func TestImportedPackageAuthors(t *testing.T) {
	cases := []struct {
		name     string
		manifest importedPackagePluginManifest
		want     []string
	}{
		{"empty", importedPackagePluginManifest{}, nil},
		{"authors-list", importedPackagePluginManifest{Authors: []string{"bob", "alice", "bob"}}, []string{"alice", "bob"}},
		{"author-only", importedPackagePluginManifest{Author: importedPackagePluginAuthor{Name: "carol"}}, []string{"carol"}},
		{"author-empty-name", importedPackagePluginManifest{Author: importedPackagePluginAuthor{Name: "  "}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := importedPackageAuthors(c.manifest)
			if len(got) != len(c.want) {
				t.Fatalf("authors = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("authors = %v, want %v", got, c.want)
				}
			}
		})
	}
}

// ---------- canonicalPluginOutputsFromOpenCodeFile ----------

func TestCanonicalPluginOutputsFromOpenCodeFile(t *testing.T) {
	dir := t.TempDir()
	rel := relOpenCodePluginsDir + "myplugin/file.js"
	src := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("console.log('hi')"), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalPluginOutputsFromOpenCodeFile("proj", rel, src)
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d: %#v", len(outputs), outputs)
	}

	// Check first output: PLUGIN.yaml manifest
	if outputs[0].destRel != "plugins/proj/myplugin/PLUGIN.yaml" {
		t.Errorf("manifest dest = %q", outputs[0].destRel)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(outputs[0].content, &spec); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if spec["name"] != "myplugin" {
		t.Errorf("spec.name = %#v, want myplugin", spec["name"])
	}
	if spec["kind"] != "native" {
		t.Errorf("spec.kind = %#v, want native", spec["kind"])
	}

	// Check second output: file passthrough
	if outputs[1].destRel != "plugins/proj/myplugin/files/file.js" {
		t.Errorf("file dest = %q", outputs[1].destRel)
	}
}

func TestCanonicalPluginOutputsFromOpenCodeFile_BadPath(t *testing.T) {
	dir := t.TempDir()
	// rel that does not split into name + rest
	rel := relOpenCodePluginsDir + "only"
	src := filepath.Join(dir, "ignored")
	os.WriteFile(src, []byte("x"), 0644)
	_, ok, err := canonicalPluginOutputsFromOpenCodeFile("proj", rel, src)
	if err != nil || ok {
		t.Errorf("bad path: ok=%v err=%v, want (false,nil)", ok, err)
	}
}

// ---------- canonicalPluginOutputs end-to-end (copilot package plugin) ----------

func TestCanonicalPluginOutputs_CopilotManifest(t *testing.T) {
	sourceRoot := t.TempDir()
	manifestPath := filepath.Join(sourceRoot, relCopilotPluginManifest)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		t.Fatal(err)
	}
	manifestData := []byte(`{
  "name": "acme",
  "version": "1.0.0",
  "description": "demo",
  "authors": ["alice"],
  "keywords": ["agent", "demo"]
}`)
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		t.Fatal(err)
	}

	c := importCandidate{
		project:    "proj",
		sourceRoot: sourceRoot,
		sourcePath: manifestPath,
	}
	outputs, ok, err := canonicalPluginOutputs(c, relCopilotPluginManifest)
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}
	if outputs[0].destRel != "plugins/proj/acme/PLUGIN.yaml" {
		t.Errorf("manifest dest = %q", outputs[0].destRel)
	}
	if outputs[1].destRel != "plugins/proj/acme/platforms/copilot/plugin.json" {
		t.Errorf("passthrough dest = %q", outputs[1].destRel)
	}

	var spec map[string]any
	if err := yaml.Unmarshal(outputs[0].content, &spec); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if spec["name"] != "acme" {
		t.Errorf("name = %#v, want acme", spec["name"])
	}
	if spec["kind"] != "package" {
		t.Errorf("kind = %#v, want package", spec["kind"])
	}
}

func TestCanonicalPluginOutputs_UnknownRelReturnsFalse(t *testing.T) {
	sourceRoot := t.TempDir()
	c := importCandidate{project: "proj", sourceRoot: sourceRoot, sourcePath: filepath.Join(sourceRoot, "x")}
	_, ok, err := canonicalPluginOutputs(c, "unrelated/path.txt")
	if err != nil || ok {
		t.Errorf("unrelated: ok=%v err=%v", ok, err)
	}
}

// ---------- gatherDirectPackagePluginCandidates ----------

func TestGatherDirectPackagePluginCandidates_CopilotManifestPointsToHooksFile(t *testing.T) {
	projectPath := t.TempDir()
	// Write a copilot manifest at plugin.json pointing to .github/external-hooks/myhook.json
	manifest := []byte(`{
  "name": "myplugin",
  "hooks": "external-hooks/myhook.json"
}`)
	if err := os.WriteFile(filepath.Join(projectPath, relCopilotPluginManifest), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(projectPath, "external-hooks", "myhook.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	candidates := gatherDirectPackagePluginCandidates("proj", projectPath)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %#v", len(candidates), candidates)
	}
	if candidates[0].sourcePath != hookPath {
		t.Errorf("candidate sourcePath = %q, want %q", candidates[0].sourcePath, hookPath)
	}
}

func TestGatherDirectPackagePluginCandidates_NoManifest(t *testing.T) {
	projectPath := t.TempDir()
	candidates := gatherDirectPackagePluginCandidates("proj", projectPath)
	if len(candidates) != 0 {
		t.Errorf("expected no candidates, got %d", len(candidates))
	}
}

func TestGatherDirectPackagePluginCandidates_DirRefCollectsFiles(t *testing.T) {
	projectPath := t.TempDir()
	// Copilot manifest pointing to an external agents directory
	manifest := []byte(`{
  "name": "myplugin",
  "agents": "external-agents"
}`)
	if err := os.WriteFile(filepath.Join(projectPath, relCopilotPluginManifest), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(projectPath, "external-agents")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "a.md"), []byte("# a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "b.md"), []byte("# b"), 0644); err != nil {
		t.Fatal(err)
	}

	candidates := gatherDirectPackagePluginCandidates("proj", projectPath)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates from agents dir, got %d: %#v", len(candidates), candidates)
	}
}

// ---------- supportsCanonicalImportPathNonPlugin ----------

func TestSupportsCanonicalImportPathNonPlugin(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{relCursorHooksJSON, true},
		{relCodexHooksJSON, true},
		{relClaudeSettingsLocal, true},
		{relClaudeSettingsJSON, true},
		{".github/hooks/some.json", true},
		{".opencode/plugins/p/file.js", true},
		{"random/file.txt", false},
		{"", false},
	}
	for _, c := range cases {
		got := supportsCanonicalImportPathNonPlugin(c.rel)
		if got != c.want {
			t.Errorf("supportsCanonicalImportPathNonPlugin(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}
