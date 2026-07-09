package commands

import (
	"os"
	"path/filepath"
	"strings"
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
	var badOK bool
	var badErr error
	out := captureRelinkStdout(t, func() {
		_, badOK, badErr = loadImportedPackagePluginManifest(bad)
	})
	if badErr != nil || badOK {
		t.Errorf("invalid json: err=%v ok=%v, want (nil,false)", badErr, badOK)
	}
	if !strings.Contains(out, "not valid JSON") {
		t.Errorf("expected loud warning for genuinely malformed JSON, got:\n%s", out)
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

// ---------- packagePluginLayoutKind (extended cases) ----------

func TestPackagePluginLayoutKind_AllSwitchArms(t *testing.T) {
	cases := []struct {
		rel        string
		rootPrefix string
		want       string
	}{
		{".claude-plugin/plugin.json", ".claude-plugin/", packagePluginManifestFile},
		{".claude-plugin/marketplace.json", ".claude-plugin/", packagePluginMarketplaceFile},
		{".claude-plugin/commands/plugin.json", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/agents/plugin.json", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/skills/plugin.json", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/hooks/plugin.json", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/rules/plugin.json", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/mcp.json", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/.mcp.json", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/commands/x.md", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/agents/x.md", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/skills/SKILL.md", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/hooks/h.yaml", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/rules/r.md", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/mcp/x", ".claude-plugin/", packagePluginComponentFile},
		{".claude-plugin/other.txt", ".claude-plugin/", packagePluginOverlayFile},
		{".claude-plugin/", ".claude-plugin/", ""},
	}
	for _, c := range cases {
		got := packagePluginLayoutKind(c.rel, c.rootPrefix)
		if got != c.want {
			t.Errorf("packagePluginLayoutKind(%q, %q) = %q, want %q", c.rel, c.rootPrefix, got, c.want)
		}
	}
}

// ---------- canonicalPluginOutputs error / fallback paths ----------

func TestCanonicalPluginOutputs_PathWithoutManifestNameReturnsFalse(t *testing.T) {
	// Source dir has .claude-plugin/commands/x.md but no plugin.json next to it.
	root := t.TempDir()
	src := filepath.Join(root, ".claude-plugin", "commands", "x.md")
	os.MkdirAll(filepath.Dir(src), 0755)
	os.WriteFile(src, []byte("body"), 0644)

	c := importCandidate{project: "p", sourceRoot: root, sourcePath: src}
	outputs, ok, err := canonicalPluginOutputs(c, ".claude-plugin/commands/x.md")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if ok || outputs != nil {
		t.Errorf("expected ok=false when manifest name is missing; got ok=%v outputs=%v", ok, outputs)
	}
}

func TestCanonicalPluginOutputs_MarketplaceRoute(t *testing.T) {
	root := t.TempDir()
	// .github/plugin/plugin.json names the plugin; marketplace.json is the source path.
	manifestPath := filepath.Join(root, ".github", "plugin", "plugin.json")
	os.MkdirAll(filepath.Dir(manifestPath), 0755)
	os.WriteFile(manifestPath, []byte(`{"name":"acme"}`), 0644)
	marketPath := filepath.Join(root, relCopilotPluginMarket)
	os.WriteFile(marketPath, []byte(`{"plugins":[{"name":"acme"}]}`), 0644)

	c := importCandidate{project: "p", sourceRoot: root, sourcePath: marketPath}
	outputs, ok, err := canonicalPluginOutputs(c, relCopilotPluginMarket)
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if len(outputs) != 1 || !strings.HasSuffix(outputs[0].destRel, "marketplace.json") {
		t.Errorf("unexpected outputs: %+v", outputs)
	}
}

func TestCanonicalPluginOutputs_ComponentRoute(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, ".claude-plugin", "plugin.json")
	os.MkdirAll(filepath.Dir(manifestPath), 0755)
	os.WriteFile(manifestPath, []byte(`{"name":"acme"}`), 0644)
	componentPath := filepath.Join(root, ".claude-plugin", "commands", "x.md")
	os.MkdirAll(filepath.Dir(componentPath), 0755)
	os.WriteFile(componentPath, []byte("body"), 0644)

	c := importCandidate{project: "p", sourceRoot: root, sourcePath: componentPath}
	outputs, ok, err := canonicalPluginOutputs(c, ".claude-plugin/commands/x.md")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if outputs[0].destRel != "plugins/p/acme/resources/commands/x.md" {
		t.Errorf("destRel = %q", outputs[0].destRel)
	}
}

func TestCanonicalPluginOutputs_OverlayRoute(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, ".claude-plugin", "plugin.json")
	os.MkdirAll(filepath.Dir(manifestPath), 0755)
	os.WriteFile(manifestPath, []byte(`{"name":"acme"}`), 0644)
	overlayPath := filepath.Join(root, ".claude-plugin", "extras", "f.txt")
	os.MkdirAll(filepath.Dir(overlayPath), 0755)
	os.WriteFile(overlayPath, []byte("over"), 0644)

	c := importCandidate{project: "p", sourceRoot: root, sourcePath: overlayPath}
	outputs, ok, err := canonicalPluginOutputs(c, ".claude-plugin/extras/f.txt")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if outputs[0].destRel != "plugins/p/acme/platforms/claude/extras/f.txt" {
		t.Errorf("destRel = %q", outputs[0].destRel)
	}
}

// ---------- canonicalPluginOutputsFromOpenCodeFile (error path) ----------

func TestCanonicalPluginOutputsFromOpenCodeFile_MissingSourceErrors(t *testing.T) {
	rel := relOpenCodePluginsDir + "myplugin/file.js"
	src := filepath.Join(t.TempDir(), "missing.js")
	_, _, err := canonicalPluginOutputsFromOpenCodeFile("proj", rel, src)
	if err == nil {
		t.Error("expected error for missing source")
	}
}

// ---------- directPluginFileCandidate ----------

func TestDirectPluginFileCandidate_CoveredRel(t *testing.T) {
	tmp := t.TempDir()
	// `.mcp.json` is a covered single → expected false even if file exists
	target := filepath.Join(tmp, ".mcp.json")
	os.WriteFile(target, []byte("{}"), 0644)
	_, ok := directPluginFileCandidate(tmp, ".mcp.json")
	if ok {
		t.Error("covered rel should return ok=false")
	}
}

func TestDirectPluginFileCandidate_BackupFile(t *testing.T) {
	tmp := t.TempDir()
	rel := "extras/x.dot-agents-backup"
	target := filepath.Join(tmp, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("x"), 0644)
	_, ok := directPluginFileCandidate(tmp, rel)
	if ok {
		t.Error("backup file should be rejected")
	}
}

func TestDirectPluginFileCandidate_DirectoryRejected(t *testing.T) {
	tmp := t.TempDir()
	rel := "subdir"
	os.MkdirAll(filepath.Join(tmp, rel), 0755)
	_, ok := directPluginFileCandidate(tmp, rel)
	if ok {
		t.Error("directory should be rejected as file candidate")
	}
}

func TestDirectPluginFileCandidate_OK(t *testing.T) {
	tmp := t.TempDir()
	rel := "extras/x.json"
	target := filepath.Join(tmp, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("{}"), 0644)
	got, ok := directPluginFileCandidate(tmp, rel)
	if !ok || got != target {
		t.Errorf("got=%q ok=%v", got, ok)
	}
}

// ---------- packagePluginNameFromMarketplace ----------

func TestPackagePluginNameFromMarketplace_PrefersSourcePath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mp.json")
	os.WriteFile(src, []byte(`{"plugins":[{"name":"primary"}]}`), 0644)

	name, err := packagePluginNameFromMarketplace(src, "copilot", filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if name != "primary" {
		t.Errorf("name = %q, want primary", name)
	}
}

func TestPackagePluginNameFromMarketplace_FallsBackToSiblingMarketplace(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "plugin.json")
	// source path is the manifest itself with no name → empty
	os.WriteFile(manifestPath, []byte(`{"plugins":[]}`), 0644)
	// sibling marketplace.json provides the name
	os.WriteFile(filepath.Join(dir, importMarketplaceJSON), []byte(`{"plugins":[{"name":"sibling"}]}`), 0644)

	name, err := packagePluginNameFromMarketplace(manifestPath, "claude", manifestPath)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if name != "sibling" {
		t.Errorf("name = %q, want sibling", name)
	}
}

func TestPackagePluginNameFromMarketplace_NoneFound(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "absent.json")
	// unknown platform skips sibling lookup; manifest path doesn't exist → empty
	name, err := packagePluginNameFromMarketplace(manifestPath, "opencode", manifestPath)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}

// ---------- canonicalPackagePluginMarketplaceOutputs ----------

func TestCanonicalPackagePluginMarketplaceOutputs_WritesMarketplaceUnderPlatform(t *testing.T) {
	src := filepath.Join(t.TempDir(), "marketplace.json")
	body := []byte(`{"plugins":[{"name":"acme"}]}`)
	os.WriteFile(src, body, 0644)

	c := importCandidate{project: "proj", sourceRoot: filepath.Dir(src), sourcePath: src}
	outputs, ok, err := canonicalPackagePluginMarketplaceOutputs(c, "copilot", "acme", "ignored")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	want := "plugins/proj/acme/platforms/copilot/marketplace.json"
	if outputs[0].destRel != want {
		t.Errorf("destRel = %q, want %q", outputs[0].destRel, want)
	}
	if string(outputs[0].content) != string(body) {
		t.Errorf("content mismatch")
	}
}

func TestCanonicalPackagePluginMarketplaceOutputs_MissingSourceErrors(t *testing.T) {
	c := importCandidate{project: "proj", sourceRoot: t.TempDir(), sourcePath: filepath.Join(t.TempDir(), "missing.json")}
	_, _, err := canonicalPackagePluginMarketplaceOutputs(c, "copilot", "acme", "")
	if err == nil {
		t.Error("expected error reading missing source")
	}
}

// ---------- canonicalPackagePluginComponentOutput ----------

func TestCanonicalPackagePluginComponentOutput_StripsRootRel(t *testing.T) {
	src := filepath.Join(t.TempDir(), "x.md")
	body := []byte("# component")
	os.WriteFile(src, body, 0644)
	c := importCandidate{project: "proj", sourceRoot: filepath.Dir(src), sourcePath: src}

	outputs, ok, err := canonicalPackagePluginComponentOutput(c, "claude", "acme", ".claude-plugin", ".claude-plugin/commands/x.md")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	want := "plugins/proj/acme/resources/commands/x.md"
	if outputs[0].destRel != want {
		t.Errorf("destRel = %q, want %q", outputs[0].destRel, want)
	}
}

func TestCanonicalPackagePluginComponentOutput_NoRootRel(t *testing.T) {
	src := filepath.Join(t.TempDir(), "y.md")
	os.WriteFile(src, []byte("body"), 0644)
	c := importCandidate{project: "proj", sourceRoot: filepath.Dir(src), sourcePath: src}

	outputs, ok, err := canonicalPackagePluginComponentOutput(c, "copilot", "acme", "", "agents/y.md")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if outputs[0].destRel != "plugins/proj/acme/resources/agents/y.md" {
		t.Errorf("destRel = %q", outputs[0].destRel)
	}
}

func TestCanonicalPackagePluginComponentOutput_RelDoesNotStartWithRootRel(t *testing.T) {
	c := importCandidate{project: "p", sourceRoot: t.TempDir(), sourcePath: filepath.Join(t.TempDir(), "x")}
	// rel not prefixed by rootRel/ → returns ok=false
	outputs, ok, err := canonicalPackagePluginComponentOutput(c, "claude", "acme", ".claude-plugin", "other/path.md")
	if err != nil || ok || outputs != nil {
		t.Errorf("expected (nil,false,nil); got outputs=%v ok=%v err=%v", outputs, ok, err)
	}
}

func TestCanonicalPackagePluginComponentOutput_UnknownTrimmedReturnsFalse(t *testing.T) {
	c := importCandidate{project: "p", sourceRoot: t.TempDir(), sourcePath: filepath.Join(t.TempDir(), "x")}
	// trimmed=garbage doesn't match any prefix rule
	outputs, ok, err := canonicalPackagePluginComponentOutput(c, "claude", "acme", ".claude-plugin", ".claude-plugin/unrelated/path")
	if err != nil || ok || outputs != nil {
		t.Errorf("expected (nil,false,nil); got outputs=%v ok=%v err=%v", outputs, ok, err)
	}
}

func TestCanonicalPackagePluginComponentOutput_MissingSourceErrors(t *testing.T) {
	c := importCandidate{project: "p", sourceRoot: t.TempDir(), sourcePath: filepath.Join(t.TempDir(), "missing.md")}
	_, _, err := canonicalPackagePluginComponentOutput(c, "claude", "acme", ".claude-plugin", ".claude-plugin/commands/x.md")
	if err == nil {
		t.Error("expected error reading missing source")
	}
}

// ---------- canonicalPackagePluginOverlayOutput ----------

func TestCanonicalPackagePluginOverlayOutput_PreservesRelativePath(t *testing.T) {
	src := filepath.Join(t.TempDir(), "extra.txt")
	body := []byte("overlay")
	os.WriteFile(src, body, 0644)
	c := importCandidate{project: "proj", sourceRoot: filepath.Dir(src), sourcePath: src}

	outputs, ok, err := canonicalPackagePluginOverlayOutput(c, "claude", "acme", ".claude-plugin", ".claude-plugin/extras/extra.txt")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	want := "plugins/proj/acme/platforms/claude/extras/extra.txt"
	if outputs[0].destRel != want {
		t.Errorf("destRel = %q, want %q", outputs[0].destRel, want)
	}
	if string(outputs[0].content) != "overlay" {
		t.Errorf("content mismatch")
	}
}

func TestCanonicalPackagePluginOverlayOutput_NoRootRelMatchReturnsFalse(t *testing.T) {
	c := importCandidate{project: "p", sourceRoot: t.TempDir(), sourcePath: filepath.Join(t.TempDir(), "x")}
	// rel doesn't have rootRel/ prefix → trimmed == rel → ok=false
	outputs, ok, err := canonicalPackagePluginOverlayOutput(c, "claude", "acme", ".claude-plugin", "other/file")
	if err != nil || ok || outputs != nil {
		t.Errorf("expected (nil,false,nil); got %v %v %v", outputs, ok, err)
	}
}

func TestCanonicalPackagePluginOverlayOutput_EmptyTrimmedReturnsFalse(t *testing.T) {
	c := importCandidate{project: "p", sourceRoot: t.TempDir(), sourcePath: filepath.Join(t.TempDir(), "x")}
	// rel == rootRel + "/" → trimmed becomes "" → ok=false
	outputs, ok, err := canonicalPackagePluginOverlayOutput(c, "claude", "acme", ".claude-plugin", ".claude-plugin/")
	if err != nil || ok || outputs != nil {
		t.Errorf("expected (nil,false,nil); got %v %v %v", outputs, ok, err)
	}
}

func TestCanonicalPackagePluginOverlayOutput_MissingSourceErrors(t *testing.T) {
	c := importCandidate{project: "p", sourceRoot: t.TempDir(), sourcePath: filepath.Join(t.TempDir(), "missing")}
	_, _, err := canonicalPackagePluginOverlayOutput(c, "claude", "acme", ".claude-plugin", ".claude-plugin/file")
	if err == nil {
		t.Error("expected error reading missing source")
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

// TestDirectPackagePluginRefs_GitHubManifest exercises the .github/plugin/plugin.json
// manifest branch (relGitHubPluginManifest) which is not covered by the copilot
// plugin.json tests.
func TestDirectPackagePluginRefs_GitHubManifest(t *testing.T) {
	projectPath := t.TempDir()
	manifest := []byte(`{
  "name": "ghplugin",
  "agents": "external-agents",
  "hooks": "external/hooks.json"
}`)
	manifestPath := filepath.Join(projectPath, relGitHubPluginManifest)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
		t.Fatal(err)
	}
	// Backing files so candidate paths can resolve.
	agentDir := filepath.Join(projectPath, "external-agents")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "a.md"), []byte("# a"), 0644)
	os.MkdirAll(filepath.Join(projectPath, "external"), 0755)
	os.WriteFile(filepath.Join(projectPath, "external", "hooks.json"), []byte("{}"), 0644)

	refs, err := directPackagePluginRefs(projectPath)
	if err != nil {
		t.Fatalf("directPackagePluginRefs: %v", err)
	}
	if len(refs) == 0 {
		t.Errorf("expected refs from github plugin manifest, got 0")
	}
}

// TestDirectPackagePluginRefs_CodexManifest covers the relCodexPluginDir branch
// (.codex-plugin/plugin.json).
func TestDirectPackagePluginRefs_CodexManifest(t *testing.T) {
	projectPath := t.TempDir()
	codexManifestDir := filepath.Join(projectPath, ".codex-plugin")
	os.MkdirAll(codexManifestDir, 0755)
	manifest := []byte(`{
  "name": "codexplug",
  "skills": "external/skills",
  "hooks": "external/hooks.json",
  "mcpServers": "external/mcp.json",
  "apps": "external/app.json"
}`)
	if err := os.WriteFile(filepath.Join(codexManifestDir, "plugin.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(projectPath, "external", "skills")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "s.md"), []byte("# s"), 0644)
	os.MkdirAll(filepath.Join(projectPath, "external"), 0755)
	os.WriteFile(filepath.Join(projectPath, "external", "hooks.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectPath, "external", "mcp.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectPath, "external", "app.json"), []byte("{}"), 0644)

	refs, err := directPackagePluginRefs(projectPath)
	if err != nil {
		t.Fatalf("directPackagePluginRefs: %v", err)
	}
	if len(refs) == 0 {
		t.Errorf("expected refs from codex plugin manifest, got 0")
	}
}

// TestDirectPackagePluginRefsForManifest_LoadError forces a manifest-load error
// (manifest path is a directory → ReadFile returns non-IsNotExist error) to
// cover the err != nil branch (line 227-229).
func TestDirectPackagePluginRefsForManifest_LoadError(t *testing.T) {
	projectPath := t.TempDir()
	// Directory at the manifest path → ReadFile returns EISDIR.
	manifestPath := filepath.Join(projectPath, "plugin.json")
	if err := os.MkdirAll(manifestPath, 0755); err != nil {
		t.Fatal(err)
	}
	refs, err := directPackagePluginRefsForManifest(projectPath, "copilot", manifestPath, func(importedPackagePluginManifest, string) []directPackagePluginRef {
		return nil
	})
	if err == nil {
		t.Errorf("expected error from non-readable manifest, got refs=%v", refs)
	}
}

// TestDirectPackagePluginRefsForManifest_NoManifest covers the !ok branch.
func TestDirectPackagePluginRefsForManifest_NoManifest(t *testing.T) {
	projectPath := t.TempDir()
	manifestPath := filepath.Join(projectPath, "does-not-exist.json")
	refs, err := directPackagePluginRefsForManifest(projectPath, "copilot", manifestPath, func(importedPackagePluginManifest, string) []directPackagePluginRef {
		return nil
	})
	if err != nil || refs != nil {
		t.Errorf("expected (nil, nil) for missing manifest, got (%v, %v)", refs, err)
	}
}

// TestDirectPackagePluginRefsForManifest_NameFromMarketplace covers the path
// where manifest.Name is empty and we fall back to marketplace lookup. When the
// marketplace also doesn't resolve a name → returns (nil, nil).
func TestDirectPackagePluginRefsForManifest_NoName(t *testing.T) {
	projectPath := t.TempDir()
	// Manifest with no Name field and no marketplace neighbor.
	manifestPath := filepath.Join(projectPath, relCopilotPluginManifest)
	if err := os.WriteFile(manifestPath, []byte(`{"agents": "x"}`), 0644); err != nil {
		t.Fatal(err)
	}
	refs, err := directPackagePluginRefsForManifest(projectPath, "copilot", manifestPath, func(importedPackagePluginManifest, string) []directPackagePluginRef {
		t.Errorf("build should not be called when name is empty")
		return nil
	})
	if err != nil || refs != nil {
		t.Errorf("expected (nil, nil) when name resolution fails, got (%v, %v)", refs, err)
	}
}

// TestCanonicalPluginOutputs_ManifestLoadError covers err return from
// loadImportedPackagePluginManifest inside canonicalPluginOutputs.
func TestCanonicalPluginOutputs_ManifestLoadError(t *testing.T) {
	tmp := t.TempDir()
	// Make plugin.json a directory → ReadFile returns non-IsNotExist error.
	if err := os.MkdirAll(filepath.Join(tmp, "plugin.json"), 0755); err != nil {
		t.Fatal(err)
	}
	agentsDir := filepath.Join(tmp, "agents")
	os.MkdirAll(agentsDir, 0755)
	source := filepath.Join(agentsDir, "foo.md")
	os.WriteFile(source, []byte("# foo"), 0644)
	c := importCandidate{
		project:    "test",
		sourceRoot: tmp,
		sourcePath: source,
	}
	outputs, ok, err := canonicalPluginOutputs(c, "agents/foo.md")
	if err == nil {
		t.Errorf("expected manifest-load error, got outputs=%v ok=%v", outputs, ok)
	}
}
