package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// ---------- NewInstallCmd metadata ----------

func TestNewInstallCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewInstallCmd()
	if cmd.Flags().Lookup("generate") == nil {
		t.Error("missing --generate flag")
	}
	if cmd.Flags().Lookup("strict") == nil {
		t.Error("missing --strict flag")
	}
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Error("install should reject positional args")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("install should accept zero args, got: %v", err)
	}
}

// ---------- installProjectName ----------

func TestInstallProjectName(t *testing.T) {
	if got := installProjectName("manifest-name", "/tmp/whatever/dir"); got != "manifest-name" {
		t.Errorf("got %q, want manifest-name", got)
	}
	if got := installProjectName("", "/tmp/whatever/dir"); got != "dir" {
		t.Errorf("got %q, want dir (basename)", got)
	}
}

// ---------- resourceMarkerFile ----------

func TestResourceMarkerFile(t *testing.T) {
	cases := map[string]string{
		"skills": "SKILL.md",
		"agents": "AGENT.md",
		"other":  "",
	}
	for in, want := range cases {
		if got := resourceMarkerFile(in); got != want {
			t.Errorf("resourceMarkerFile(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------- resourceCandidateIsValid ----------

func TestResourceCandidateIsValid(t *testing.T) {
	tmp := t.TempDir()
	// missing path
	if resourceCandidateIsValid(filepath.Join(tmp, "missing"), "SKILL.md") {
		t.Error("missing path should be invalid")
	}

	// file (not dir)
	plain := filepath.Join(tmp, "file.txt")
	os.WriteFile(plain, []byte("x"), 0644)
	if resourceCandidateIsValid(plain, "") {
		t.Error("non-dir should be invalid")
	}

	// dir without marker but markerFile == "" → valid
	dir := filepath.Join(tmp, "agentdir")
	os.MkdirAll(dir, 0755)
	if !resourceCandidateIsValid(dir, "") {
		t.Error("dir with empty markerFile should be valid")
	}

	// dir with marker missing → invalid
	if resourceCandidateIsValid(dir, "SKILL.md") {
		t.Error("dir without marker should be invalid")
	}

	// dir with marker present → valid
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0644)
	if !resourceCandidateIsValid(dir, "SKILL.md") {
		t.Error("dir with marker should be valid")
	}
}

// ---------- firstResourceCandidate ----------

func TestFirstResourceCandidate(t *testing.T) {
	root := t.TempDir()
	// project-scoped skill present
	projDir := filepath.Join(root, "skills", "proj", "mine")
	os.MkdirAll(projDir, 0755)
	os.WriteFile(filepath.Join(projDir, "SKILL.md"), []byte("x"), 0644)

	candidate, srcRoot, found := firstResourceCandidate("skills", "mine", "SKILL.md", "proj", []string{root})
	if !found {
		t.Fatal("expected candidate to be found")
	}
	if candidate != projDir {
		t.Errorf("candidate = %q, want %q", candidate, projDir)
	}
	if srcRoot != root {
		t.Errorf("srcRoot = %q, want %q", srcRoot, root)
	}

	// fallback to global
	root2 := t.TempDir()
	globalDir := filepath.Join(root2, "skills", "global", "shared")
	os.MkdirAll(globalDir, 0755)
	os.WriteFile(filepath.Join(globalDir, "SKILL.md"), []byte("x"), 0644)
	candidate, _, found = firstResourceCandidate("skills", "shared", "SKILL.md", "proj", []string{root2})
	if !found || candidate != globalDir {
		t.Errorf("expected global fallback, got candidate=%q found=%v", candidate, found)
	}

	// not found
	_, _, found = firstResourceCandidate("skills", "absent", "SKILL.md", "proj", []string{t.TempDir()})
	if found {
		t.Error("absent resource should not be found")
	}
}

// ---------- shouldSkipLinkDestination ----------

func TestShouldSkipLinkDestination(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "missing")
	if shouldSkipLinkDestination(missing) {
		t.Error("missing dest should not be skipped")
	}

	// exists, no force → skip
	exists := filepath.Join(tmp, "exists")
	os.MkdirAll(exists, 0755)
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if !shouldSkipLinkDestination(exists) {
		t.Error("existing without --force should be skipped")
	}

	// exists with force → remove and don't skip
	Flags = GlobalFlags{Force: true}
	if shouldSkipLinkDestination(exists) {
		t.Error("existing with --force should not skip")
	}
	if _, err := os.Stat(exists); err == nil {
		t.Error("--force should remove existing destination")
	}
}

// ---------- resolveSourceRoot ----------

func TestResolveSourceRoot_LocalDefault(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	root, err := resolveSourceRoot(config.Source{Type: "local"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if root != agentsHome {
		t.Errorf("root = %q, want %q", root, agentsHome)
	}
}

func TestResolveSourceRoot_LocalCustomPath(t *testing.T) {
	tmp := t.TempDir()
	root, err := resolveSourceRoot(config.Source{Type: "local", Path: tmp})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if root != tmp {
		t.Errorf("root = %q, want %q", root, tmp)
	}
}

func TestResolveSourceRoot_GitMissingURL(t *testing.T) {
	root, err := resolveSourceRoot(config.Source{Type: "git"})
	if err != nil || root != "" {
		t.Errorf("missing url: root=%q err=%v, want empty", root, err)
	}
}

func TestResolveSourceRoot_UnknownType(t *testing.T) {
	root, err := resolveSourceRoot(config.Source{Type: "ftp"})
	if err != nil || root != "" {
		t.Errorf("unknown type: root=%q err=%v, want empty", root, err)
	}
}

// ---------- resolveSources ----------

func TestResolveSources_MixedAndCustomDirs(t *testing.T) {
	tmp := t.TempDir()
	custom := filepath.Join(tmp, "custom-src")
	os.MkdirAll(custom, 0755)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	sources := []config.Source{
		{Type: "local", Path: custom},
		{Type: "ftp"}, // skipped
		{Type: "git"}, // skipped (missing url)
	}
	resolved, err := resolveSources(sources)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(resolved) != 1 || resolved[0] != custom {
		t.Errorf("resolved = %v, want [%s]", resolved, custom)
	}
}

// ---------- gitCloneDryRunCommand ----------

func TestGitCloneDryRunCommand(t *testing.T) {
	got := gitCloneDryRunCommand("https://example.com/repo.git", "", "/cache/x")
	if got != "git clone --depth 1 https://example.com/repo.git /cache/x" {
		t.Errorf("no ref: %q", got)
	}
	got = gitCloneDryRunCommand("https://example.com/repo.git", "main", "/cache/x")
	if got != "git clone --depth 1 --branch main https://example.com/repo.git /cache/x" {
		t.Errorf("with ref: %q", got)
	}
}

// ---------- hasCachedGitSource / shouldUseCachedGitSource ----------

func TestCachedGitSource(t *testing.T) {
	tmp := t.TempDir()
	if hasCachedGitSource(tmp) {
		t.Error("empty dir should not be a cached source")
	}
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	if !hasCachedGitSource(tmp) {
		t.Error("dir with .git should be a cached source")
	}

	saved := Flags
	defer func() { Flags = saved }()

	// no .last-fetch → should not use cache
	Flags = GlobalFlags{}
	if shouldUseCachedGitSource(tmp, "url") {
		t.Error("no .last-fetch: cache should not be used")
	}

	// fresh .last-fetch → use cache
	os.WriteFile(filepath.Join(tmp, ".last-fetch"), []byte("now"), 0644)
	if !shouldUseCachedGitSource(tmp, "url") {
		t.Error("fresh .last-fetch: cache should be used")
	}

	// force → don't use cache even with fresh marker
	Flags = GlobalFlags{Force: true}
	if shouldUseCachedGitSource(tmp, "url") {
		t.Error("--force: cache should not be used")
	}
}

// ---------- findProjectByPath ----------

func TestFindProjectByPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	projPath := filepath.Join(tmp, "myrepo")
	os.MkdirAll(projPath, 0755)
	cfg.AddProject("myrepo", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if got := findProjectByPath(projPath); got != "myrepo" {
		t.Errorf("got %q, want myrepo", got)
	}
	if got := findProjectByPath(filepath.Join(tmp, "missing")); got != "" {
		t.Errorf("missing project should return empty, got %q", got)
	}
}

// ---------- linkResourceFromSources (dry-run) ----------

func TestLinkResourceFromSources_DryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	if err := linkResourceFromSources("skills", "demo", "proj", []string{src}); err != nil {
		t.Fatalf("dry-run link failed: %v", err)
	}
	// Nothing should have been linked
	dest := filepath.Join(tmp, ".agents", "skills", "proj", "demo")
	if _, err := os.Lstat(dest); err == nil {
		t.Error("dry-run should not have created link")
	}
}

func TestLinkResourceFromSources_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	os.MkdirAll(filepath.Join(tmp, ".agents"), 0755)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := linkResourceFromSources("skills", "absent", "proj", []string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestLinkResourceFromSources_CreatesSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	os.MkdirAll(agentsHome, 0755)

	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0644)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := linkResourceFromSources("skills", "demo", "proj", []string{src}); err != nil {
		t.Fatalf("link failed: %v", err)
	}
	dest := filepath.Join(agentsHome, "skills", "proj", "demo")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", dest, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected dest to be a symlink")
	}
}

// ---------- runInstall: error pathways ----------

func TestRunInstall_NoManifestErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Run from a directory that has no .agentsrc.json
	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runInstall(false)
	if err == nil {
		t.Fatal("expected error when manifest missing")
	}
	if !strings.Contains(err.Error(), ".agentsrc.json") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunInstall_UninitializedAgentsHomeErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755) // exists but no config.json
	t.Setenv("AGENTS_HOME", agentsHome)

	projDir := filepath.Join(tmp, "proj")
	os.MkdirAll(projDir, 0755)
	manifest := config.AgentsRC{Version: 1, Project: "proj"}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(projDir, config.AgentsRCFile), data, 0644)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runInstall(false)
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected not-initialized error, got: %v", err)
	}
}

// ---------- runInstallGenerate: round-trip from current state ----------

func TestRunInstallGenerate_CreatesManifestFromState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Initialize a skill and an agent in canonical home
	projName := "myrepo"
	skillDir := filepath.Join(agentsHome, "skills", projName, "demo")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo"), 0644)
	agentDir := filepath.Join(agentsHome, "agents", projName, "support")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# support"), 0644)

	projPath := filepath.Join(tmp, "myrepo")
	os.MkdirAll(projPath, 0755)

	// Register the project so generate picks it up by name
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject(projName, projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projPath); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInstallGenerate(); err != nil {
		t.Fatalf("runInstallGenerate: %v", err)
	}

	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if rc.Project != projName {
		t.Errorf("manifest project = %q, want %q", rc.Project, projName)
	}
	if !containsString(rc.Skills, "demo") {
		t.Errorf("manifest skills = %v, want demo present", rc.Skills)
	}
	if !containsString(rc.Agents, "support") {
		t.Errorf("manifest agents = %v, want support present", rc.Agents)
	}
}

func TestRunInstallGenerate_DryRunDoesNotWrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "drygen")
	os.MkdirAll(projPath, 0755)

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projPath); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runInstallGenerate(); err != nil {
		t.Fatalf("dry-run generate: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projPath, config.AgentsRCFile)); err == nil {
		t.Error("dry-run should not write manifest")
	}
}

func TestRunInstallGenerate_PreservesExistingProjectAndExtras(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "mergetest")
	os.MkdirAll(projPath, 0755)

	// Pre-existing manifest with explicit project name and unknown key
	existing := []byte(`{
  "version": 1,
  "project": "explicit-name",
  "hooks": false,
  "mcp": false,
  "settings": false,
  "sources": [{"type":"local"}],
  "custom_extra": {"keep":true}
}`)
	if err := os.WriteFile(filepath.Join(projPath, config.AgentsRCFile), existing, 0644); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projPath); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInstallGenerate(); err != nil {
		t.Fatalf("runInstallGenerate: %v", err)
	}

	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if rc.Project != "explicit-name" {
		t.Errorf("project = %q, want explicit-name (preserved)", rc.Project)
	}
	if _, ok := rc.ExtraFields["custom_extra"]; !ok {
		t.Errorf("extra fields should be preserved: %v", rc.ExtraFields)
	}
}

// ---------- helpers ----------

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
